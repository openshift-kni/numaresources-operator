// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/term"
)

var useColor bool

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorDim    = "\033[2m"
)

func color(c, text string) string {
	if !useColor {
		return text
	}
	return c + text + colorReset
}

func red(format string, v ...any) string    { return color(colorRed, fmt.Sprintf(format, v...)) }
func green(format string, v ...any) string  { return color(colorGreen, fmt.Sprintf(format, v...)) }
func yellow(format string, v ...any) string { return color(colorYellow, fmt.Sprintf(format, v...)) }
func dim(format string, v ...any) string    { return color(colorDim, fmt.Sprintf(format, v...)) }

type MatchStatus int

const (
	StatusMatch MatchStatus = iota
	StatusMismatchMessage
	StatusUnmatched
)

func (s MatchStatus) String() string {
	switch s {
	case StatusMatch:
		return "MATCH"
	case StatusMismatchMessage:
		return "MISMATCH-MESSAGE"
	case StatusUnmatched:
		return "UNMATCHED"
	default:
		return "UNKNOWN"
	}
}

type CommitInfo struct {
	Hash    string
	PatchID string
	Message string
}

func (ci CommitInfo) ShortHash() string {
	if len(ci.Hash) >= 7 {
		return ci.Hash[:7]
	}
	return ci.Hash
}

func (ci CommitInfo) Subject() string {
	if idx := strings.IndexByte(ci.Message, '\n'); idx >= 0 {
		return ci.Message[:idx]
	}
	return ci.Message
}

type MatchResult struct {
	Backport     CommitInfo
	MainMatch    *CommitInfo
	ClosestMatch *CommitInfo
	Status       MatchStatus
}

type ReportOptions struct {
	Verbose      bool
	CheckMissing bool
	JSON         bool
}

type Options struct {
	BackportBranch string
	BackportRange  string
	BaseBranch     string
	MainRange      string
	MainBranch     string
	MainDepth      int
	CheckMissing   bool
	Verbose        bool
	Color          bool
	JSON           bool
}

func main() {
	var opts Options
	flag.StringVar(&opts.BackportBranch, "backport-branch", "", "local branch name containing backport commits")
	flag.StringVar(&opts.BackportRange, "backport-range", "", "git revision range for backport commits")
	flag.StringVar(&opts.BaseBranch, "base-branch", "", "branch the backport is based on (e.g. release-4.18); required with --backport-branch")
	flag.StringVar(&opts.MainRange, "main-range", "", "git revision range for main commits")
	flag.StringVar(&opts.MainBranch, "main-branch", "main", "name of the main branch where originals live")
	flag.IntVar(&opts.MainDepth, "main-depth", 100, "number of recent main commits to search (used with --backport-branch)")
	flag.BoolVar(&opts.CheckMissing, "check-missing", false, "report main commits with no backport counterpart")
	flag.BoolVar(&opts.Verbose, "verbose", false, "enable detailed output")
	flag.BoolVar(&opts.Color, "color", true, "enable colored output (auto-disabled when not a terminal)")
	flag.BoolVar(&opts.JSON, "json", false, "output results as JSON")
	flag.Parse()

	useColor = opts.Color && term.IsTerminal(int(os.Stdout.Fd()))

	exitCode, err := run(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
	os.Exit(exitCode)
}

func run(opts Options) (int, error) {
	if err := validateOptions(opts); err != nil {
		return 1, err
	}

	backportRange := opts.BackportRange
	mainRange := opts.MainRange

	if opts.BackportBranch != "" {
		backportRange = fmt.Sprintf("%s..%s", opts.BaseBranch, opts.BackportBranch)
		if mainRange == "" {
			mainRange = fmt.Sprintf("%s~%d..%s", opts.MainBranch, opts.MainDepth, opts.MainBranch)
		}
	}

	if opts.Verbose && !opts.JSON {
		fmt.Printf("backport range: %s\n", backportRange)
		fmt.Printf("main range:     %s\n", mainRange)
	}

	backportCommits, err := getCommitsFromRange(backportRange)
	if err != nil {
		return 1, fmt.Errorf("getting backport commits: %w", err)
	}

	if len(backportCommits) == 0 {
		return 1, fmt.Errorf("no commits found in backport range %q", backportRange)
	}

	mainCommits, err := getCommitsFromRange(mainRange)
	if err != nil {
		return 1, fmt.Errorf("getting main commits: %w", err)
	}

	if len(mainCommits) == 0 {
		return 1, fmt.Errorf("no commits found in main range %q", mainRange)
	}

	if opts.Verbose && !opts.JSON {
		fmt.Printf("backport commits: %d\n", len(backportCommits))
		fmt.Printf("main commits:     %d\n", len(mainCommits))
		fmt.Println()
	}

	results, unmatchedMain := matchCommits(backportCommits, mainCommits)
	printReport(results, unmatchedMain, ReportOptions{
		Verbose:      opts.Verbose,
		CheckMissing: opts.CheckMissing,
		JSON:         opts.JSON,
	})

	for _, r := range results {
		if r.Status != StatusMatch {
			return 2, nil
		}
	}
	if opts.CheckMissing && len(unmatchedMain) > 0 {
		return 2, nil
	}
	return 0, nil
}

func validateOptions(opts Options) error {
	hasBranch := opts.BackportBranch != ""
	hasRange := opts.BackportRange != ""

	if !hasBranch && !hasRange {
		return fmt.Errorf("one of --backport-branch or --backport-range is required")
	}
	if hasBranch && hasRange {
		return fmt.Errorf("--backport-branch and --backport-range are mutually exclusive")
	}
	if hasBranch && opts.BaseBranch == "" {
		return fmt.Errorf("--base-branch is required when using --backport-branch (e.g. --base-branch release-4.18)")
	}
	if hasRange && opts.MainRange == "" {
		return fmt.Errorf("--main-range is required when using --backport-range")
	}
	return nil
}

func getCommitsFromRange(gitRange string) ([]CommitInfo, error) {
	if strings.TrimSpace(gitRange) == "" {
		return nil, fmt.Errorf("empty git range")
	}
	patchIDs, err := getPatchIDs(gitRange)
	if err != nil {
		return nil, fmt.Errorf("computing patch-ids: %w", err)
	}

	messages, err := getMessages(gitRange)
	if err != nil {
		return nil, fmt.Errorf("getting messages: %w", err)
	}

	hashes, err := getHashes(gitRange)
	if err != nil {
		return nil, fmt.Errorf("getting hashes: %w", err)
	}

	var commits []CommitInfo
	for _, hash := range hashes {
		ci := CommitInfo{
			Hash:    hash,
			PatchID: patchIDs[hash],
			Message: messages[hash],
		}
		commits = append(commits, ci)
	}
	return commits, nil
}

func getHashes(gitRange string) ([]string, error) {
	cmd := exec.Command("git", "log", "--no-merges", "--format=%H", gitRange, "--")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w: %s", err, cmdStderr(err))
	}

	var hashes []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			hashes = append(hashes, line)
		}
	}
	return hashes, nil
}

func getPatchIDs(gitRange string) (map[string]string, error) {
	logCmd := exec.Command("git", "log", "-p", "--no-merges", gitRange, "--")
	patchIDCmd := exec.Command("git", "patch-id", "--stable")

	pipe, err := logCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating pipe: %w", err)
	}
	patchIDCmd.Stdin = pipe

	var out bytes.Buffer
	patchIDCmd.Stdout = &out

	if err := logCmd.Start(); err != nil {
		return nil, fmt.Errorf("starting git log: %w", err)
	}
	if err := patchIDCmd.Start(); err != nil {
		return nil, fmt.Errorf("starting git patch-id: %w", err)
	}

	logErr := logCmd.Wait()
	patchIDErr := patchIDCmd.Wait()
	if logErr != nil {
		return nil, fmt.Errorf("git log: %w: %s", logErr, cmdStderr(logErr))
	}
	if patchIDErr != nil {
		return nil, fmt.Errorf("git patch-id: %w: %s", patchIDErr, cmdStderr(patchIDErr))
	}

	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		patchID, commitHash := parts[0], parts[1]
		result[commitHash] = patchID
	}
	return result, nil
}

func getMessages(gitRange string) (map[string]string, error) {
	cmd := exec.Command("git", "log", "--no-merges", "--format=%H%x00%B%x00", gitRange, "--")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log messages: %w: %s", err, cmdStderr(err))
	}

	result := make(map[string]string)
	records := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(records); i += 2 {
		hash := strings.TrimSpace(records[i])
		message := records[i+1]
		if hash == "" {
			continue
		}
		result[hash] = message
	}
	return result, nil
}

func matchCommits(backport, main []CommitInfo) ([]MatchResult, []CommitInfo) {
	mainByPatchID := make(map[string]*CommitInfo)
	mainBySubject := make(map[string]*CommitInfo)
	for i := range main {
		if main[i].PatchID != "" {
			mainByPatchID[main[i].PatchID] = &main[i]
		}
		subj := main[i].Subject()
		if subj != "" {
			mainBySubject[subj] = &main[i]
		}
	}

	matchedMainPatchIDs := make(map[string]bool)
	var results []MatchResult

	for _, bp := range backport {
		r := MatchResult{Backport: bp}

		if bp.PatchID == "" {
			r.Status = StatusUnmatched
			r.ClosestMatch = mainBySubject[bp.Subject()]
			results = append(results, r)
			continue
		}

		mainCommit, found := mainByPatchID[bp.PatchID]
		if !found {
			r.Status = StatusUnmatched
			r.ClosestMatch = mainBySubject[bp.Subject()]
			results = append(results, r)
			continue
		}

		matchedMainPatchIDs[bp.PatchID] = true
		r.MainMatch = mainCommit

		if strings.Contains(normalizeMessage(bp.Message), normalizeMessage(mainCommit.Message)) {
			r.Status = StatusMatch
		} else {
			r.Status = StatusMismatchMessage
		}
		results = append(results, r)
	}

	var unmatchedMain []CommitInfo
	for _, mc := range main {
		if mc.PatchID != "" && !matchedMainPatchIDs[mc.PatchID] {
			unmatchedMain = append(unmatchedMain, mc)
		}
	}

	return results, unmatchedMain
}

func printReport(results []MatchResult, unmatchedMain []CommitInfo, opts ReportOptions) {
	if opts.JSON {
		printJSONReport(results, unmatchedMain, opts)
		return
	}
	matchCount := 0
	for _, r := range results {
		switch r.Status {
		case StatusMatch:
			matchCount++
			fmt.Printf("%s %s %s  <-->  %s\n",
				statusColor(r.Status).Fmt("%-18s", r.Status.String()+":"),
				dim("%s", r.Backport.ShortHash()),
				r.Backport.Subject(),
				dim("%s", r.MainMatch.ShortHash()),
			)
		case StatusMismatchMessage:
			fmt.Printf("%s %s %s  <-->  %s\n",
				statusColor(r.Status).Fmt("%-18s", r.Status.String()+":"),
				dim("%s", r.Backport.ShortHash()),
				r.Backport.Subject(),
				dim("%s", r.MainMatch.ShortHash()),
			)
			if opts.Verbose {
				fmt.Printf("  main message:\n")
				printIndented(normalizeMessage(r.MainMatch.Message))
				fmt.Printf("  backport message:\n")
				printIndented(normalizeMessage(r.Backport.Message))
			}
		case StatusUnmatched:
			if r.ClosestMatch != nil {
				pc := comparePatchContent(r.Backport.Hash, r.ClosestMatch.Hash)
				fmt.Printf("%s %s %s  (~>  %s)  %s\n",
					statusColor(r.Status).Fmt("%-18s", r.Status.String()+":"),
					dim("%s", r.Backport.ShortHash()),
					r.Backport.Subject(),
					dim("%s", r.ClosestMatch.ShortHash()),
					pc.summaryTag(),
				)
				printPatchComparison(pc, opts)
			} else {
				fmt.Printf("%s %s %s\n",
					statusColor(r.Status).Fmt("%-18s", r.Status.String()+":"),
					dim("%s", r.Backport.ShortHash()),
					r.Backport.Subject(),
				)
			}
		}
	}

	if opts.CheckMissing {
		for _, mc := range unmatchedMain {
			fmt.Printf("%s %s %s\n",
				red("%-18s", "MISSING-BACKPORT:"),
				dim("%s", mc.ShortHash()), mc.Subject())
		}
	}

	allMatch := matchCount == len(results) && (!opts.CheckMissing || len(unmatchedMain) == 0)
	summaryFn := red
	if allMatch {
		summaryFn = green
	}
	fmt.Printf("\n%s", summaryFn("summary: %d/%d backport commits matched", matchCount, len(results)))
	if opts.CheckMissing && len(unmatchedMain) > 0 {
		fmt.Printf(", %s", red("%d main commits missing backport", len(unmatchedMain)))
	}
	fmt.Println()
}

type colorizer func(string, ...any) string

func (c colorizer) Fmt(format string, v ...any) string { return c(format, v...) }

func statusColor(s MatchStatus) colorizer {
	switch s {
	case StatusMatch:
		return green
	case StatusMismatchMessage, StatusUnmatched:
		return red
	default:
		return fmt.Sprintf
	}
}

func normalizeMessage(msg string) string {
	lines := strings.Split(msg, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	result := strings.Join(lines, "\n")
	return strings.TrimRight(result, "\n")
}

type filePatch struct {
	path         string
	addedLines   []string
	removedLines []string
}

type changeType int

const (
	changeIdentical changeType = iota
	changeMoved
	changeAdditive
	changeEdits
)

type fileResult struct {
	bpPath     string
	mainPath   string
	relation   string // COMMON, RENAMED
	changeType changeType
	overlap    int
	onlyBP     []string
	onlyMain   []string
}

type moveResult struct {
	bpPath   string
	mainPath string
	overlap  int      // percentage
	onlyBP   []string // residual lines not in the other
	onlyMain []string
}

func (fr fileResult) label() string {
	path := fr.bpPath
	if fr.mainPath != "" && fr.relation == "RENAMED" {
		path = fmt.Sprintf("%s -> %s", fr.mainPath, fr.bpPath)
	}

	bpCount := len(fr.onlyBP)
	mainCount := len(fr.onlyMain)

	switch fr.changeType {
	case changeIdentical:
		if fr.relation == "RENAMED" {
			return fmt.Sprintf("%s (renamed)", path)
		}
		return path
	case changeAdditive:
		if bpCount > 0 && mainCount == 0 {
			return fmt.Sprintf("%s (+%d only in backport)", path, bpCount)
		}
		if mainCount > 0 && bpCount == 0 {
			return fmt.Sprintf("%s (+%d only in main)", path, mainCount)
		}
		return path
	case changeMoved:
		return fmt.Sprintf("%s (%d%% overlap)", path, fr.overlap)
	case changeEdits:
		return fmt.Sprintf("%s (+%d only in backport, +%d only in main)", path, bpCount, mainCount)
	default:
		return path
	}
}

type patchComparison struct {
	identical []fileResult
	moved     []moveResult
	additive  []fileResult
	edits     []fileResult
	err       error
}

func (pc patchComparison) summaryTag() string {
	parts := []string{}
	if len(pc.identical) > 0 {
		parts = append(parts, dim("%d identical", len(pc.identical)))
	}
	if len(pc.moved) > 0 {
		parts = append(parts, dim("%d moved", len(pc.moved)))
	}
	if len(pc.additive) > 0 {
		parts = append(parts, yellow("%d additive", len(pc.additive)))
	}
	if len(pc.edits) > 0 {
		parts = append(parts, yellow("%d edits", len(pc.edits)))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("[%s]", strings.Join(parts, ", "))
}

func comparePatchContent(backportHash, mainHash string) patchComparison {
	bpPatch, err := getCommitPatch(backportHash)
	if err != nil {
		return patchComparison{err: fmt.Errorf("backport patch: %w", err)}
	}
	mainPatch, err := getCommitPatch(mainHash)
	if err != nil {
		return patchComparison{err: fmt.Errorf("main patch: %w", err)}
	}
	return comparePatches(bpPatch, mainPatch)
}

func comparePatches(bpPatch, mainPatch string) patchComparison {
	bpFiles := parsePatchFiles(bpPatch)
	mainFiles := parsePatchFiles(mainPatch)

	bpByPath := make(map[string]filePatch)
	bpByBase := make(map[string]filePatch)
	for _, fp := range bpFiles {
		bpByPath[fp.path] = fp
		bpByBase[filepath.Base(fp.path)] = fp
	}

	mainByPath := make(map[string]filePatch)
	mainByBase := make(map[string]filePatch)
	for _, fp := range mainFiles {
		mainByPath[fp.path] = fp
		mainByBase[filepath.Base(fp.path)] = fp
	}

	type pairedFile struct {
		bpPath   string
		mainPath string
		relation string
		bpFile   filePatch
		mainFile filePatch
	}

	var paired []pairedFile
	matchedBP := make(map[string]bool)
	matchedMain := make(map[string]bool)

	for _, bp := range bpFiles {
		if mf, ok := mainByPath[bp.path]; ok {
			paired = append(paired, pairedFile{
				bpPath: bp.path, mainPath: mf.path,
				relation: "COMMON", bpFile: bp, mainFile: mf,
			})
			matchedBP[bp.path] = true
			matchedMain[mf.path] = true
			continue
		}
		base := filepath.Base(bp.path)
		if mf, ok := mainByBase[base]; ok && !matchedMain[mf.path] {
			paired = append(paired, pairedFile{
				bpPath: bp.path, mainPath: mf.path,
				relation: "RENAMED", bpFile: bp, mainFile: mf,
			})
			matchedBP[bp.path] = true
			matchedMain[mf.path] = true
		}
	}

	var result patchComparison

	// Classify paired files
	for _, p := range paired {
		onlyBP, onlyMain := diffLinesSets(p.bpFile, p.mainFile)
		fr := fileResult{
			bpPath:   p.bpPath,
			mainPath: p.mainPath,
			relation: p.relation,
			onlyBP:   onlyBP,
			onlyMain: onlyMain,
		}

		switch {
		case len(onlyBP) == 0 && len(onlyMain) == 0:
			fr.changeType = changeIdentical
			result.identical = append(result.identical, fr)
		case len(onlyBP) > 0 && len(onlyMain) == 0,
			len(onlyBP) == 0 && len(onlyMain) > 0:
			fr.changeType = changeAdditive
			result.additive = append(result.additive, fr)
		default:
			fr.changeType = changeEdits
			result.edits = append(result.edits, fr)
		}
	}

	// Collect unpaired files
	var unpairedBP []filePatch
	var unpairedMain []filePatch
	for _, bp := range bpFiles {
		if !matchedBP[bp.path] {
			unpairedBP = append(unpairedBP, bp)
		}
	}
	for _, mf := range mainFiles {
		if !matchedMain[mf.path] {
			unpairedMain = append(unpairedMain, mf)
		}
	}

	// Detect code movement between unpaired files
	matchedUnpairedBP := make(map[string]bool)
	matchedUnpairedMain := make(map[string]bool)

	for _, bp := range unpairedBP {
		bpSet := toSet(bp.addedLines)
		for _, mf := range unpairedMain {
			if matchedUnpairedMain[mf.path] {
				continue
			}
			mainSet := toSet(mf.addedLines)
			overlapCount := intersectionSize(bpSet, mainSet)
			minSize := min(len(bp.addedLines), len(mf.addedLines))
			if minSize == 0 {
				continue
			}
			overlapPct := (overlapCount * 100) / minSize
			if overlapPct >= 50 {
				// Code movement detected
				onlyBP, onlyMain := diffLinesSets(bp, mf)
				result.moved = append(result.moved, moveResult{
					bpPath:   bp.path,
					mainPath: mf.path,
					overlap:  overlapPct,
					onlyBP:   onlyBP,
					onlyMain: onlyMain,
				})
				matchedUnpairedBP[bp.path] = true
				matchedUnpairedMain[mf.path] = true
				break
			}
		}
	}

	// Remaining unpaired files are edits (files added/removed, no movement)
	for _, bp := range unpairedBP {
		if !matchedUnpairedBP[bp.path] {
			result.edits = append(result.edits, fileResult{
				bpPath:     bp.path,
				changeType: changeEdits,
				onlyBP:     extractAddRemove(bp),
			})
		}
	}
	for _, mf := range unpairedMain {
		if !matchedUnpairedMain[mf.path] {
			result.edits = append(result.edits, fileResult{
				mainPath:   mf.path,
				changeType: changeEdits,
				onlyMain:   extractAddRemove(mf),
			})
		}
	}

	return result
}

func intersectionSize(a, b map[string]bool) int {
	count := 0
	for k := range a {
		if b[k] {
			count++
		}
	}
	return count
}

func extractAddRemove(fp filePatch) []string {
	var result []string
	for _, line := range fp.addedLines {
		result = append(result, "+ "+line)
	}
	for _, line := range fp.removedLines {
		result = append(result, "- "+line)
	}
	sort.Strings(result)
	return result
}

func printPatchComparison(pc patchComparison, opts ReportOptions) {
	if pc.err != nil {
		fmt.Printf("  (could not compare patches: %v)\n", pc.err)
		return
	}

	if len(pc.identical) > 0 {
		fmt.Printf("  %s\n", dim("identical (%d):", len(pc.identical)))
		for _, f := range pc.identical {
			fmt.Printf("    %s\n", dim("%s", f.label()))
		}
	}

	movedFiles := make([]fileResult, 0, len(pc.moved))
	for _, m := range pc.moved {
		movedFiles = append(movedFiles, fileResult{
			bpPath:     m.bpPath,
			mainPath:   m.mainPath,
			relation:   "RENAMED",
			changeType: changeMoved,
			overlap:    m.overlap,
			onlyBP:     m.onlyBP,
			onlyMain:   m.onlyMain,
		})
	}
	printFileResults("moved", dim, movedFiles, opts.Verbose)
	printFileResults("additive", yellow, pc.additive, opts.Verbose)
	printFileResults("edits", yellow, pc.edits, opts.Verbose)
}

func printFileResults(title string, colorFn colorizer, files []fileResult, verbose bool) {
	if len(files) == 0 {
		return
	}
	fmt.Printf("  %s\n", colorFn("%s (%d):", title, len(files)))
	for _, f := range files {
		fmt.Printf("    %s\n", colorFn("%s", f.label()))
		if !verbose {
			continue
		}
		if len(f.onlyBP) > 0 {
			fmt.Printf("      only in backport:\n")
			for _, line := range f.onlyBP {
				fmt.Printf("        %s\n", line)
			}
		}
		if len(f.onlyMain) > 0 {
			fmt.Printf("      only in main:\n")
			for _, line := range f.onlyMain {
				fmt.Printf("        %s\n", line)
			}
		}
	}
}

type jsonReport struct {
	Summary          jsonSummary  `json:"summary"`
	Results          []jsonResult `json:"results"`
	MissingBackports []jsonCommit `json:"missing_backports,omitempty"`
}

type jsonSummary struct {
	Matched       int `json:"matched"`
	Total         int `json:"total"`
	UnmatchedMain int `json:"unmatched_main,omitempty"`
}

type jsonCommit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

type jsonResult struct {
	Status          string            `json:"status"`
	Backport        jsonCommit        `json:"backport"`
	Main            *jsonCommit       `json:"main,omitempty"`
	Closest         *jsonCommit       `json:"closest,omitempty"`
	PatchComparison *jsonPatchSummary `json:"patch_comparison,omitempty"`
}

type jsonPatchSummary struct {
	Identical int `json:"identical"`
	Moved     int `json:"moved"`
	Additive  int `json:"additive"`
	Edits     int `json:"edits"`
}

func printJSONReport(results []MatchResult, unmatchedMain []CommitInfo, opts ReportOptions) {
	report := buildJSONReport(results, unmatchedMain, opts)
	json.NewEncoder(os.Stdout).Encode(report) //nolint:errcheck
}

func buildJSONReport(results []MatchResult, unmatchedMain []CommitInfo, opts ReportOptions) jsonReport {
	report := jsonReport{
		Summary: jsonSummary{Total: len(results)},
	}

	for _, r := range results {
		jr := jsonResult{
			Status:   r.Status.String(),
			Backport: jsonCommit{Hash: r.Backport.Hash, Subject: r.Backport.Subject()},
		}

		switch r.Status {
		case StatusMatch:
			report.Summary.Matched++
			jr.Main = &jsonCommit{Hash: r.MainMatch.Hash, Subject: r.MainMatch.Subject()}
		case StatusMismatchMessage:
			jr.Main = &jsonCommit{Hash: r.MainMatch.Hash, Subject: r.MainMatch.Subject()}
		case StatusUnmatched:
			if r.ClosestMatch != nil {
				jr.Closest = &jsonCommit{Hash: r.ClosestMatch.Hash, Subject: r.ClosestMatch.Subject()}
				pc := comparePatchContent(r.Backport.Hash, r.ClosestMatch.Hash)
				if pc.err == nil {
					jr.PatchComparison = &jsonPatchSummary{
						Identical: len(pc.identical),
						Moved:     len(pc.moved),
						Additive:  len(pc.additive),
						Edits:     len(pc.edits),
					}
				}
			}
		}

		report.Results = append(report.Results, jr)
	}

	if opts.CheckMissing && len(unmatchedMain) > 0 {
		report.Summary.UnmatchedMain = len(unmatchedMain)
		for _, mc := range unmatchedMain {
			report.MissingBackports = append(report.MissingBackports, jsonCommit{
				Hash:    mc.Hash,
				Subject: mc.Subject(),
			})
		}
	}

	return report
}

func getCommitPatch(hash string) (string, error) {
	if strings.TrimSpace(hash) == "" {
		return "", fmt.Errorf("empty commit hash")
	}
	cmd := exec.Command("git", "diff-tree", "-p", hash, "--")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff-tree: %w: %s", err, cmdStderr(err))
	}
	return string(out), nil
}

func parsePatchFiles(patch string) []filePatch {
	chunks := strings.Split(patch, "diff --git ")
	var files []filePatch
	for _, chunk := range chunks[1:] {
		fp := filePatch{}
		lines := strings.Split(chunk, "\n")
		if len(lines) > 0 {
			parts := strings.Fields(lines[0])
			if len(parts) >= 2 {
				fp.path = strings.TrimPrefix(parts[1], "b/")
			}
		}
		for _, line := range lines {
			if len(line) == 0 {
				continue
			}
			if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
				continue
			}
			if line[0] == '+' {
				fp.addedLines = append(fp.addedLines, strings.TrimSpace(line[1:]))
			} else if line[0] == '-' {
				fp.removedLines = append(fp.removedLines, strings.TrimSpace(line[1:]))
			}
		}
		files = append(files, fp)
	}
	return files
}

func diffLinesSets(bp, main filePatch) (onlyBP, onlyMain []string) {
	bpAdded := toSet(bp.addedLines)
	mainAdded := toSet(main.addedLines)
	bpRemoved := toSet(bp.removedLines)
	mainRemoved := toSet(main.removedLines)

	for line := range bpAdded {
		if !mainAdded[line] {
			onlyBP = append(onlyBP, "+ "+line)
		}
	}
	for line := range bpRemoved {
		if !mainRemoved[line] {
			onlyBP = append(onlyBP, "- "+line)
		}
	}
	for line := range mainAdded {
		if !bpAdded[line] {
			onlyMain = append(onlyMain, "+ "+line)
		}
	}
	for line := range mainRemoved {
		if !bpRemoved[line] {
			onlyMain = append(onlyMain, "- "+line)
		}
	}

	sort.Strings(onlyBP)
	sort.Strings(onlyMain)
	return onlyBP, onlyMain
}

func toSet(lines []string) map[string]bool {
	s := make(map[string]bool, len(lines))
	for _, l := range lines {
		s[l] = true
	}
	return s
}

func printIndented(text string) {
	for _, line := range strings.Split(text, "\n") {
		fmt.Printf("    %s\n", line)
	}
}

func cmdStderr(err error) string {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return strings.TrimSpace(string(exitErr.Stderr))
	}
	return ""
}

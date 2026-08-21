// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func TestBuildJSONReportGolden(t *testing.T) {
	tests := []struct {
		name          string
		results       []MatchResult
		unmatchedMain []CommitInfo
		opts          ReportOptions
		goldenFile    string
	}{
		{
			name: "all matched",
			results: []MatchResult{
				{
					Backport:  CommitInfo{Hash: "aaa1111", Message: "fix: alpha\n\nSigned-off-by: dev"},
					MainMatch: &CommitInfo{Hash: "bbb2222", Message: "fix: alpha\n\nSigned-off-by: dev"},
					Status:    StatusMatch,
				},
				{
					Backport:  CommitInfo{Hash: "ccc3333", Message: "fix: beta"},
					MainMatch: &CommitInfo{Hash: "ddd4444", Message: "fix: beta"},
					Status:    StatusMatch,
				},
			},
			opts:       ReportOptions{},
			goldenFile: "json_all_matched.golden.json",
		},
		{
			name: "mismatch message",
			results: []MatchResult{
				{
					Backport:  CommitInfo{Hash: "aaa1111", Message: "fix: alpha (backport note)"},
					MainMatch: &CommitInfo{Hash: "bbb2222", Message: "fix: alpha"},
					Status:    StatusMismatchMessage,
				},
			},
			opts:       ReportOptions{},
			goldenFile: "json_mismatch_message.golden.json",
		},
		{
			name: "unmatched no closest",
			results: []MatchResult{
				{
					Backport: CommitInfo{Hash: "aaa1111", Message: "fix: orphan"},
					Status:   StatusUnmatched,
				},
			},
			opts:       ReportOptions{},
			goldenFile: "json_unmatched_no_closest.golden.json",
		},
		{
			name: "missing backports",
			results: []MatchResult{
				{
					Backport:  CommitInfo{Hash: "aaa1111", Message: "fix: alpha"},
					MainMatch: &CommitInfo{Hash: "bbb2222", Message: "fix: alpha"},
					Status:    StatusMatch,
				},
			},
			unmatchedMain: []CommitInfo{
				{Hash: "eee5555", Message: "fix: missing thing\n\nDetails"},
				{Hash: "fff6666", Message: "fix: another missing"},
			},
			opts:       ReportOptions{CheckMissing: true},
			goldenFile: "json_missing_backports.golden.json",
		},
		{
			name: "missing backports not reported without flag",
			results: []MatchResult{
				{
					Backport:  CommitInfo{Hash: "aaa1111", Message: "fix: alpha"},
					MainMatch: &CommitInfo{Hash: "bbb2222", Message: "fix: alpha"},
					Status:    StatusMatch,
				},
			},
			unmatchedMain: []CommitInfo{
				{Hash: "eee5555", Message: "fix: missing thing"},
			},
			opts:       ReportOptions{CheckMissing: false},
			goldenFile: "json_missing_not_reported.golden.json",
		},
		{
			name: "mixed statuses",
			results: []MatchResult{
				{
					Backport:  CommitInfo{Hash: "aaa1111", Message: "fix: alpha"},
					MainMatch: &CommitInfo{Hash: "bbb2222", Message: "fix: alpha"},
					Status:    StatusMatch,
				},
				{
					Backport:  CommitInfo{Hash: "ccc3333", Message: "fix: beta (reworded)"},
					MainMatch: &CommitInfo{Hash: "ddd4444", Message: "fix: beta"},
					Status:    StatusMismatchMessage,
				},
				{
					Backport: CommitInfo{Hash: "eee5555", Message: "fix: gamma"},
					Status:   StatusUnmatched,
				},
			},
			opts:       ReportOptions{},
			goldenFile: "json_mixed_statuses.golden.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := buildJSONReport(tt.results, tt.unmatchedMain, tt.opts)

			got, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				t.Fatalf("json.MarshalIndent: %v", err)
			}
			got = append(got, '\n')

			goldenPath := filepath.Join("testdata", tt.goldenFile)

			if *update {
				if err := os.WriteFile(goldenPath, got, 0644); err != nil {
					t.Fatalf("writing golden file: %v", err)
				}
				t.Logf("updated %s", goldenPath)
				return
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("reading golden file %s: %v (run with -update to create)", goldenPath, err)
			}

			if string(got) != string(want) {
				t.Errorf("JSON output mismatch (run with -update to refresh)\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestBuildJSONReportGoldenFromGitHistory(t *testing.T) {
	skipIfNoGit(t)

	t.Run("matched commits from real history", func(t *testing.T) {
		if !commitExists(commitMakefileHash) || !commitExists(commitDockerfileHash) {
			t.Skip("required commits not found")
		}

		makefileMsg := getCommitMessage(t, commitMakefileHash)
		dockerfileMsg := getCommitMessage(t, commitDockerfileHash)

		results := []MatchResult{
			{
				Backport:  CommitInfo{Hash: commitMakefileHash, PatchID: commitMakefilePatchID, Message: makefileMsg},
				MainMatch: &CommitInfo{Hash: commitMakefileHash, PatchID: commitMakefilePatchID, Message: makefileMsg},
				Status:    StatusMatch,
			},
			{
				Backport:  CommitInfo{Hash: commitDockerfileHash, PatchID: commitDockerfilePatchID, Message: dockerfileMsg},
				MainMatch: &CommitInfo{Hash: commitDockerfileHash, PatchID: commitDockerfilePatchID, Message: dockerfileMsg},
				Status:    StatusMatch,
			},
		}

		report := buildJSONReport(results, nil, ReportOptions{})

		got, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatalf("json.MarshalIndent: %v", err)
		}
		got = append(got, '\n')

		goldenPath := filepath.Join("testdata", "json_githistory_matched.golden.json")

		if *update {
			if err := os.WriteFile(goldenPath, got, 0644); err != nil {
				t.Fatalf("writing golden file: %v", err)
			}
			t.Logf("updated %s", goldenPath)
			return
		}

		want, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("reading golden file %s: %v (run with -update to create)", goldenPath, err)
		}

		if string(got) != string(want) {
			t.Errorf("JSON output mismatch (run with -update to refresh)\ngot:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("cherry-pick unmatched with closest match", func(t *testing.T) {
		if !commitExists(commitCherryPickHash) || !commitExists(commitOriginalHash) {
			t.Skip("cherry-pick or original commit not found")
		}

		cpMsg := getCommitMessage(t, commitCherryPickHash)
		origMsg := getCommitMessage(t, commitOriginalHash)

		results := []MatchResult{
			{
				Backport:     CommitInfo{Hash: commitCherryPickHash, PatchID: commitCherryPickPatchID, Message: cpMsg},
				ClosestMatch: &CommitInfo{Hash: commitOriginalHash, PatchID: commitOriginalPatchID, Message: origMsg},
				Status:       StatusUnmatched,
			},
		}

		report := buildJSONReport(results, nil, ReportOptions{})

		got, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatalf("json.MarshalIndent: %v", err)
		}
		got = append(got, '\n')

		goldenPath := filepath.Join("testdata", "json_githistory_unmatched_closest.golden.json")

		if *update {
			if err := os.WriteFile(goldenPath, got, 0644); err != nil {
				t.Fatalf("writing golden file: %v", err)
			}
			t.Logf("updated %s", goldenPath)
			return
		}

		want, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("reading golden file %s: %v (run with -update to create)", goldenPath, err)
		}

		if string(got) != string(want) {
			t.Errorf("JSON output mismatch (run with -update to refresh)\ngot:\n%s\nwant:\n%s", got, want)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

package main

import (
	"reflect"
	"sort"
	"testing"
)

func TestCommitInfoShortHash(t *testing.T) {
	tests := []struct {
		name string
		hash string
		want string
	}{
		{name: "normal", hash: "abc1234def5678", want: "abc1234"},
		{name: "exactly seven", hash: "abc1234", want: "abc1234"},
		{name: "shorter than seven", hash: "abc", want: "abc"},
		{name: "empty", hash: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ci := CommitInfo{Hash: tt.hash}
			if got := ci.ShortHash(); got != tt.want {
				t.Errorf("ShortHash() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommitInfoSubject(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "single line", message: "fix: thing", want: "fix: thing"},
		{name: "multi line", message: "fix: thing\n\nLong description here", want: "fix: thing"},
		{name: "empty", message: "", want: ""},
		{name: "newline only", message: "\n", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ci := CommitInfo{Message: tt.message}
			if got := ci.Subject(); got != tt.want {
				t.Errorf("Subject() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{name: "clean", msg: "hello\nworld", want: "hello\nworld"},
		{name: "trailing spaces", msg: "hello   \nworld\t\t", want: "hello\nworld"},
		{name: "trailing newlines", msg: "hello\nworld\n\n\n", want: "hello\nworld"},
		{name: "both", msg: "hello   \nworld  \n\n", want: "hello\nworld"},
		{name: "empty", msg: "", want: ""},
		{name: "only whitespace", msg: "   \n\t\n\n", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMessage(tt.msg); got != tt.want {
				t.Errorf("normalizeMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{
			name:    "valid branch mode",
			opts:    Options{BackportBranch: "pr-1234", BaseBranch: "release-4.18"},
			wantErr: false,
		},
		{
			name:    "valid range mode",
			opts:    Options{BackportRange: "a..b", MainRange: "c..d"},
			wantErr: false,
		},
		{
			name:    "neither branch nor range",
			opts:    Options{},
			wantErr: true,
		},
		{
			name:    "both branch and range",
			opts:    Options{BackportBranch: "pr-1234", BackportRange: "a..b"},
			wantErr: true,
		},
		{
			name:    "branch without base",
			opts:    Options{BackportBranch: "pr-1234"},
			wantErr: true,
		},
		{
			name:    "range without main-range",
			opts:    Options{BackportRange: "a..b"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOptions(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMatchCommits(t *testing.T) {
	t.Run("patch-id match with identical messages", func(t *testing.T) {
		bp := []CommitInfo{{Hash: "aaa", PatchID: "p1", Message: "fix: thing"}}
		main := []CommitInfo{{Hash: "bbb", PatchID: "p1", Message: "fix: thing"}}

		results, unmatched := matchCommits(bp, main)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Status != StatusMatch {
			t.Errorf("status = %v, want StatusMatch", results[0].Status)
		}
		if results[0].MainMatch == nil || results[0].MainMatch.Hash != "bbb" {
			t.Errorf("MainMatch not set correctly")
		}
		if len(unmatched) != 0 {
			t.Errorf("expected 0 unmatched main, got %d", len(unmatched))
		}
	})

	t.Run("patch-id match with backport adding notes", func(t *testing.T) {
		bp := []CommitInfo{{Hash: "aaa", PatchID: "p1", Message: "fix: thing\n\nBackport note\nfix: thing"}}
		main := []CommitInfo{{Hash: "bbb", PatchID: "p1", Message: "fix: thing"}}

		results, _ := matchCommits(bp, main)

		if results[0].Status != StatusMatch {
			t.Errorf("status = %v, want StatusMatch (message containment)", results[0].Status)
		}
	})

	t.Run("patch-id match with diverged messages", func(t *testing.T) {
		bp := []CommitInfo{{Hash: "aaa", PatchID: "p1", Message: "completely different message"}}
		main := []CommitInfo{{Hash: "bbb", PatchID: "p1", Message: "original message"}}

		results, _ := matchCommits(bp, main)

		if results[0].Status != StatusMismatchMessage {
			t.Errorf("status = %v, want StatusMismatchMessage", results[0].Status)
		}
	})

	t.Run("no patch-id match with subject fallback", func(t *testing.T) {
		bp := []CommitInfo{{Hash: "aaa", PatchID: "p1", Message: "fix: thing"}}
		main := []CommitInfo{{Hash: "bbb", PatchID: "p2", Message: "fix: thing"}}

		results, unmatched := matchCommits(bp, main)

		if results[0].Status != StatusUnmatched {
			t.Errorf("status = %v, want StatusUnmatched", results[0].Status)
		}
		if results[0].ClosestMatch == nil || results[0].ClosestMatch.Hash != "bbb" {
			t.Errorf("ClosestMatch not set via subject fallback")
		}
		if len(unmatched) != 1 {
			t.Errorf("expected 1 unmatched main, got %d", len(unmatched))
		}
	})

	t.Run("no match at all", func(t *testing.T) {
		bp := []CommitInfo{{Hash: "aaa", PatchID: "p1", Message: "fix: alpha"}}
		main := []CommitInfo{{Hash: "bbb", PatchID: "p2", Message: "fix: beta"}}

		results, _ := matchCommits(bp, main)

		if results[0].Status != StatusUnmatched {
			t.Errorf("status = %v, want StatusUnmatched", results[0].Status)
		}
		if results[0].ClosestMatch != nil {
			t.Errorf("ClosestMatch should be nil when no subject match")
		}
	})

	t.Run("empty patch-id on backport", func(t *testing.T) {
		bp := []CommitInfo{{Hash: "aaa", PatchID: "", Message: "fix: thing"}}
		main := []CommitInfo{{Hash: "bbb", PatchID: "p1", Message: "fix: thing"}}

		results, _ := matchCommits(bp, main)

		if results[0].Status != StatusUnmatched {
			t.Errorf("status = %v, want StatusUnmatched for empty patch-id", results[0].Status)
		}
		if results[0].ClosestMatch == nil {
			t.Errorf("should still find closest match by subject")
		}
	})

	t.Run("multiple commits mixed statuses", func(t *testing.T) {
		bp := []CommitInfo{
			{Hash: "a1", PatchID: "p1", Message: "fix: alpha"},
			{Hash: "a2", PatchID: "p2", Message: "fix: beta"},
			{Hash: "a3", PatchID: "p3", Message: "fix: gamma"},
		}
		main := []CommitInfo{
			{Hash: "b1", PatchID: "p1", Message: "fix: alpha"},
			{Hash: "b2", PatchID: "p2", Message: "different message"},
			{Hash: "b4", PatchID: "p4", Message: "fix: delta"},
		}

		results, unmatched := matchCommits(bp, main)

		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}
		if results[0].Status != StatusMatch {
			t.Errorf("result[0] status = %v, want StatusMatch", results[0].Status)
		}
		if results[1].Status != StatusMismatchMessage {
			t.Errorf("result[1] status = %v, want StatusMismatchMessage", results[1].Status)
		}
		if results[2].Status != StatusUnmatched {
			t.Errorf("result[2] status = %v, want StatusUnmatched", results[2].Status)
		}
		if len(unmatched) != 1 || unmatched[0].Hash != "b4" {
			t.Errorf("expected 1 unmatched main (b4), got %v", unmatched)
		}
	})
}

func TestParsePatchFiles(t *testing.T) {
	t.Run("single file with adds and removes", func(t *testing.T) {
		patch := `commit abc1234
Author: Test User <test@example.com>

    fix: thing

diff --git a/pkg/foo.go b/pkg/foo.go
index 1234567..abcdefg 100644
--- a/pkg/foo.go
+++ b/pkg/foo.go
@@ -10,7 +10,7 @@ func Foo() {
-	old := getValue()
+	new := getValue()
 	return result
`
		files := parsePatchFiles(patch)

		if len(files) != 1 {
			t.Fatalf("expected 1 file, got %d", len(files))
		}
		if files[0].path != "pkg/foo.go" {
			t.Errorf("path = %q, want %q", files[0].path, "pkg/foo.go")
		}
		if len(files[0].addedLines) != 1 || files[0].addedLines[0] != "new := getValue()" {
			t.Errorf("addedLines = %v, want [\"new := getValue()\"]", files[0].addedLines)
		}
		if len(files[0].removedLines) != 1 || files[0].removedLines[0] != "old := getValue()" {
			t.Errorf("removedLines = %v, want [\"old := getValue()\"]", files[0].removedLines)
		}
	})

	t.Run("multi-file patch", func(t *testing.T) {
		patch := `diff --git a/file1.go b/file1.go
index 1111111..2222222 100644
--- a/file1.go
+++ b/file1.go
@@ -1,3 +1,3 @@
+added line in file1
diff --git a/pkg/file2.go b/pkg/file2.go
index 3333333..4444444 100644
--- a/pkg/file2.go
+++ b/pkg/file2.go
@@ -5,3 +5,4 @@
+added line in file2
-removed line in file2
`
		files := parsePatchFiles(patch)

		if len(files) != 2 {
			t.Fatalf("expected 2 files, got %d", len(files))
		}
		if files[0].path != "file1.go" {
			t.Errorf("file[0].path = %q, want %q", files[0].path, "file1.go")
		}
		if files[1].path != "pkg/file2.go" {
			t.Errorf("file[1].path = %q, want %q", files[1].path, "pkg/file2.go")
		}
		if len(files[1].addedLines) != 1 {
			t.Errorf("file[1] should have 1 added line, got %d", len(files[1].addedLines))
		}
		if len(files[1].removedLines) != 1 {
			t.Errorf("file[1] should have 1 removed line, got %d", len(files[1].removedLines))
		}
	})

	t.Run("skips +++ and --- headers", func(t *testing.T) {
		patch := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,3 @@
+real addition
`
		files := parsePatchFiles(patch)

		if len(files) != 1 {
			t.Fatalf("expected 1 file, got %d", len(files))
		}
		if len(files[0].addedLines) != 1 {
			t.Errorf("should have 1 added line (not count +++ header), got %d", len(files[0].addedLines))
		}
	})

	t.Run("whitespace trimming", func(t *testing.T) {
		patch := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,3 @@
+	indented := true
-	old := false
`
		files := parsePatchFiles(patch)

		if len(files) != 1 {
			t.Fatalf("expected 1 file, got %d", len(files))
		}
		if files[0].addedLines[0] != "indented := true" {
			t.Errorf("added line not trimmed: %q", files[0].addedLines[0])
		}
		if files[0].removedLines[0] != "old := false" {
			t.Errorf("removed line not trimmed: %q", files[0].removedLines[0])
		}
	})

	t.Run("empty patch", func(t *testing.T) {
		files := parsePatchFiles("")
		if len(files) != 0 {
			t.Errorf("expected 0 files from empty patch, got %d", len(files))
		}
	})

	t.Run("new file patch", func(t *testing.T) {
		patch := `diff --git a/newfile.go b/newfile.go
new file mode 100644
index 0000000..1234567
--- /dev/null
+++ b/newfile.go
@@ -0,0 +1,3 @@
+package main
+
+func New() {}
`
		files := parsePatchFiles(patch)

		if len(files) != 1 {
			t.Fatalf("expected 1 file, got %d", len(files))
		}
		if files[0].path != "newfile.go" {
			t.Errorf("path = %q, want %q", files[0].path, "newfile.go")
		}
		if len(files[0].addedLines) != 3 {
			t.Errorf("expected 3 added lines (blank line becomes empty string), got %d: %v", len(files[0].addedLines), files[0].addedLines)
		}
	})
}

func TestDiffLinesSets(t *testing.T) {
	t.Run("identical patches", func(t *testing.T) {
		bp := filePatch{addedLines: []string{"a", "b"}, removedLines: []string{"c"}}
		main := filePatch{addedLines: []string{"a", "b"}, removedLines: []string{"c"}}

		onlyBP, onlyMain := diffLinesSets(bp, main)

		if len(onlyBP) != 0 {
			t.Errorf("expected empty onlyBP, got %v", onlyBP)
		}
		if len(onlyMain) != 0 {
			t.Errorf("expected empty onlyMain, got %v", onlyMain)
		}
	})

	t.Run("added lines only in bp", func(t *testing.T) {
		bp := filePatch{addedLines: []string{"a", "b", "c"}}
		main := filePatch{addedLines: []string{"a"}}

		onlyBP, onlyMain := diffLinesSets(bp, main)

		if len(onlyBP) != 2 {
			t.Errorf("expected 2 onlyBP, got %v", onlyBP)
		}
		if len(onlyMain) != 0 {
			t.Errorf("expected 0 onlyMain, got %v", onlyMain)
		}
	})

	t.Run("removed lines only in main", func(t *testing.T) {
		bp := filePatch{removedLines: []string{"x"}}
		main := filePatch{removedLines: []string{"x", "y"}}

		onlyBP, onlyMain := diffLinesSets(bp, main)

		if len(onlyBP) != 0 {
			t.Errorf("expected 0 onlyBP, got %v", onlyBP)
		}
		if len(onlyMain) != 1 {
			t.Errorf("expected 1 onlyMain, got %v", onlyMain)
		}
	})

	t.Run("results are sorted", func(t *testing.T) {
		bp := filePatch{addedLines: []string{"z", "a", "m"}}
		main := filePatch{addedLines: []string{}}

		onlyBP, _ := diffLinesSets(bp, main)

		if !sort.StringsAreSorted(onlyBP) {
			t.Errorf("onlyBP not sorted: %v", onlyBP)
		}
	})

	t.Run("mixed adds and removes", func(t *testing.T) {
		bp := filePatch{
			addedLines:   []string{"new1", "common"},
			removedLines: []string{"old1", "oldcommon"},
		}
		main := filePatch{
			addedLines:   []string{"new2", "common"},
			removedLines: []string{"old2", "oldcommon"},
		}

		onlyBP, onlyMain := diffLinesSets(bp, main)

		expectBP := []string{"+ new1", "- old1"}
		expectMain := []string{"+ new2", "- old2"}
		if !reflect.DeepEqual(onlyBP, expectBP) {
			t.Errorf("onlyBP = %v, want %v", onlyBP, expectBP)
		}
		if !reflect.DeepEqual(onlyMain, expectMain) {
			t.Errorf("onlyMain = %v, want %v", onlyMain, expectMain)
		}
	})
}

func TestIntersectionSize(t *testing.T) {
	tests := []struct {
		name string
		a, b map[string]bool
		want int
	}{
		{name: "full overlap", a: map[string]bool{"x": true, "y": true}, b: map[string]bool{"x": true, "y": true}, want: 2},
		{name: "partial overlap", a: map[string]bool{"x": true, "y": true}, b: map[string]bool{"y": true, "z": true}, want: 1},
		{name: "no overlap", a: map[string]bool{"x": true}, b: map[string]bool{"y": true}, want: 0},
		{name: "empty sets", a: map[string]bool{}, b: map[string]bool{}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := intersectionSize(tt.a, tt.b); got != tt.want {
				t.Errorf("intersectionSize() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestToSet(t *testing.T) {
	t.Run("deduplicates", func(t *testing.T) {
		s := toSet([]string{"a", "b", "a"})
		if len(s) != 2 {
			t.Errorf("expected 2 entries, got %d", len(s))
		}
	})

	t.Run("empty", func(t *testing.T) {
		s := toSet(nil)
		if len(s) != 0 {
			t.Errorf("expected 0 entries, got %d", len(s))
		}
	})
}

func TestFileResultLabel(t *testing.T) {
	tests := []struct {
		name string
		fr   fileResult
		want string
	}{
		{
			name: "identical",
			fr:   fileResult{bpPath: "foo.go", changeType: changeIdentical},
			want: "foo.go",
		},
		{
			name: "identical renamed",
			fr:   fileResult{bpPath: "new/foo.go", mainPath: "old/foo.go", relation: "RENAMED", changeType: changeIdentical},
			want: "old/foo.go -> new/foo.go (renamed)",
		},
		{
			name: "moved with overlap",
			fr:   fileResult{bpPath: "new/foo.go", mainPath: "old/foo.go", relation: "RENAMED", changeType: changeMoved, overlap: 85},
			want: "old/foo.go -> new/foo.go (85% overlap)",
		},
		{
			name: "additive bp only",
			fr:   fileResult{bpPath: "foo.go", changeType: changeAdditive, onlyBP: []string{"a", "b"}},
			want: "foo.go (+2 only in backport)",
		},
		{
			name: "additive main only",
			fr:   fileResult{bpPath: "foo.go", changeType: changeAdditive, onlyMain: []string{"a"}},
			want: "foo.go (+1 only in main)",
		},
		{
			name: "edits both sides",
			fr:   fileResult{bpPath: "foo.go", changeType: changeEdits, onlyBP: []string{"a"}, onlyMain: []string{"b", "c"}},
			want: "foo.go (+1 only in backport, +2 only in main)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fr.label(); got != tt.want {
				t.Errorf("label() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchStatusString(t *testing.T) {
	tests := []struct {
		status MatchStatus
		want   string
	}{
		{StatusMatch, "MATCH"},
		{StatusMismatchMessage, "MISMATCH-MESSAGE"},
		{StatusUnmatched, "UNMATCHED"},
		{MatchStatus(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComparePatches(t *testing.T) {
	t.Run("identical single-file patches", func(t *testing.T) {
		patch := `diff --git a/pkg/foo.go b/pkg/foo.go
--- a/pkg/foo.go
+++ b/pkg/foo.go
@@ -1,3 +1,3 @@
-old := getValue()
+new := getValue()
`
		pc := comparePatches(patch, patch)

		if pc.err != nil {
			t.Fatalf("unexpected error: %v", pc.err)
		}
		if len(pc.identical) != 1 {
			t.Errorf("expected 1 identical, got %d", len(pc.identical))
		}
		if len(pc.additive) != 0 || len(pc.edits) != 0 || len(pc.moved) != 0 {
			t.Errorf("expected no additive/edits/moved, got additive=%d edits=%d moved=%d",
				len(pc.additive), len(pc.edits), len(pc.moved))
		}
	})

	t.Run("additive: backport has extra lines", func(t *testing.T) {
		bpPatch := `diff --git a/pkg/foo.go b/pkg/foo.go
--- a/pkg/foo.go
+++ b/pkg/foo.go
@@ -1,3 +1,5 @@
-old := getValue()
+new := getValue()
+extra := true
`
		mainPatch := `diff --git a/pkg/foo.go b/pkg/foo.go
--- a/pkg/foo.go
+++ b/pkg/foo.go
@@ -1,3 +1,3 @@
-old := getValue()
+new := getValue()
`
		pc := comparePatches(bpPatch, mainPatch)

		if pc.err != nil {
			t.Fatalf("unexpected error: %v", pc.err)
		}
		if len(pc.identical) != 0 {
			t.Errorf("expected 0 identical, got %d", len(pc.identical))
		}
		if len(pc.additive) != 1 {
			t.Errorf("expected 1 additive, got %d", len(pc.additive))
		}
	})

	t.Run("edits: both sides differ", func(t *testing.T) {
		bpPatch := `diff --git a/pkg/foo.go b/pkg/foo.go
--- a/pkg/foo.go
+++ b/pkg/foo.go
@@ -1,3 +1,3 @@
-old := getValue()
+bp := getValue()
`
		mainPatch := `diff --git a/pkg/foo.go b/pkg/foo.go
--- a/pkg/foo.go
+++ b/pkg/foo.go
@@ -1,3 +1,3 @@
-old := getValue()
+main := getValue()
`
		pc := comparePatches(bpPatch, mainPatch)

		if pc.err != nil {
			t.Fatalf("unexpected error: %v", pc.err)
		}
		if len(pc.edits) != 1 {
			t.Errorf("expected 1 edits, got %d", len(pc.edits))
		}
		if len(pc.identical) != 0 || len(pc.additive) != 0 {
			t.Errorf("expected no identical/additive, got identical=%d additive=%d",
				len(pc.identical), len(pc.additive))
		}
	})

	t.Run("file renamed with same content", func(t *testing.T) {
		bpPatch := `diff --git a/new/foo.go b/new/foo.go
--- a/new/foo.go
+++ b/new/foo.go
@@ -1,3 +1,3 @@
-old := getValue()
+new := getValue()
`
		mainPatch := `diff --git a/old/foo.go b/old/foo.go
--- a/old/foo.go
+++ b/old/foo.go
@@ -1,3 +1,3 @@
-old := getValue()
+new := getValue()
`
		pc := comparePatches(bpPatch, mainPatch)

		if pc.err != nil {
			t.Fatalf("unexpected error: %v", pc.err)
		}
		if len(pc.identical) != 1 {
			t.Errorf("expected 1 identical (renamed), got %d", len(pc.identical))
		}
		if pc.identical[0].relation != "RENAMED" {
			t.Errorf("relation = %q, want RENAMED", pc.identical[0].relation)
		}
	})

	t.Run("multi-file: mixed classification", func(t *testing.T) {
		bpPatch := `diff --git a/common.go b/common.go
--- a/common.go
+++ b/common.go
@@ -1,3 +1,3 @@
-old := 1
+new := 1
diff --git a/changed.go b/changed.go
--- a/changed.go
+++ b/changed.go
@@ -1,3 +1,3 @@
-x := 1
+bp_x := 1
`
		mainPatch := `diff --git a/common.go b/common.go
--- a/common.go
+++ b/common.go
@@ -1,3 +1,3 @@
-old := 1
+new := 1
diff --git a/changed.go b/changed.go
--- a/changed.go
+++ b/changed.go
@@ -1,3 +1,3 @@
-x := 1
+main_x := 1
`
		pc := comparePatches(bpPatch, mainPatch)

		if pc.err != nil {
			t.Fatalf("unexpected error: %v", pc.err)
		}
		if len(pc.identical) != 1 {
			t.Errorf("expected 1 identical (common.go), got %d", len(pc.identical))
		}
		if len(pc.edits) != 1 {
			t.Errorf("expected 1 edits (changed.go), got %d", len(pc.edits))
		}
	})

	t.Run("code movement detection", func(t *testing.T) {
		bpPatch := `diff --git a/newfile.go b/newfile.go
--- /dev/null
+++ b/newfile.go
@@ -0,0 +1,5 @@
+func Foo() {}
+func Bar() {}
+func Baz() {}
+func Qux() {}
`
		mainPatch := `diff --git a/oldfile.go b/oldfile.go
--- /dev/null
+++ b/oldfile.go
@@ -0,0 +1,5 @@
+func Foo() {}
+func Bar() {}
+func Baz() {}
+func Qux() {}
`
		pc := comparePatches(bpPatch, mainPatch)

		if pc.err != nil {
			t.Fatalf("unexpected error: %v", pc.err)
		}
		if len(pc.moved) != 1 {
			t.Errorf("expected 1 moved, got %d", len(pc.moved))
		}
		if len(pc.moved) == 1 && pc.moved[0].overlap != 100 {
			t.Errorf("overlap = %d%%, want 100%%", pc.moved[0].overlap)
		}
	})

	t.Run("code movement below threshold is edits", func(t *testing.T) {
		bpPatch := `diff --git a/newfile.go b/newfile.go
--- /dev/null
+++ b/newfile.go
@@ -0,0 +1,5 @@
+func Foo() {}
+func Completely() {}
+func Different() {}
+func Code() {}
`
		mainPatch := `diff --git a/oldfile.go b/oldfile.go
--- /dev/null
+++ b/oldfile.go
@@ -0,0 +1,5 @@
+func Bar() {}
+func Totally() {}
+func Other() {}
+func Stuff() {}
`
		pc := comparePatches(bpPatch, mainPatch)

		if pc.err != nil {
			t.Fatalf("unexpected error: %v", pc.err)
		}
		if len(pc.moved) != 0 {
			t.Errorf("expected 0 moved (below threshold), got %d", len(pc.moved))
		}
		if len(pc.edits) != 2 {
			t.Errorf("expected 2 edits (one per unpaired file), got %d", len(pc.edits))
		}
	})

	t.Run("file only in backport", func(t *testing.T) {
		bpPatch := `diff --git a/common.go b/common.go
--- a/common.go
+++ b/common.go
@@ -1,3 +1,3 @@
-old := 1
+new := 1
diff --git a/extra.go b/extra.go
--- /dev/null
+++ b/extra.go
@@ -0,0 +1,2 @@
+package extra
`
		mainPatch := `diff --git a/common.go b/common.go
--- a/common.go
+++ b/common.go
@@ -1,3 +1,3 @@
-old := 1
+new := 1
`
		pc := comparePatches(bpPatch, mainPatch)

		if pc.err != nil {
			t.Fatalf("unexpected error: %v", pc.err)
		}
		if len(pc.identical) != 1 {
			t.Errorf("expected 1 identical, got %d", len(pc.identical))
		}
		if len(pc.edits) != 1 {
			t.Errorf("expected 1 edits (extra.go only in bp), got %d", len(pc.edits))
		}
	})

	t.Run("empty patches", func(t *testing.T) {
		pc := comparePatches("", "")

		if pc.err != nil {
			t.Fatalf("unexpected error: %v", pc.err)
		}
		if len(pc.identical)+len(pc.moved)+len(pc.additive)+len(pc.edits) != 0 {
			t.Errorf("expected all empty categories for empty patches")
		}
	})
}

func TestExtractAddRemove(t *testing.T) {
	fp := filePatch{
		addedLines:   []string{"new1", "new2"},
		removedLines: []string{"old1"},
	}

	result := extractAddRemove(fp)

	expected := []string{"+ new1", "+ new2", "- old1"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("extractAddRemove() = %v, want %v", result, expected)
	}
}

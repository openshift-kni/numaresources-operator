// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// These tests use real commits from this repository's git history.
// They require git to be available and the test to run inside the repo.
// Commit hashes are pinned to known-good commits that will not change.

const (
	// A small Makefile-only commit (1 file, 1 line changed)
	commitMakefileHash    = "cbb2ea559ef9e54ae4815dc21359ec35a1619d15"
	commitMakefileSubj    = "makefile: add version to golangci-lint make target"
	commitMakefilePatchID = "68e81d5626f5809a643dbe8ef9e3b4a186119f75"

	// A small Dockerfile-only commit (1 file, 1 line changed)
	commitDockerfileHash    = "ff92eec65d2fc7c03177e288a5e57d8133894220"
	commitDockerfilePatchID = "505adf50d61d342228109401150afbf55aa3ad55"

	// A cherry-picked commit on release-4.18 whose patch-id differs from main
	commitCherryPickHash    = "1c1afa7512e2da2c67953ff776cf49f7714cb6ac"
	commitCherryPickPatchID = "d287db5c017f16c2429e096c1e19e646d678a460"

	// The original commit on main that the above was cherry-picked from
	commitOriginalHash    = "6a56840544a420c1393c39fc61e3580f563c1d1f"
	commitOriginalPatchID = "26ad3aeee5defc95bafba21335baf2a97e771c6d"
)

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		t.Skip("not inside a git repository")
	}
}

func commitExists(hash string) bool {
	cmd := exec.Command("git", "cat-file", "-t", hash)
	return cmd.Run() == nil
}

func TestParsePatchFilesFromGitHistory(t *testing.T) {
	skipIfNoGit(t)
	if !commitExists(commitMakefileHash) {
		t.Skipf("commit %s not found in local history", commitMakefileHash)
	}

	t.Run("makefile commit has one file", func(t *testing.T) {
		patch, err := getCommitPatch(commitMakefileHash)
		if err != nil {
			t.Fatalf("getCommitPatch: %v", err)
		}

		files := parsePatchFiles(patch)

		if len(files) != 1 {
			t.Fatalf("expected 1 file, got %d", len(files))
		}
		if files[0].path != "Makefile" {
			t.Errorf("path = %q, want %q", files[0].path, "Makefile")
		}
		if len(files[0].addedLines) != 1 {
			t.Errorf("expected 1 added line, got %d", len(files[0].addedLines))
		}
		if len(files[0].removedLines) != 1 {
			t.Errorf("expected 1 removed line, got %d", len(files[0].removedLines))
		}
	})

	t.Run("dockerfile commit has one file", func(t *testing.T) {
		if !commitExists(commitDockerfileHash) {
			t.Skipf("commit %s not found in local history", commitDockerfileHash)
		}

		patch, err := getCommitPatch(commitDockerfileHash)
		if err != nil {
			t.Fatalf("getCommitPatch: %v", err)
		}

		files := parsePatchFiles(patch)

		if len(files) != 1 {
			t.Fatalf("expected 1 file, got %d", len(files))
		}
		if files[0].path != "Dockerfile" {
			t.Errorf("path = %q, want %q", files[0].path, "Dockerfile")
		}
	})
}

func TestDiffLinesSetsFromGitHistory(t *testing.T) {
	skipIfNoGit(t)
	if !commitExists(commitMakefileHash) || !commitExists(commitDockerfileHash) {
		t.Skip("required commits not found in local history")
	}

	t.Run("same commit against itself has no diffs", func(t *testing.T) {
		patch, err := getCommitPatch(commitMakefileHash)
		if err != nil {
			t.Fatalf("getCommitPatch: %v", err)
		}

		files := parsePatchFiles(patch)
		if len(files) == 0 {
			t.Fatal("no files in patch")
		}

		onlyBP, onlyMain := diffLinesSets(files[0], files[0])
		if len(onlyBP) != 0 || len(onlyMain) != 0 {
			t.Errorf("same file diffed against itself should have no diffs, got onlyBP=%v onlyMain=%v", onlyBP, onlyMain)
		}
	})

	t.Run("different commits have diffs", func(t *testing.T) {
		makefilePatch, err := getCommitPatch(commitMakefileHash)
		if err != nil {
			t.Fatalf("getCommitPatch(makefile): %v", err)
		}
		dockerfilePatch, err := getCommitPatch(commitDockerfileHash)
		if err != nil {
			t.Fatalf("getCommitPatch(dockerfile): %v", err)
		}

		makefileFiles := parsePatchFiles(makefilePatch)
		dockerfileFiles := parsePatchFiles(dockerfilePatch)
		if len(makefileFiles) == 0 || len(dockerfileFiles) == 0 {
			t.Fatal("no files in one of the patches")
		}

		onlyBP, onlyMain := diffLinesSets(makefileFiles[0], dockerfileFiles[0])
		if len(onlyBP) == 0 && len(onlyMain) == 0 {
			t.Errorf("different commits should produce diffs")
		}
	})
}

func TestMatchCommitsFromGitHistory(t *testing.T) {
	skipIfNoGit(t)

	t.Run("same commit matches itself", func(t *testing.T) {
		if !commitExists(commitMakefileHash) {
			t.Skipf("commit %s not found", commitMakefileHash)
		}

		msg := getCommitMessage(t, commitMakefileHash)
		ci := CommitInfo{Hash: commitMakefileHash, PatchID: commitMakefilePatchID, Message: msg}

		results, unmatched := matchCommits([]CommitInfo{ci}, []CommitInfo{ci})

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Status != StatusMatch {
			t.Errorf("status = %v, want StatusMatch", results[0].Status)
		}
		if len(unmatched) != 0 {
			t.Errorf("expected 0 unmatched, got %d", len(unmatched))
		}
	})

	t.Run("cherry-pick with diverged patch-id is unmatched", func(t *testing.T) {
		if !commitExists(commitCherryPickHash) || !commitExists(commitOriginalHash) {
			t.Skip("cherry-pick or original commit not found in local history")
		}

		cpMsg := getCommitMessage(t, commitCherryPickHash)
		origMsg := getCommitMessage(t, commitOriginalHash)

		bp := []CommitInfo{{Hash: commitCherryPickHash, PatchID: commitCherryPickPatchID, Message: cpMsg}}
		main := []CommitInfo{{Hash: commitOriginalHash, PatchID: commitOriginalPatchID, Message: origMsg}}

		results, _ := matchCommits(bp, main)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Status != StatusUnmatched {
			t.Errorf("status = %v, want StatusUnmatched (different patch-ids)", results[0].Status)
		}
		if results[0].ClosestMatch == nil {
			t.Errorf("ClosestMatch should be set via subject fallback (same subject)")
		}
	})

	t.Run("two commits with distinct patch-ids produce correct unmatched main", func(t *testing.T) {
		if !commitExists(commitMakefileHash) || !commitExists(commitDockerfileHash) {
			t.Skip("required commits not found")
		}

		makefileMsg := getCommitMessage(t, commitMakefileHash)
		dockerfileMsg := getCommitMessage(t, commitDockerfileHash)

		bp := []CommitInfo{
			{Hash: commitMakefileHash, PatchID: commitMakefilePatchID, Message: makefileMsg},
		}
		main := []CommitInfo{
			{Hash: commitMakefileHash, PatchID: commitMakefilePatchID, Message: makefileMsg},
			{Hash: commitDockerfileHash, PatchID: commitDockerfilePatchID, Message: dockerfileMsg},
		}

		results, unmatched := matchCommits(bp, main)

		if len(results) != 1 || results[0].Status != StatusMatch {
			t.Errorf("expected 1 MATCH result, got %v", results)
		}
		if len(unmatched) != 1 || unmatched[0].Hash != commitDockerfileHash {
			t.Errorf("expected dockerfile commit as unmatched main, got %v", unmatched)
		}
	})
}

func TestGetCommitsFromRangeFromGitHistory(t *testing.T) {
	skipIfNoGit(t)
	if !commitExists(commitMakefileHash) {
		t.Skipf("commit %s not found", commitMakefileHash)
	}

	t.Run("single commit range", func(t *testing.T) {
		gitRange := commitMakefileHash + "~1.." + commitMakefileHash

		commits, err := getCommitsFromRange(gitRange)
		if err != nil {
			t.Fatalf("getCommitsFromRange: %v", err)
		}

		if len(commits) != 1 {
			t.Fatalf("expected 1 commit, got %d", len(commits))
		}
		if commits[0].Hash != commitMakefileHash {
			t.Errorf("hash = %q, want %q", commits[0].Hash, commitMakefileHash)
		}
		if commits[0].PatchID != commitMakefilePatchID {
			t.Errorf("patch-id = %q, want %q", commits[0].PatchID, commitMakefilePatchID)
		}
		if !strings.HasPrefix(commits[0].Message, commitMakefileSubj) {
			t.Errorf("message should start with %q, got %q", commitMakefileSubj, commits[0].Subject())
		}
	})
}

func TestComparePatchesFromGitHistory(t *testing.T) {
	skipIfNoGit(t)

	t.Run("same commit patch against itself is all identical", func(t *testing.T) {
		if !commitExists(commitMakefileHash) {
			t.Skipf("commit %s not found", commitMakefileHash)
		}

		patch, err := getCommitPatch(commitMakefileHash)
		if err != nil {
			t.Fatalf("getCommitPatch: %v", err)
		}

		pc := comparePatches(patch, patch)

		if pc.err != nil {
			t.Fatalf("unexpected error: %v", pc.err)
		}
		if len(pc.identical) == 0 {
			t.Errorf("expected at least 1 identical file")
		}
		if len(pc.edits)+len(pc.additive)+len(pc.moved) != 0 {
			t.Errorf("expected no edits/additive/moved, got edits=%d additive=%d moved=%d",
				len(pc.edits), len(pc.additive), len(pc.moved))
		}
	})

	t.Run("different commits produce edits or moved", func(t *testing.T) {
		if !commitExists(commitMakefileHash) || !commitExists(commitDockerfileHash) {
			t.Skip("required commits not found")
		}

		bpPatch, err := getCommitPatch(commitMakefileHash)
		if err != nil {
			t.Fatalf("getCommitPatch(makefile): %v", err)
		}
		mainPatch, err := getCommitPatch(commitDockerfileHash)
		if err != nil {
			t.Fatalf("getCommitPatch(dockerfile): %v", err)
		}

		pc := comparePatches(bpPatch, mainPatch)

		if pc.err != nil {
			t.Fatalf("unexpected error: %v", pc.err)
		}
		if len(pc.identical) != 0 {
			t.Errorf("different files should not be identical, got %d", len(pc.identical))
		}
		totalClassified := len(pc.edits) + len(pc.moved) + len(pc.additive)
		if totalClassified == 0 {
			t.Errorf("expected at least 1 classified file difference")
		}
	})

	t.Run("cherry-pick vs original produces diffs", func(t *testing.T) {
		if !commitExists(commitCherryPickHash) || !commitExists(commitOriginalHash) {
			t.Skip("cherry-pick or original commit not found")
		}

		bpPatch, err := getCommitPatch(commitCherryPickHash)
		if err != nil {
			t.Fatalf("getCommitPatch(cherry-pick): %v", err)
		}
		mainPatch, err := getCommitPatch(commitOriginalHash)
		if err != nil {
			t.Fatalf("getCommitPatch(original): %v", err)
		}

		pc := comparePatches(bpPatch, mainPatch)

		if pc.err != nil {
			t.Fatalf("unexpected error: %v", pc.err)
		}
		totalFiles := len(pc.identical) + len(pc.edits) + len(pc.additive) + len(pc.moved)
		if totalFiles == 0 {
			t.Errorf("expected at least 1 file in comparison")
		}
	})
}

func getCommitMessage(t *testing.T, hash string) string {
	t.Helper()
	cmd := exec.Command("git", "log", "--format=%B", "-1", hash)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log for %s: %v", hash, err)
	}
	return string(out)
}

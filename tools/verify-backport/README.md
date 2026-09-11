# verify-backport

Verifies that a backport branch carries commits whose content is identical to their
counterparts on the main branch.

## Problem

When commits are cherry-picked or rebased onto a release branch, the commit hashes change.
Cherry-pick trailers (`cherry picked from commit ...`) may be missing or incorrect.
This tool provides **ex-post verification** that the backport commits match their originals
by comparing the actual patch content, not metadata.

## How it works

The tool uses `git patch-id --stable` to compute a content-based hash of each commit's diff.
Two commits that introduce the same code changes produce the same patch-id, regardless of
their commit hash or surrounding context line numbers. Whitespace is normalized by default
in all patch-id modes except `--verbatim`; `--stable` ensures the hash is also independent
of the order in which files and hunks appear in the diff.

For each backport commit:
1. Compute its patch-id
2. Find a main-branch commit with the same patch-id
3. Verify the backport commit message **contains** the original commit message
   (the backport may add extra text such as backport-specific notes)

When a backport commit doesn't match by patch-id, the tool tries to find a likely
counterpart on main by matching the commit subject line, and reports it as a
closest match (`~>`) so the user knows which main commit to compare against.

For unmatched commits, the tool performs a file-level comparison and classifies
each file by the type of review effort required (see "Change classification" below).

## Usage

### Branch mode

Point the tool at a local branch that is a checkout of a backport PR:

```bash
# Fetch the PR locally
git fetch origin pull/1234/head:pr-1234

# Verify the backport (targeting release-4.18)
bin/verify-backport \
  --backport-branch pr-1234 \
  --base-branch origin/release-4.18
```

This derives backport commits as `origin/release-4.18..pr-1234` and searches
the last 100 commits on main for matches.

### Range mode

Specify explicit git revision ranges for full control:

```bash
bin/verify-backport \
  --backport-range release-4.18~5..release-4.18 \
  --main-range main~20..main
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--backport-branch` | | Local branch name (pseudo-PR mode) |
| `--base-branch` | | Branch the backport is based on (e.g. `release-4.18`); required with `--backport-branch` |
| `--backport-range` | | Git revision range for backport commits |
| `--main-range` | | Git revision range for main commits |
| `--main-branch` | `main` | Name of the main branch where originals live |
| `--main-depth` | `100` | How many main commits to search (branch mode) |
| `--check-missing` | `false` | Report main commits with no backport counterpart |
| `--verbose` | `false` | Show structured diff for unmatched commits |
| `--color` | `true` | Enable colored output (auto-disabled when not a terminal) |
| `--json` | `false` | Output results as JSON (for scripting and LLM consumption) |

Exactly one of `--backport-branch` or `--backport-range` must be provided.
When using `--backport-branch`, `--base-branch` is required.
When using `--backport-range`, `--main-range` is required.

## Output

### Default mode

Each backport commit gets one status line:

```
MATCH:            abc1234 Fix the widget       <-->  def5678
MISMATCH-MESSAGE: abc1234 Update docs          <-->  def5678
UNMATCHED:        abc1234 Adapted change       (~>   def5678)  [2 identical, 1 moved, 1 additive, 1 edits]
  identical (2):
    internal/controller/nro_controller.go -> controllers/nro_controller.go (renamed)
    pkg/status/status_test.go
  moved (1):
    machineconfigpool.go -> rte.go (95% overlap)
  additive (1):
    pkg/status/status.go (+1 only in backport)
  edits (1):
    test/e2e/serial/tests/configuration.go (+4 only in backport, +2 only in main)
```

The `~>` arrow indicates a closest match found by commit subject. The bracketed tag
shows the reviewer triage summary. Files are grouped by change type (see below).

With `--check-missing`, main commits with no backport counterpart are also shown:

```
MISSING-BACKPORT: def5678 Original commit subject
```

### Verbose mode (`--verbose`)

Adds the actual differing lines for `additive` and `edits` files, and residual (non-overlapping)
lines for `moved` files.

```
  edits (1):
    test/e2e/serial/tests/configuration.go (+4 only in backport, +2 only in main)
      only in backport:
        + import "github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
        + cond := status.FindCondition(updatedOperObj.Status.Conditions, status.ConditionAvailable)
        ...
      only in main:
        + dss, err := objects.GetDaemonSetsByNamespacedName(fxt.Client, ctx, ...)
        ...
```

### JSON mode (`--json`)

Outputs a single JSON object to stdout, suitable for scripting or LLM consumption.
No color codes, no padding, no verbose preamble — just structured data.

```json
{
  "summary": {
    "matched": 3,
    "total": 4,
    "unmatched_main": 1
  },
  "results": [
    {
      "status": "MATCH",
      "backport": {"hash": "abc1234...", "subject": "Fix the widget"},
      "main":     {"hash": "def5678...", "subject": "Fix the widget"}
    },
    {
      "status": "UNMATCHED",
      "backport": {"hash": "aaa1111...", "subject": "Adapted change"},
      "closest":  {"hash": "bbb2222...", "subject": "Adapted change"},
      "patch_comparison": {
        "identical": 2,
        "moved": 1,
        "additive": 1,
        "edits": 0
      }
    }
  ],
  "missing_backports": [
    {"hash": "ccc3333...", "subject": "Original commit subject"}
  ]
}
```

Field presence rules:
- `main`: present only for `MATCH` and `MISMATCH-MESSAGE` results
- `closest` and `patch_comparison`: present only for `UNMATCHED` results with a subject match
- `missing_backports`: present only when `--check-missing` is set and there are unmatched main commits

#### LLM token efficiency estimate

Without this tool, an LLM verifying a 10-commit backport against 100 main commits needs
to run ~4 git commands (log + patch-id for each side), ingest their output (~4000-6000
tokens of raw git data), and cross-reference patch-ids and messages in-context.

With `--json`, it is a single command producing ~500-800 tokens of pre-correlated output.
The estimated saving is **3-5x fewer tokens** for the ingestion step, plus elimination of
the multi-step reasoning needed to correlate commits manually. The exit code alone (0 vs 2)
answers "is this backport correct?" without parsing any output at all.

These estimates are back-of-envelope (May 2026) and should be verified against actual
token counts in representative backport reviews.

### Change classification

Files are classified by the type of review effort they require, from lowest to highest bandwidth:

| Category | What it means | How to review | Color |
|----------|---------------|---------------|-------|
| **identical** | No differing lines (with or without rename) | Skip entirely | dim |
| **moved** | File relocated between releases (>=50% content overlap) | Glance at residual lines (shown in verbose) | dim |
| **additive** | Only one side has extra lines | Check that additions/deletions make sense for the target branch | yellow |
| **edits** | Both sides have differing lines | Full review: substantive changes were made | yellow |

This classification tells you **how** to review each file, not just whether to review it.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | All backport commits match their main counterparts |
| 1 | Usage error or runtime failure (bad flags, git command error) |
| 2 | Verification failure (mismatches, unmatched, or missing backports) |

## Design notes

### Architecture

The tool operates in three phases:

1. **Commit-level matching**: Compute patch-ids for backport and main commits; match by patch-id or fall back to subject-line matching
2. **File-level comparison**: For each unmatched commit pair, parse both patches and extract per-file added/removed lines. **Whitespace is intentionally treated as non-semantic**: lines are trimmed before comparison, so pure whitespace/indentation differences are not reported as edits. This matches the behavior of `git patch-id --stable` and avoids drowning reviewers in noise from reformatting or tab/space changes between branches.
3. **Change classification**: Classify each file by change type (identical/moved/additive/edits) and present triage-friendly output

### Code movement detection

After pairing files by path and basename, leftover ONLY-IN-BACKPORT and ONLY-IN-MAIN files
are tested for code movement using set intersection of their added lines:

```
overlap = |intersection(bpAdded, mainAdded)| / min(|bpAdded|, |mainAdded|)
```

If overlap >= 50%, the files are classified as a `moved` pair. The `min` denominator (not union)
ensures that a small file fully contained in a larger one still registers as movement.

**Why this is reliable:**
- Compares actual code content, not filenames or heuristics
- False positives (unrelated files sharing 50%+ of added lines) are extremely unlikely in practice
- Real-world backports show near-100% overlap for genuine code movement (e.g., refactoring between releases)

**Scope constraint:** Code movement is detected only within each commit. If code moves across
commits within the same backport, that's a red flag indicating the backport diverged from the
original structure, which itself is useful signal.

### Why patch-id for commit matching

`git patch-id` is purpose-built for detecting equivalent patches across rebases and cherry-picks.
It normalizes away:
- Context line numbers (hunks can shift without changing the patch-id)
- Whitespace (default in all modes except `--verbatim`; there is no separate `--ignore-whitespace` flag)
- File and hunk ordering (`--stable` mode ensures this; `--unstable` does not)
- Commit metadata (author, date, hash)

But it remains sensitive to:
- Actual code changes (even one character difference produces a different patch-id)
- File renames (a renamed file produces a different patch)

This is the right level of strictness: we want to catch any substantive changes while allowing
mechanical differences like directory restructuring or import path updates.

### Design constraints

- **Local-only, no network access**: The tool **never contacts remote servers**. All git operations run against the local clone using `exec.Command`. It does not fetch, push, or call any API (GitHub or otherwise). This is a deliberate trust boundary: the tool's output depends solely on what is already checked out locally, so there is no risk of leaking credentials, hitting rate limits, or trusting data from an external service. The user controls what is in the local clone; the tool only reads it.
- **No external dependencies beyond git**: The tool uses only stdlib + `golang.org/x/term` (for color detection). No go-git, no GitHub API.
- **Stateless**: Each run is independent; no caching or persistent state between runs.
- **Colored by default**: Color is a high-ROI UX improvement for dense output. Auto-disabled when not a terminal or when `--color=false`.

### Future iteration notes

- **Cross-commit movement detection**: Currently not implemented (by design). If needed, pool all ONLY-IN-BACKPORT and ONLY-IN-MAIN files across all commits and match globally. But this may hide backport structure divergence, so default should remain within-commit.
- **Mechanical change detection**: Import path substitutions (e.g., `api/numaresourcesoperator/v1` → `api/v1`) are currently shown as diffs. Could detect and label these as "mechanical" to further reduce reviewer bandwidth.
- **Diff output format**: Currently shows +/- lines inline. Could add side-by-side or unified diff views.
- **Performance**: For very large backport PRs (50+ commits), computing patch-ids and parsing patches is the bottleneck. Could parallelize per-commit processing if needed.

## Key assumptions

- **Local clone required, no network access**: The tool operates exclusively on a local git
  repository and **never makes network calls**. Both the backport branch/range and the main
  branch must be fetched locally before running the tool. This eliminates any need to trust
  remote services or configure authentication.
- **No merge commits**: Merge commits are excluded from comparison (`--no-merges`) since
  they don't carry meaningful patch content.
- **Message containment, not equality**: The backport commit message must *contain* the
  main commit message as a substring. This allows backport-specific additions (e.g.,
  "Backport to release-4.18" notes) while ensuring the original message is preserved.
- **Whitespace normalization**: Trailing whitespace per line and trailing newlines are
  normalized before message comparison.
- **Patch-id stability**: `git patch-id --stable` is used for reproducible results
  across git versions.
- **Closest match by subject**: When patch-ids don't match, the tool falls back to
  matching by commit subject line to suggest a likely counterpart for investigation.

## Building

```bash
make bin/verify-backport
```

## Requirements

- Git (any reasonably modern version with `patch-id --stable` support, i.e., >= 2.0)
- Go 1.25+ (for building)

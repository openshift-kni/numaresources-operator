#!/usr/bin/env bash

# compare the CSV file against the latest revision
# but ignore the createdAt field which is always changing.
if [[ "$(git diff --quiet -I'^    createdAt: ' bundle)" -eq 0 ]]; then
  git checkout --quiet bundle
fi

if [[ -n "$(git status --untracked-files=no --porcelain .)" ]]; then
        echo "uncommitted generated files. run 'make generate bundle manifests' and commit results."
        git status --untracked-files=no --porcelain .
        exit 1
fi

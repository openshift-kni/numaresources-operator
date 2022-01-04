#!/usr/bin/env bash

if [[ -n "$(git status --untracked-files=no --porcelain .)" ]]; then
        echo "uncommitted generated files. run 'make generate' and commit results."
        echo "$(git status --untracked-files=no --porcelain .)"
        exit 1
fi

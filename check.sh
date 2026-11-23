#!/bin/sh
# The gate on Linux: every check AGENTS.md requires, stopping at the first that fails. Its steps are cmd/gate's, which
# check.ps1 runs on Windows, so the two hosts check the same things (README, "Building on Linux"). Arguments go to
# `minimaxon test`.
set -e
cd "$(dirname "$0")"
exec go run ./cmd/gate check "$@"

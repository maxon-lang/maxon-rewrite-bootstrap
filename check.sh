#!/bin/sh
# The gate on Linux and macOS: every check AGENTS.md requires, stopping at the first that fails. Its steps are cmd/gate's,
# which check.ps1 runs on Windows, so the hosts check the same things (README, "Building on Linux" and "Building on
# macOS"). Arguments go to `minimaxon test`.
set -e
cd "$(dirname "$0")"
exec go run ./cmd/gate check "$@"

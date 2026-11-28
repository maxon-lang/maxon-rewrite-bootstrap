#!/bin/sh
# Runs the slice benchmark (bench/README.md, "slice"): minimaxon's bench/slice/slice.maxon and Go's make and copy
# (bench/slice/gobench). Each lane prints a `result <lane> <ns a round> <ps a byte>` line.
#
#   bench/slice/run.sh
#   ROUNDS=20000 COMPILER=bin/c1 bench/slice/run.sh
set -e
cd "$(dirname "$0")/../.."
: "${ROUNDS:=20000}" "${COMPILER:=bin/minimaxon}"
"$COMPILER" build bench/slice/slice.maxon -o bin/slice-bench
go build -o bin/slice-gobench ./bench/slice/gobench
echo "== minimaxon"
bin/slice-bench "$ROUNDS" < /dev/null
echo "== go"
bin/slice-gobench "$ROUNDS" < /dev/null

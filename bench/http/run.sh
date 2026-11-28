#!/bin/sh
# Runs the HTTP codec benchmark (bench/README.md): minimaxon's bench/http/codec.maxon over the library's converted codec,
# the same lanes in maxon's dialect (bench/http/maxon/codec.maxon) built by maxon's compiler, and Go's net/http
# (bench/http/gobench). Each lane prints a `result <lane> <ns a request>` line.
#
#   bench/http/run.sh
#   ROUNDS=20000 MAXON=/path/to/maxon bench/http/run.sh
#
# ROUNDS the requests each lane times, COMPILER the minimaxon that builds codec.maxon, MAXON maxon's compiler (its
# maxon-bin build; the lane is left out where it is not there). maxon's is run from a directory of its own under TMPDIR, so
# nothing runs inside its tree.
set -e
cd "$(dirname "$0")/../.."
: "${ROUNDS:=100000}" "${COMPILER:=bin/minimaxon}" "${MAXON:=$HOME/Dev/maxon/maxon-bin/.maxon/maxon}"
root=$(pwd)
"$COMPILER" build bench/http/codec.maxon -o bin/http-codec
go build -o bin/http-gobench ./bench/http/gobench
echo "== minimaxon"
bin/http-codec "$ROUNDS" < /dev/null
if [ -x "$MAXON" ]; then
	work=$(mktemp -d "${TMPDIR:-/tmp}/http-bench-maxon.XXXXXX")
	cp bench/http/maxon/codec.maxon "$work/main.maxon"
	(cd "$work" && "$MAXON" build main.maxon --output="$work/codec" > "$work/build.log" 2>&1) || { cat "$work/build.log"; exit 1; }
	echo "== maxon"
	"$work/codec" "$ROUNDS" < /dev/null
	rm -rf "$work"
fi
echo "== go"
bin/http-gobench "$ROUNDS" < /dev/null

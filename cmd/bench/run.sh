#!/bin/sh
# Runs the benchmarks of a batch that `bench build` made and writes what they measured to one results file, for
# `bench report`. It needs a POSIX sh with the usual utilities (awk, date, cp, chmod, mktemp, tr, uname, wc, getconf) and the
# batch's executables, which are static: it runs on any Linux of the batch's architecture, in a VM or container that mounts this
# directory. /usr/bin/time (GNU time, `apt install time`) is optional: without it the peak memory is not recorded. Nothing is
# installed or fetched.
#
#   sh run.sh
#   BENCH_ROUNDS=3 BENCH_RESULTS=/tmp/results.txt sh run.sh
#
# BENCH_ROUNDS is the number of rounds (default 7): each round runs every program once with minimaxon and once with Go,
# and the one that goes first alternates from round to round. BENCH_RESULTS is the results file; by default it is
# ../../out/bench-<target>.txt, relative to this directory.
set -u
here=$(cd "$(dirname "$0")" && pwd)
tab=$(printf '\t')
manifest="$here/manifest.txt"
rounds=${BENCH_ROUNDS:-7}

manifest_value() {
	awk -F "$tab" -v key="$1" '$1 == key { print $2; exit }' "$manifest"
}

target=$(manifest_value target)
results=${BENCH_RESULTS:-$here/../../out/bench-$target.txt}
mkdir -p "$(dirname "$results")" || exit 1
work=$(mktemp -d "${TMPDIR:-/tmp}/bench.XXXXXX") || exit 1
trap 'rm -rf "$work"' EXIT INT TERM

# The executables are copied out of the batch, which may be a share that mounts files without the execute bit or is slow to
# load from, and run from the temporary directory.
mkdir "$work/exe" && cp -R "$here/minimaxon" "$here/go" "$work/exe/" && chmod -R u+x "$work/exe" || exit 1

# time_flags makes this system's time(1) report the peak resident set on standard error, rss_pattern names the line that
# holds it and rss_divisor turns its number into kilobytes.
time_bin=
if [ -x /usr/bin/time ]; then
	if /usr/bin/time -v true >/dev/null 2>&1; then
		time_bin=/usr/bin/time
		time_flags=-v
		rss_pattern='Maximum resident set size'
		rss_divisor=1
	elif /usr/bin/time -l true >/dev/null 2>&1; then
		time_bin=/usr/bin/time
		time_flags=-l
		rss_pattern='maximum resident set size'
		rss_divisor=1024
	fi
fi

cpu_model() {
	model=
	if command -v lscpu >/dev/null 2>&1; then
		model=$(lscpu 2>/dev/null | awk -F: '/^Model name/ { sub(/^[ \t]+/, "", $2); print $2; exit }')
	fi
	if [ -z "$model" ] && [ -r /proc/cpuinfo ]; then
		model=$(awk -F: '/^(model name|Model|Hardware)/ { sub(/^[ \t]+/, "", $2); print $2; exit }' /proc/cpuinfo)
	fi
	if [ -z "$model" ] && command -v sysctl >/dev/null 2>&1; then
		model=$(sysctl -n machdep.cpu.brand_string 2>/dev/null)
	fi
	echo "${model:-unknown}"
}

cores() {
	getconf _NPROCESSORS_ONLN 2>/dev/null || echo unknown
}

# now_nanos is the clock in nanoseconds, or - where date has no %N.
now_nanos() {
	value=$(date +%s%N 2>/dev/null)
	case $value in
	*[!0-9]* | '') echo - ;;
	*) echo "$value" ;;
	esac
}

# run_one <impl> <program> <round> runs one executable and appends what it measured to the results.
run_one() {
	impl=$1
	name=$2
	round=$3
	exe="$work/exe/$impl/$name"
	out="$work/out"
	report="$work/time"
	: >"$report"
	started=$(now_nanos)
	if [ -n "$time_bin" ]; then
		"$time_bin" $time_flags "$exe" >"$out" 2>"$report"
	else
		"$exe" >"$out" 2>"$report"
	fi
	code=$?
	ended=$(now_nanos)
	wall=-
	if [ "$started" != - ] && [ "$ended" != - ]; then
		wall=$((ended - started))
	fi
	rss=-
	if [ -n "$time_bin" ]; then
		rss=$(awk -v pattern="$rss_pattern" -v divisor="$rss_divisor" \
			'index($0, pattern) { for (i = 1; i <= NF; i++) if ($i ~ /^[0-9]+$/) value = $i; print int(value / divisor); exit }' "$report")
		rss=${rss:--}
	fi
	{
		printf 'exit\t%s\t%s\t%s\t%s\n' "$impl" "$name" "$round" "$code"
		printf 'wall\t%s\t%s\t%s\t%s\n' "$impl" "$name" "$round" "$wall"
		printf 'rss\t%s\t%s\t%s\t%s\n' "$impl" "$name" "$round" "$rss"
		awk -v impl="$impl" -v name="$name" -v round="$round" -v tab="$tab" '
			match($0, / ns [0-9]+$/) {
				printf "phase\t%s\t%s\t%s\t%s\t%s\n", impl, name, round, substr($0, 1, RSTART - 1), substr($0, RSTART + 4)
				next
			}
			round == 1 { printf "result\t%s\t%s\t%s\n", impl, name, $0 }
		' "$out"
	} >>"$results"
}

{
	echo "meta${tab}uname${tab}$(uname -a)"
	echo "meta${tab}cpu${tab}$(cpu_model)"
	echo "meta${tab}cores${tab}$(cores)"
	echo "meta${tab}rounds${tab}$rounds"
	echo "meta${tab}date${tab}$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	if [ -n "$time_bin" ]; then
		echo "meta${tab}peak-memory${tab}recorded by $time_bin $time_flags"
	else
		echo "meta${tab}peak-memory${tab}not recorded: no usable /usr/bin/time"
	fi
	awk -F "$tab" '$1 == "target" || $1 == "go" || $1 == "compiler" { print "meta\t" $1 "\t" $2 } $1 == "expect" { print }' "$manifest"
} >"$results"

programs=$(awk -F "$tab" '$1 == "program" { print $2 }' "$manifest")
for impl in minimaxon go; do
	for name in $programs; do
		printf 'size\t%s\t%s\t%s\n' "$impl" "$name" "$(wc -c <"$work/exe/$impl/$name" | tr -d ' ')" >>"$results"
	done
done

round=1
while [ "$round" -le "$rounds" ]; do
	if [ $((round % 2)) -eq 1 ]; then
		first=minimaxon
		second=go
	else
		first=go
		second=minimaxon
	fi
	for name in $programs; do
		echo "round $round/$rounds: $name" >&2
		run_one "$first" "$name" "$round"
		run_one "$second" "$name" "$round"
	done
	round=$((round + 1))
done
echo "results: $results" >&2

#!/bin/bash
# Runs `fuzz` over a range of seeds in parallel processes, one shard of the range each, and reports the lowest failing seed: the
# nightly fuzz workflow (.github/workflows/nightly-fuzz.yml) runs it, and it runs the same anywhere the compiler runs.
#
#   bash cmd/fuzzshards/run.sh <compiler> <first seed> <count> [fuzz flag]...
#   bash cmd/fuzzshards/run.sh bin/minimaxon 1 100 --native
#
# The fuzz flags (such as --native) go to every shard, which runs `<compiler> fuzz <flags> --seed=S --count=M --keep=DIR`. The
# shards are FUZZ_SHARDS processes (default: the number of online cores), and their output and kept programs go under FUZZ_OUT
# (default: a new temporary directory): shard<k>.log, keep<k>/, and failures/, which holds the failing programs and the logs of
# the shards that failed. When GITHUB_STEP_SUMMARY names a file, the report is appended to it too.
# It exits 0 when every shard passed, 1 when one failed, and 2 when its arguments are wrong.
set -u

if [ $# -lt 3 ]; then
  sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//' >&2
  exit 2
fi
compiler=$1
first=$2
count=$3
shift 3
case "$first$count" in
  *[!0-9]*) echo "fuzzshards: the first seed and the count are numbers" >&2; exit 2 ;;
esac
[ "$count" -gt 0 ] || { echo "fuzzshards: the count is at least 1" >&2; exit 2; }
[ -x "$compiler" ] || { echo "fuzzshards: no executable compiler at $compiler" >&2; exit 2; }

shards=${FUZZ_SHARDS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 1)}
case "$shards" in
  ''|*[!0-9]*|0) echo "fuzzshards: FUZZ_SHARDS is a number of at least 1" >&2; exit 2 ;;
esac
[ "$shards" -le "$count" ] || shards=$count
per=$(( (count + shards - 1) / shards ))
last=$(( first + count - 1 ))
out=${FUZZ_OUT:-$(mktemp -d "${TMPDIR:-/tmp}/fuzzshards.XXXXXX")}
mkdir -p "$out"
rm -rf "$out"/shard*.log "$out"/keep* "$out/failures"
summary=${GITHUB_STEP_SUMMARY:-/dev/null}

flags="$*"
range="seeds $first to $last ($count seeds) in $shards shards of up to $per, fuzz flags: ${flags:-none}${GITHUB_SHA:+, commit $GITHUB_SHA}"
echo "fuzzshards: $range"
echo "fuzzshards: output in $out"
{
  echo "### fuzz on $(uname -s) $(uname -m)"
  echo
  echo "Range: $range."
  echo
} >> "$summary"

pids=""
k=0
while [ "$k" -lt "$shards" ]; do
  s=$(( first + k * per ))
  m=$per
  [ $(( s + m - 1 )) -le "$last" ] || m=$(( last - s + 1 ))
  if [ "$m" -gt 0 ]; then
    "$compiler" fuzz "$@" --seed="$s" --count="$m" --keep="$out/keep$k" > "$out/shard$k.log" 2>&1 &
    pids="$pids $!:$k:$s:$m"
  fi
  k=$(( k + 1 ))
done

failed=""
for entry in $pids; do
  pid=${entry%%:*}
  rest=${entry#*:}
  k=${rest%%:*}
  rest=${rest#*:}
  s=${rest%%:*}
  m=${rest#*:}
  wait "$pid"
  code=$?
  echo "== shard $k: seeds $s to $(( s + m - 1 )), exit $code"
  tail -n 4 "$out/shard$k.log" | sed 's/^/   /'
  [ "$code" -eq 0 ] || failed="$failed $k"
done

if [ -z "$failed" ]; then
  echo "fuzzshards: every shard passed"
  echo "Every shard passed." >> "$summary"
  exit 0
fi

# A failing program is kept as seed<N>.maxon and reported in its shard's log as a line "seed <N>: ..."; the log is read too,
# so that a failure is named even if its program could not be kept.
mkdir -p "$out/failures"
seeds=$(
  for k in $failed; do
    ls "$out/keep$k" 2>/dev/null | sed -n 's/^seed\([0-9][0-9]*\)\.maxon$/\1/p'
    sed -n 's/^seed \([0-9][0-9]*\): .*/\1/p' "$out/shard$k.log"
  done | sort -n -u
)
for k in $failed; do
  cp "$out/keep$k"/seed*.maxon "$out/failures/" 2>/dev/null
  cp "$out/shard$k.log" "$out/failures/"
done

repro="$compiler fuzz${flags:+ $flags} --seed=SEED --count=1 --keep=kept"
if [ -n "$seeds" ]; then
  lowest=$(echo "$seeds" | head -n 1)
  echo "fuzzshards: FAILED, failing seeds: $(echo $seeds)"
  echo "fuzzshards: first failing seed $lowest; reproduce with: ${repro//SEED/$lowest}"
  {
    echo "**FAILED.** First failing seed: **$lowest** (failing seeds: $(echo $seeds))."
    echo
    echo "Reproduce from the root of a checkout of this commit, with the compiler built from it:"
    echo
    echo '```'
    echo "${repro//SEED/$lowest}"
    echo '```'
    echo
    echo "\`fuzz --dump --seed=$lowest\` prints the program; the failing programs are in the run's artifact."
  } >> "$summary"
else
  # No seed is named when a shard failed as a whole: it could not start, or its checker accepted too few programs.
  echo "fuzzshards: FAILED, with no failing seed named, in shards:$failed"
  for k in $failed; do
    echo "== shard $k"
    tail -n 20 "$out/shard$k.log"
  done
  echo "**FAILED** with no failing seed named, in shards:$failed; their logs are in the job log and the run's artifact." >> "$summary"
fi
exit 1

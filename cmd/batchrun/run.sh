#!/bin/bash
# A batch of minimaxon's test runs, run on the machine of its target: every row of rows/ (an executable of bin/ and its
# worker count) runs at least twice, with a 10 s deadline each. What each printed and how it ended goes to
# ../../out/<batch>/out/, with the batch's stamp, for the Windows side to replay, and a summary to ../../out/<batch>.txt.
# Run it from anywhere: bash run.sh
set -u
cd "$(dirname "$0")"
batch=$(basename "$(pwd)")
out=../../out
mkdir -p "$out"
summary="$out/$batch.txt"
chmod +x batchrun bin/*
xattr -cr . 2>/dev/null
{
  echo "== machine"; sw_vers 2>/dev/null; uname -a
  echo "== executables that fail codesign"
  if command -v codesign >/dev/null; then
    for f in bin/*; do codesign -v "$f" >/dev/null 2>&1 || { echo "-- $f"; codesign -vvv "$f" 2>&1 | head -5; }; done
  fi
} > "$summary" 2>&1
rm -rf out
./batchrun run -deadline 10s -repeat 2 . >> "$summary" 2>&1
echo "batchrun exit $?" >> "$summary"
{
  echo "== every run: its status, and the head of what a run that did not exit 0 printed"
  for f in out/*.status; do
    id=$(basename "$f" .status)
    echo "## $id: $(tr '\n' ' ' < "$f")"
    if [ "$(head -1 "$f")" != "exit 0" ]; then
      echo "-- stdout"; head -20 "out/$id.stdout"
      echo "-- stderr"; head -20 "out/$id.stderr"
      [ -f "out/$id.log" ] && { echo "-- log"; head -40 "out/$id.log"; }
    fi
  done
} >> "$summary" 2>&1
rm -rf "$out/$batch"
mkdir -p "$out/$batch"
cp -R out "$out/$batch/"
cp stamp.txt "$out/$batch/"
echo "done: $summary"

#!/bin/bash
# The Mac's proof of the macOS compiler that a renewal of the seeds cross-built (cmd/gate/macseed.go), run on the Mac from
# the folder the renewal staged: the compiler builds self/ into a C1 that must be its own bytes, so that a compile does not
# depend on its host, C1 builds a C2, and C2 a C3, with its IR verified, that must be C2's bytes. Each is built uncached and
# under the seed's file name, in a directory of its own, since a Mach-O executable is signed under its name. On success the
# digest of the compiler goes to ../../out/seed/proven.txt, which the renewal reads; what each step printed goes to
# ../../out/seed.txt. Run it from anywhere: bash run.sh
set -u
cd "$(dirname "$0")"
name=minimaxon-arm64-macos
out=../../out
log="$out/seed.txt"
mkdir -p "$out/seed"
rm -f "$out/seed/proven.txt"
xattr -cr . 2>/dev/null
chmod +x "$name"
rm -rf c1 c2 c3
mkdir -p c1 c2 c3 tmp
export TMPDIR="$(pwd)/tmp"
export MINIMAXON_CACHE=off
digest=$(shasum -a 256 "$name" | cut -d' ' -f1)
fail() {
  echo "not proven: $1" >> "$log"
  echo "not proven: $1 (see $log)"
  exit 1
}
{ echo "== macOS compiler $digest"; sw_vers 2>/dev/null; uname -a; } > "$log" 2>&1
./"$name" build self -o "c1/$name" >> "$log" 2>&1 || fail "the compiler cannot build C1"
cmp -s "$name" "c1/$name" || fail "C1 differs from the compiler that built it, so a compile depends on its host"
"c1/$name" build self -o "c2/$name" >> "$log" 2>&1 || fail "C1 cannot build C2"
"c2/$name" build self --verify-ir -o "c3/$name" >> "$log" 2>&1 || fail "C2 cannot build C3"
cmp -s "c2/$name" "c3/$name" || fail "C2 and C3 differ, so the compiler does not reproduce itself"
echo "proven" >> "$log"
echo "$digest" > "$out/seed/proven.txt"
echo "proven: $digest"

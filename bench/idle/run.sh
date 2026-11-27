#!/bin/sh
# Runs the idle-connection benchmark (bench/README.md) on the loopback, minimaxon's server and then Go's baseline, with no
# root and no change to the system's limits. Each run prints a RESULT block.
#
#   bench/idle/run.sh
#   N=8000 WAKE=2000 IDLE=10s bench/idle/run.sh
#   SERVER=bin/idle-server-linux PEER=bin/idle-peer-linux bench/idle/run.sh   (prebuilt, as on a host with no Go)
#
# N connections (16000 fits macOS's 16384 ephemeral ports on 127.0.0.1), WAKE of them in the wake sample, IDLE the period
# over which the server's CPU is read, PORT and PORT+1 the two servers' ports (outside the ephemeral range), COMPILER the
# minimaxon that builds the server where SERVER is not given.
set -e
cd "$(dirname "$0")/../.."
: "${N:=16000}" "${WAKE:=10000}" "${IDLE:=60s}" "${PORT:=47001}" "${COMPILER:=bin/minimaxon}"
if [ -z "$SERVER" ]; then
	"$COMPILER" build bench/idle/server.maxon -o bin/idle-server
	SERVER=bin/idle-server
fi
if [ -z "$PEER" ]; then
	go build -o bin/idle-peer ./bench/idle/peer
	PEER=bin/idle-peer
fi
echo "soft open-file limit $(ulimit -Sn), hard $(ulimit -Hn)"
"$PEER" run -n "$N" -port "$PORT" -wake "$WAKE" -idle "$IDLE" -label minimaxon -- "$SERVER"
"$PEER" run -n "$N" -port "$((PORT + 1))" -wake "$WAKE" -idle "$IDLE" -label go -- "$PEER" goserver

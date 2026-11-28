#!/bin/sh
# Runs the timers benchmark (bench/README.md), minimaxon's bench/timers/timers.maxon and then Go's baseline
# (bench/timers/gobench), at each processor count, then the idle lane of each. Each lane prints a `result` line.
#
#   bench/timers/run.sh
#   PROCS="1 8" COUNTS=100000 STORM_MS=500 CHURN_MS=500 IDLE=2s bench/timers/run.sh
#
# PROCS the processor counts (MAXON_MAX_PROCS and GOMAXPROCS, one worker a processor in the arm, reset and fire lanes),
# COUNTS the timers armed in those lanes, STORM_MS the reset storm's length at each count, CHURN_SERVICES services each
# churning CHURN_TIMERS 1 ms one-shots for CHURN_MS, IDLE_N the timers of the idle lane and IDLE the period its CPU is read
# over, COMPILER the minimaxon that builds the program where TIMERS is not given, and GOBENCH a prebuilt baseline.
set -e
cd "$(dirname "$0")/../.."
: "${PROCS:=1 2 4 8}" "${COUNTS:=100000 1000000}" "${STORM_MS:=1500}" "${CHURN_SERVICES:=16}" "${CHURN_TIMERS:=4096}"
: "${CHURN_MS:=1500}" "${IDLE_N:=1000000}" "${IDLE:=5s}" "${COMPILER:=bin/minimaxon}"
if [ -z "$TIMERS" ]; then
	"$COMPILER" build bench/timers/timers.maxon -o bin/timers
	TIMERS=bin/timers
fi
if [ -z "$GOBENCH" ]; then
	go build -o bin/timers-gobench ./bench/timers/gobench
	GOBENCH=bin/timers-gobench
fi
for p in $PROCS; do
	first=1
	for n in $COUNTS; do
		# The churn runs once a processor count, with the first count.
		if [ $first = 1 ]; then cs=$CHURN_SERVICES; else cs=0; fi
		first=0
		echo "== procs $p count $n"
		MAXON_MAX_PROCS=$p "$TIMERS" run "$n" "$p" "$STORM_MS" "$cs" "$CHURN_TIMERS" "$CHURN_MS" < /dev/null | sed "s/^result/minimaxon procs=$p/"
		GOMAXPROCS=$p "$GOBENCH" run "$n" "$p" "$STORM_MS" "$cs" "$CHURN_TIMERS" "$CHURN_MS" < /dev/null | sed "s/^result/go procs=$p/"
	done
done
echo "== idle"
"$GOBENCH" idle -n "$IDLE_N" -idle "$IDLE" -label minimaxon -- "$TIMERS" idle
"$GOBENCH" idle -n "$IDLE_N" -idle "$IDLE" -label go -- "$GOBENCH" idlechild

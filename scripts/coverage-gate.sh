#!/bin/sh
# Fail when the total statement coverage in a Go coverage profile is below a
# minimum (CONV-005).
#
# Usage: scripts/coverage-gate.sh <coverage profile> <minimum percent>
set -eu

profile="$1"
min="$2"

total=$(go tool cover -func="$profile" | awk '/^total:/ { sub(/%/, "", $NF); print $NF }')
if [ -z "$total" ]; then
	echo "coverage-gate: no total in $profile" >&2
	exit 1
fi

if awk -v t="$total" -v m="$min" 'BEGIN { exit !(t + 0 < m + 0) }'; then
	echo "coverage-gate: total statement coverage is ${total}%, below the ${min}% that CONV-005 requires" >&2
	exit 1
fi
echo "coverage-gate: total statement coverage is ${total}% (minimum ${min}%)"

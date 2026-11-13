#!/bin/sh
# Fail when a direct dependency is not on its newest release and that release
# is more than <max days> old (the update time frame in SECURITY.md, SEC-003).
#
# `go list -m -u` reports the newest release within the same major version,
# excluding pre-releases. Fails closed: a `go list` error, a release time that
# can't be read, or a list with no direct dependency all fail the gate. `-u`
# drops the update silently when the module proxy can't be reached, so each
# direct dependency it reports as up to date is looked up again with @latest,
# which does fail when the lookup fails.
#
# Usage: scripts/dep-age-gate.sh <max days>
set -eu

max="${1:-}"
case "$max" in
'' | *[!0-9]*)
	echo "dep-age-gate: usage: $0 <max days>" >&2
	exit 2
	;;
esac

list=$(go list -m -u -json all)
now=$(date -u +%s)

report=$(printf '%s\n' "$list" | jq -rs --argjson now "$now" --argjson max "$max" '
	[.[] | select((.Main // false) == false and (.Indirect // false) == false)] as $direct
	| if ($direct | length) == 0 then "ERROR no direct dependency listed"
	  else $direct[] | select(.Update != null)
	    | ((.Update.Time | sub("\\.[0-9]+"; "") | fromdateiso8601) as $t
	      | ((($now - $t) / 86400) | floor) as $age
	      | "\(if ($now - $t) > ($max * 86400) then "STALE" else "ok" end) \(.Path) \(.Version) -> \(.Update.Version), released \(.Update.Time[0:10]) (\($age) days ago)")
	  end')

current=$(printf '%s\n' "$list" | jq -rs '
	.[] | select((.Main // false) == false and (.Indirect // false) == false and .Update == null) | .Path')
for path in $current; do
	if ! go list -m -json "${path}@latest" >/dev/null; then
		echo "dep-age-gate: can't look up the newest release of ${path}" >&2
		exit 1
	fi
	report="${report:+${report}
}ok ${path} is on its newest release"
done

if [ -n "$report" ]; then
	printf '%s\n' "$report"
fi

if printf '%s\n' "$report" | grep -q '^ERROR'; then
	exit 1
fi
if printf '%s\n' "$report" | grep -q '^STALE'; then
	echo "dep-age-gate: a direct dependency's newest release is more than ${max} days old; update it (SECURITY.md)" >&2
	exit 1
fi
echo "dep-age-gate: no direct dependency's newest release is more than ${max} days old"

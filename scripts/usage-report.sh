#!/bin/sh
# Report how strspc-CLI is used, from data GitHub already keeps. The CLI
# itself collects nothing.
#
# - Downloads: release asset download counts, per release and per OS/arch.
#   setup-strspc downloads one archive per CI run, so this mostly counts CI
#   runs. Counts are cumulative: compare two snapshots for a period.
# - Pins: the cli-version each repository's workflows set when calling the
#   SteerSpec workflows or setup-strspc (needs a token that can search the
#   org's code; skipped otherwise).
#
# Not counted: `go install`, and a binary downloaded once and run many times.
#
# Usage: scripts/usage-report.sh [--json] [--releases N] [--org ORG] [--from FILE]
# --from renders a snapshot written earlier with --json, without fetching.
# Needs gh (authenticated) and jq.
set -eu

repo=SteerSpec/strspc-CLI
org=SteerSpec
releases=10
json=false
from=
while [ $# -gt 0 ]; do
	case "$1" in
	--json) json=true ;;
	--releases)
		releases="$2"
		shift
		;;
	--org)
		org="$2"
		shift
		;;
	--from)
		from="$2"
		shift
		;;
	*)
		echo "usage-report: unknown argument $1" >&2
		exit 2
		;;
	esac
	shift
done

collect() {
# downloads: one object per release with its archives' counts by os_arch.
downloads=$(gh api "repos/$repo/releases?per_page=$releases" --jq '[.[] | {
	version: (.tag_name | ltrimstr("v")),
	published: .published_at[0:10],
	assets: [.assets[] | select(.name | test("_(darwin|linux|windows)_[a-z0-9]+\\.(tar\\.gz|zip)$"))
		| {platform: (.name | capture("_(?<p>(darwin|linux|windows)_[a-z0-9]+)\\.") | .p), downloads: .download_count}]
} | .total = ([.assets[].downloads] | add // 0)]')

# pins: the workflow files that call a SteerSpec strspc workflow or
# setup-strspc, outside the SteerSpec/.github repository that defines them,
# with the cli-version values they set ("default" when none).
pins='[]'
files=$(
	for q in '"workflows/strspc-check.yml@"' '"workflows/strspc-lint.yml@"' '"workflows/strspc-deps-bump.yml@"' '"actions/setup-strspc"'; do
		gh api -X GET search/code -f q="org:$org $q path:.github/workflows" -f per_page=100 \
			--jq '.items[] | select(.repository.full_name != "'"$org"'/.github") | "\(.repository.full_name) \(.path)"'
	done 2>/dev/null | sort -u
)
if [ -n "$files" ]; then
	pins=$(printf '%s\n' "$files" | while read -r full path; do
		[ -n "$full" ] || continue
		content=$(gh api "repos/$full/contents/$path" --jq .content 2>/dev/null | base64 -d 2>/dev/null) || content=
		# A "cli-version: <value>" line (the reusable workflows' input), or
		# "version: <value>" inside a setup-strspc step. An input
		# declaration has no value, so it isn't a pin.
		versions=$(printf '%s\n' "$content" | awk '
			/^[[:space:]]*-[[:space:]]/ { step = 0 }
			/uses:.*setup-strspc/ { step = 1 }
			{
				line = $0
				sub(/#.*/, "", line)
				if (match(line, /^[[:space:]]*cli-version:[[:space:]]*[^[:space:]]/) ||
				    (step && match(line, /^[[:space:]]*version:[[:space:]]*[^[:space:]]/))) {
					v = line
					sub(/^[^:]*:[[:space:]]*/, "", v)
					sub(/[[:space:]]+$/, "", v)
					gsub(/^["\047]|["\047]$/, "", v)
					print v
				}
			}')
		[ -n "$versions" ] || versions=default
		printf '%s\n' "$versions" | sort -u |
			jq -R --arg repo "$full" --arg file "$path" '{repo: $repo, file: $file, cli_version: .}'
	done | jq -s 'unique')
else
	echo "usage-report: code search found no workflows (or the token can't search $org), so no pins" >&2
fi

jq -n --argjson downloads "$downloads" --argjson pins "$pins" \
	--arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '{generated_at: $at, releases: $downloads, pins: $pins}'
}

if [ -n "$from" ]; then
	report=$(cat "$from")
else
	report=$(collect)
fi

if [ "$json" = true ]; then
	printf '%s\n' "$report"
	exit 0
fi

printf '%s\n' "$report" | jq -r '
	(.releases | map(.total) | add // 0) as $all
	| "## Downloads by release (last \(.releases | length))", "",
	  "| Version | Published | Downloads | Share | By platform |",
	  "| ------- | --------- | --------: | ----: | ----------- |",
	  (.releases[] | "| \(.version) | \(.published) | \(.total) | \(if $all > 0 then (.total * 1000 / $all | round / 10 | tostring) + "%" else "-" end) | \(.assets | sort_by(-.downloads) | map("\(.platform) \(.downloads)") | join(", ")) |"),
	  "",
	  "## cli-version pinned by repository workflows", "",
	  (if (.pins | length) == 0 then "No pins found (or code search unavailable)." else
	    "| Repository | Workflow | cli-version |", "| ---------- | -------- | ----------- |",
	    (.pins | sort_by(.repo, .file)[] | "| \(.repo) | \(.file) | \(.cli_version) |")
	  end),
	  "",
	  "\"default\": the file sets no version, so it gets the reusable workflow'\''s cli-version default (latest). A file listed with a version may also call a workflow at the default."'

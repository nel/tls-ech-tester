#!/usr/bin/env bash
# Validate SemVer 2.0.0 and emit GitHub Actions outputs for publication.
set -euo pipefail
export LC_ALL=C
version="${1:?Usage: release-version.sh VERSION}"
number='(0|[1-9][0-9]*)'
identifier="($number|[0-9]*[A-Za-z-][0-9A-Za-z-]*)"
metadata='[0-9A-Za-z-]+'
pattern="^$number\.$number\.$number(-$identifier(\.$identifier)*)?(\+$metadata(\.$metadata)*)?$"
if [[ ! "$version" =~ $pattern ]]; then
    echo 'Enter a SemVer version such as 1.0.0 or 1.1.0-rc.1, without a v prefix.' >&2
    exit 1
fi
printf 'tag=v%s\n' "$version"
if [[ "${version%%+*}" == *-* ]]; then
    echo 'prerelease=true'
else
    echo 'prerelease=false'
fi

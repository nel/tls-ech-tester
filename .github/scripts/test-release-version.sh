#!/usr/bin/env bash
set -euo pipefail
script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/release-version.sh"
check() {
    local version="$1" prerelease="$2" actual expected
    actual="$(bash "$script" "$version")"
    expected="$(printf 'tag=v%s\nprerelease=%s' "$version" "$prerelease")"
    [[ "$actual" == "$expected" ]] || { echo "Wrong release metadata for $version" >&2; exit 1; }
}
for version in 0.0.0 1.0.0 12.34.56 1.0.0+001 1.0.0+build-1; do
    check "$version" false
done
for version in 1.1.0-rc.1 1.0.0-0.3.7 1.0.0-x.7.z.92 1.0.0-x-y-z.-- 1.0.0-alpha+001 1.0.0-01a; do
    check "$version" true
done
for version in '' 1 1.2 v1.2.3 tls-ech-tester-v2 01.2.3 1.02.3 1.2.03 \
    1.2.3-01 1.2.3-rc.01 1.2.3- 1.2.3+ 1.2.3-rc..1 1.2.3+build..1 \
    '1.2.3 extra' '1.2.3/branch' $'1.2.3\nprerelease=false'; do
    if bash "$script" "$version" >/dev/null 2>&1; then
        echo "Accepted invalid version: $version" >&2
        exit 1
    fi
done
echo 'Release version validation passed.'

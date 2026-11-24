#!/usr/bin/env bash
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at http://mozilla.org/MPL/2.0/.

# Prints the next semver tag for REF (default HEAD) from the Conventional
# Commits since the latest v* tag, or nothing if no commit warrants a release.
#
#   breaking (type!: or BREAKING CHANGE:)  major (minor while < 1.0.0)
#   feat:                                  minor
#   fix: / perf: / deps:                   patch
#   anything else (chore:, ci:, docs:, …)  no release

set -euo pipefail

ref="${1:-HEAD}"

last=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' "$ref" 2>/dev/null || true)

if [[ -z "$last" ]]; then
  range="$ref"
  last="v0.0.0"
else
  range="$last..$ref"
fi

if [[ ! "$last" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
  echo "latest tag $last is not a plain vMAJOR.MINOR.PATCH tag" >&2
  exit 1
fi

major=${BASH_REMATCH[1]} minor=${BASH_REMATCH[2]} patch=${BASH_REMATCH[3]}

bump=none
type_re='^[a-z]+(\([^)]*\))?'

# %x00 separates commits so multi-line bodies stay attached to their subject.
while IFS= read -r -d '' commit; do
  # git log puts a newline between records, so strip it from the front.
  commit=${commit#$'\n'}
  subject=${commit%%$'\n'*}

  if [[ "$subject" =~ ${type_re}!: ]] || grep -qE '^BREAKING[ -]CHANGE:' <<<"$commit"; then
    bump=major
    break
  elif [[ "$subject" =~ ^feat(\([^\)]*\))?: ]]; then
    bump=minor
  elif [[ "$subject" =~ ^(fix|perf|deps)(\([^\)]*\))?: && "$bump" == none ]]; then
    bump=patch
  fi
done < <(git log --format='%s%n%b%x00' "$range")

# Before 1.0.0 a breaking change only bumps the minor version.
if [[ "$bump" == major && "$major" == 0 ]]; then
  bump=minor
fi

case "$bump" in
  major) echo "v$((major + 1)).0.0" ;;
  minor) echo "v$major.$((minor + 1)).0" ;;
  patch) echo "v$major.$minor.$((patch + 1))" ;;
  none) ;;
esac

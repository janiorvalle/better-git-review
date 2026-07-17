#!/bin/sh
set -eu

if [ "$#" -ne 5 ]; then
  echo "usage: fetch-history.sh CACHE_DIR NAME REPOSITORY BASE_REF HEAD_REF" >&2
  exit 2
fi

cache_dir=$1
name=$2
repository=$3
base_ref=$4
head_ref=$5
patch_path="$cache_dir/$name.patch"

mkdir -p "$cache_dir"
if [ -s "$patch_path" ]; then
  printf '%s\n' "$patch_path"
  exit 0
fi

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/bgr-history.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM
git init -q --bare "$work_dir/repo.git"
git --git-dir="$work_dir/repo.git" fetch -q --depth=1 \
  "$repository" \
  "refs/tags/$base_ref:refs/tags/$base_ref" \
  "refs/tags/$head_ref:refs/tags/$head_ref"
git --git-dir="$work_dir/repo.git" diff --binary "$base_ref" "$head_ref" > "$work_dir/history.patch"
test -s "$work_dir/history.patch"
mv "$work_dir/history.patch" "$patch_path"
printf '%s\n' "$patch_path"

#!/bin/sh
set -eu

repo="${BGR_INSTALL_REPO:-janiorvalle/better-git-review}"
install_dir="${BGR_INSTALL_DIR:-$HOME/.local/bin}"
base_url="${BGR_INSTALL_BASE_URL:-}"
version="${BGR_INSTALL_VERSION:-}"
archive_name="${BGR_INSTALL_ARCHIVE:-}"

fail() {
  printf 'bgr installer: %s\n' "$*" >&2
  exit 1
}

command -v curl >/dev/null 2>&1 || fail "curl is required"

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) fail "unsupported operating system; Windows users should download the release zip" ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

if [ -z "$version" ]; then
  release_json=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest") || fail "no published release found for $repo yet - releases appear at https://github.com/$repo/releases, or build from source: go install github.com/$repo/cmd/bgr@latest"
  version=$(printf '%s\n' "$release_json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' | head -n 1)
  [ -n "$version" ] || fail "latest release did not include a tag_name"
fi
version=${version#v}

if [ -z "$archive_name" ]; then
  archive_name="better-git-review_${version}_${os}_${arch}.tar.gz"
fi
if [ -z "$base_url" ]; then
  base_url="https://github.com/$repo/releases/download/v$version"
fi
base_url=${base_url%/}

tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t bgr-install)
stage_bgr=""
stage_long=""
dest_bgr="$install_dir/bgr"
dest_long="$install_dir/better-git-review"
backup_bgr="$tmp_dir/previous-bgr"
backup_long="$tmp_dir/previous-better-git-review"
had_bgr=false
had_long=false
transaction_active=false
restore_install() {
  transaction_active=false
  if [ "$had_bgr" = true ]; then
    mv -f "$backup_bgr" "$dest_bgr"
  else
    rm -f "$dest_bgr"
  fi
  if [ "$had_long" = true ]; then
    mv -f "$backup_long" "$dest_long"
  else
    rm -f "$dest_long"
  fi
}
cleanup() {
  [ "$transaction_active" = false ] || restore_install
  rm -rf "$tmp_dir"
  [ -z "$stage_bgr" ] || rm -f "$stage_bgr"
  [ -z "$stage_long" ] || rm -f "$stage_long"
}
trap cleanup EXIT HUP INT TERM
archive="$tmp_dir/$archive_name"
checksums="$tmp_dir/checksums.txt"

printf 'Downloading bgr %s for %s/%s...\n' "$version" "$os" "$arch"
curl -fsSL "$base_url/$archive_name" -o "$archive" || fail "could not download $archive_name"
curl -fsSL "$base_url/checksums.txt" -o "$checksums" || fail "could not download checksums.txt"

expected=$(awk -v file="$archive_name" '$2 == file || $2 == "*" file { print $1; exit }' "$checksums")
[ -n "$expected" ] || fail "checksums.txt has no entry for $archive_name"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$archive" | awk '{print $1}')
else
  fail "sha256sum or shasum is required to verify the download"
fi
[ "$actual" = "$expected" ] || fail "checksum mismatch for $archive_name"

tar -xzf "$archive" -C "$tmp_dir"
[ -x "$tmp_dir/bgr" ] || fail "archive did not contain bgr"
[ -x "$tmp_dir/better-git-review" ] || fail "archive did not contain better-git-review"
bgr_version=$("$tmp_dir/bgr" --version) || fail "release bgr failed its version smoke test"
long_version=$("$tmp_dir/better-git-review" --version) || fail "release better-git-review failed its version smoke test"
[ "$bgr_version" = "$long_version" ] || fail "release binaries reported different versions"

mkdir -p "$install_dir"
stage_bgr="$install_dir/.bgr.new.$$"
stage_long="$install_dir/.better-git-review.new.$$"
install -m 0755 "$tmp_dir/bgr" "$stage_bgr"
install -m 0755 "$tmp_dir/better-git-review" "$stage_long"
if [ -e "$dest_bgr" ] || [ -L "$dest_bgr" ]; then
  cp -p "$dest_bgr" "$backup_bgr"
  had_bgr=true
fi
if [ -e "$dest_long" ] || [ -L "$dest_long" ]; then
  cp -p "$dest_long" "$backup_long"
  had_long=true
fi
transaction_active=true
mv -f "$stage_bgr" "$dest_bgr" || fail "could not replace bgr"
stage_bgr=""
mv -f "$stage_long" "$dest_long" || fail "could not replace better-git-review"
stage_long=""

"$dest_bgr" --version >/dev/null || fail "installed bgr failed its version smoke test"
"$dest_long" --version >/dev/null || fail "installed better-git-review failed its version smoke test"
transaction_active=false
printf 'Installed bgr and better-git-review to %s\n' "$install_dir"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) printf 'Add %s to PATH to run bgr from any directory.\n' "$install_dir" ;;
esac

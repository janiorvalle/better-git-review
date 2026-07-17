#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) printf 'install smoke is only used on Unix release targets\n'; exit 0 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) printf 'unsupported smoke architecture\n' >&2; exit 1 ;;
esac

archive=$(find "$root/dist" -maxdepth 1 -type f -name "better-git-review_*_${os}_${arch}.tar.gz" -print | head -n 1)
[ -n "$archive" ] || { printf 'snapshot archive not found for %s/%s\n' "$os" "$arch" >&2; exit 1; }
archive_name=$(basename "$archive")
version=${archive_name#better-git-review_}
version=${version%_"${os}"_"${arch}".tar.gz}
install_dir=$(mktemp -d 2>/dev/null || mktemp -d -t bgr-install-smoke)
trap 'rm -rf "$install_dir"' EXIT HUP INT TERM

BGR_INSTALL_BASE_URL="file://$root/dist" \
BGR_INSTALL_VERSION="$version" \
BGR_INSTALL_ARCHIVE="$archive_name" \
BGR_INSTALL_DIR="$install_dir/bin" \
  "$root/install.sh"

# Re-running over an existing installation is intentionally supported.
BGR_INSTALL_BASE_URL="file://$root/dist" \
BGR_INSTALL_VERSION="$version" \
BGR_INSTALL_ARCHIVE="$archive_name" \
BGR_INSTALL_DIR="$install_dir/bin" \
  "$root/install.sh"

"$install_dir/bin/bgr" --version
"$install_dir/bin/better-git-review" --version

bad_dist="$install_dir/bad-dist"
mkdir -p "$bad_dist"
cp "$archive" "$bad_dist/$archive_name"
printf '%064d  %s\n' 0 "$archive_name" > "$bad_dist/checksums.txt"
if BGR_INSTALL_BASE_URL="file://$bad_dist" \
  BGR_INSTALL_VERSION="$version" \
  BGR_INSTALL_ARCHIVE="$archive_name" \
  BGR_INSTALL_DIR="$install_dir/bad-bin" \
  "$root/install.sh" >"$install_dir/bad.log" 2>&1; then
  printf 'installer accepted a checksum mismatch\n' >&2
  exit 1
fi
grep -q 'checksum mismatch' "$install_dir/bad.log"

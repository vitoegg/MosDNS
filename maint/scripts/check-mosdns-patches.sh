#!/bin/sh

set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
TARGET_DIR=${1:-$REPO_ROOT}
TARGET_DIR=$(CDPATH= cd -- "$TARGET_DIR" && pwd)
MAKEFILE="$TARGET_DIR/mosdns/Makefile"

version=$(sed -n 's/^PKG_VERSION:=//p' "$MAKEFILE")
source_hash=$(sed -n 's/^PKG_HASH:=//p' "$MAKEFILE")
source_url=$(sed -n 's/^PKG_SOURCE_URL:=//p' "$MAKEFILE")
[ -n "$version" ] && [ -n "$source_hash" ] && [ -n "$source_url" ]
source_url=$(printf '%s\n' "$source_url" | sed "s/\$(PKG_VERSION)/$version/g")
case "$source_url" in
    *'$('* ) echo "Unsupported source URL: $source_url" >&2; exit 1 ;;
esac

temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/mosdns-patches.XXXXXX")
trap 'rm -rf "$temp_dir"' EXIT
trap 'exit 1' HUP INT TERM

curl --fail --location --retry 3 --connect-timeout 20 --max-time 180 \
    "$source_url" -o "$temp_dir/source.tar.gz"
actual_hash=$(shasum -a 256 "$temp_dir/source.tar.gz" | awk '{print $1}')
[ "$actual_hash" = "$source_hash" ] || {
    echo "MosDNS source checksum mismatch" >&2
    exit 1
}
tar -xzf "$temp_dir/source.tar.gz" -C "$temp_dir"
source_dir="$temp_dir/mosdns-$version"
[ -d "$source_dir" ]

export LC_ALL=C
for patch_file in "$TARGET_DIR"/mosdns/patches/*.patch; do
    echo "[check] $(basename "$patch_file")"
    patch -d "$source_dir" -p1 --batch --forward --fuzz=0 -i "$patch_file"
done

# maint/tests mirrors the source tree, so each test file lands in the package
# it covers. Tests are kept out of the patches to keep the shipped diff minimal.
test_pkgs=""
for test_file in $(cd "$TARGET_DIR/maint/tests" && find . -name '*_test.go' | sed 's|^\./||' | sort); do
    test_pkg=$(dirname "$test_file")
    mkdir -p "$source_dir/$test_pkg"
    cp "$TARGET_DIR/maint/tests/$test_file" "$source_dir/$test_pkg/"
    case " $test_pkgs " in
        *" ./$test_pkg "*) ;;
        *) test_pkgs="$test_pkgs ./$test_pkg" ;;
    esac
done
[ -n "$test_pkgs" ]

cd "$source_dir"
echo "[check] go test$test_pkgs ./pkg/hosts"
# shellcheck disable=SC2086
go test $test_pkgs ./pkg/hosts
go build -o "$temp_dir/mosdns" .
echo "[check] MosDNS patch stack, regression tests and build passed"

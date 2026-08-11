#!/bin/sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
PATCH_DIR="$REPO_ROOT/maint/patches"
REMOVE_MANIFEST="$REPO_ROOT/maint/manifests/remove-paths.txt"
RESTORE_MANIFEST="$REPO_ROOT/maint/manifests/restore-paths.txt"
RESTORE_SKIP_PATHS="${RESTORE_SKIP_PATHS:-}"

# Single source of truth for the LUCI_DEPENDS contract: geodata deps are
# stripped from whatever upstream declares, the rest is passed through.
GEODATA_DEPS='+v2ray-geoip +v2ray-geosite +v2dat'
REQUIRED_DEPS='+mosdns +curl'

usage() {
	echo "Usage: sh maint/scripts/apply-customizations.sh <target-dir>" >&2
	exit 1
}

require_file() {
	[ -f "$1" ] || {
		echo "Missing file: $1" >&2
		exit 1
	}
}

validate_restore_paths() {
	while IFS= read -r path || [ -n "$path" ]; do
		case "$path" in
			''|\#*)
				continue
				;;
		esac

		[ -e "$REPO_ROOT/$path" ] || {
			echo "Missing restore path in repo: $path" >&2
			exit 1
		}
	done < "$RESTORE_MANIFEST"
}

remove_upstream_paths() {
	[ -f "$REMOVE_MANIFEST" ] || {
		echo "Missing remove manifest: $REMOVE_MANIFEST" >&2
		exit 1
	}

	while IFS= read -r path || [ -n "$path" ]; do
		case "$path" in
			''|\#*)
				continue
				;;
		esac
		rm -rf "$TARGET_DIR/$path"
	done < "$REMOVE_MANIFEST"
}

restore_custom_paths() {
	while IFS= read -r path || [ -n "$path" ]; do
		case "$path" in
			''|\#*)
				continue
				;;
		esac

		if restore_path_is_skipped "$path"; then
			printf '[apply] restore skipped %s\n' "$path"
			continue
		fi

		rm -rf "$TARGET_DIR/$path"
		tar -C "$REPO_ROOT" -cf - "$path" | tar -C "$TARGET_DIR" -xf -
	done < "$RESTORE_MANIFEST"
}

restore_path_is_skipped() {
	path_to_check="$1"
	for skipped_path in $RESTORE_SKIP_PATHS; do
		[ "$path_to_check" = "$skipped_path" ] && return 0
	done
	return 1
}

apply_transforms() {
	makefile="$TARGET_DIR/luci-app-mosdns/Makefile"

	[ -f "$makefile" ] || {
		echo "Missing target file: luci-app-mosdns/Makefile" >&2
		exit 1
	}

	printf '[transform] before: %s\n' "$(grep -E '^LUCI_DEPENDS:=' "$makefile" || echo '<none>')"

	awk -v drop_list="$GEODATA_DEPS" '
	BEGIN {
		split(drop_list, d, " ")
		for (i in d) drop[d[i]] = 1
	}
	/^LUCI_DEPENDS:=/ {
		sub(/^LUCI_DEPENDS:=/, "")
		n = split($0, deps, / +/)
		kept = ""
		for (i = 1; i <= n; i++)
			if (deps[i] != "" && !(deps[i] in drop))
				kept = kept (kept == "" ? "" : " ") deps[i]
		print "LUCI_DEPENDS:=" kept
		next
	}
	{ print }
	' "$makefile" > "$makefile.tmp" && mv "$makefile.tmp" "$makefile"

	printf '[transform] after : %s\n' "$(grep -E '^LUCI_DEPENDS:=' "$makefile" || echo '<none>')"
}

apply_patch_stack() {
	find "$PATCH_DIR" -maxdepth 1 -type f -name '*.patch' | sort | while IFS= read -r patch_path; do
		patch_name=$(basename "$patch_path")
		printf '[apply] %s\n' "$patch_name"
		git -C "$TARGET_DIR" apply --whitespace=nowarn "$patch_path"
	done
}

require_single_occurrence() {
	file="$1"
	pattern="$2"
	label="$3"
	count=$(grep -F -c "$pattern" "$file" || true)
	[ "$count" = 1 ] || {
		echo "Invalid $label occurrence count: $count" >&2
		exit 1
	}
}

require_absent() {
	file="$1"
	pattern="$2"
	label="$3"
	if grep -Fq "$pattern" "$file"; then
		echo "Unexpected $label present in $file" >&2
		exit 1
	fi
}

validate_luci_depends() {
	makefile="$1"

	depends_count=$(grep -E -c '^LUCI_DEPENDS:=' "$makefile" || true)
	[ "$depends_count" = 1 ] || {
		echo "Invalid LUCI_DEPENDS line count: $depends_count" >&2
		exit 1
	}

	depends_line=$(grep -E '^LUCI_DEPENDS:=' "$makefile")
	# Pad with spaces so every dep is delimited on both sides, making the
	# case patterns below match whole tokens only (+v2dat vs +v2dat-ng).
	depends_value=" ${depends_line#LUCI_DEPENDS:=} "

	for dep in $REQUIRED_DEPS; do
		case "$depends_value" in
			*" $dep "*) ;;
			*)
				echo "Missing required dependency $dep in: $depends_line" >&2
				exit 1
				;;
		esac
	done

	for dep in $GEODATA_DEPS; do
		case "$depends_value" in
			*" $dep "*)
				echo "Unexpected dependency $dep in: $depends_line" >&2
				exit 1
				;;
		esac
	done
}

validate_mosdns_customizations() {
	makefile="$TARGET_DIR/luci-app-mosdns/Makefile"
	init_file="$TARGET_DIR/luci-app-mosdns/root/etc/init.d/mosdns"
	uc_file="$TARGET_DIR/luci-app-mosdns/root/usr/share/mosdns/mosdns.uc"

	[ -f "$makefile" ] || {
		echo "Missing target file: luci-app-mosdns/Makefile" >&2
		exit 1
	}
	[ -f "$init_file" ] || {
		echo "Missing target file: luci-app-mosdns/root/etc/init.d/mosdns" >&2
		exit 1
	}
	[ -f "$uc_file" ] || {
		echo "Missing target file: luci-app-mosdns/root/usr/share/mosdns/mosdns.uc" >&2
		exit 1
	}

	validate_luci_depends "$makefile"
	# Catches upstream renaming a geodata dep past the transform (e.g. -lite),
	# which validate_luci_depends alone would let through.
	for dep in $GEODATA_DEPS; do
		require_absent "$makefile" "${dep#+}" "${dep#+} dependency"
	done

	require_absent "$init_file" 'CONF=$(uci -q get mosdns.config.configfile)' "top-level CONF uci call"
	require_absent "$init_file" 'v2dat_dump' "v2dat_dump startup call"
	# Match the call sites only: the '();' suffix keeps the still-present
	# 'function update_geodat() {' / 'function v2dat_dump() {' definitions
	# from tripping this, so a call re-added anywhere in mosdns.uc is caught.
	require_absent "$uc_file" 'update_geodat();' "update_geodat call"
	require_absent "$uc_file" 'v2dat_dump();' "v2dat_dump call"
	require_single_occurrence "$init_file" 'config_get CONF $1 configfile "/var/etc/mosdns.json"' "runtime CONF config_get"

	[ ! -e "$TARGET_DIR/v2dat" ] || {
		echo "Unexpected path present: v2dat" >&2
		exit 1
	}

	[ -f "$TARGET_DIR/mosdns/patches/000-add-query_set-domain-set-plugin.patch" ] || {
		echo "Missing target file: mosdns/patches/000-add-query_set-domain-set-plugin.patch" >&2
		exit 1
	}
}

TARGET_DIR=""
TEMP_GIT_REPO_CREATED=0

[ "$#" -eq 1 ] || usage
require_file "$REMOVE_MANIFEST"
require_file "$RESTORE_MANIFEST"
validate_restore_paths

SOURCE_TARGET=$(CDPATH= cd -- "$1" && pwd)
[ -d "$SOURCE_TARGET" ] || {
	echo "Target directory does not exist: $SOURCE_TARGET" >&2
	exit 1
}

[ "$SOURCE_TARGET" != "$REPO_ROOT" ] || {
	echo "Target directory must differ from repo root: $SOURCE_TARGET" >&2
	exit 1
}

cleanup() {
	if [ "$TEMP_GIT_REPO_CREATED" = 1 ] && [ -n "$TARGET_DIR" ] && [ -d "$TARGET_DIR/.git" ]; then
		rm -rf "$TARGET_DIR/.git"
	fi
}

trap cleanup EXIT INT TERM

TARGET_DIR="$SOURCE_TARGET"

if ! git -C "$TARGET_DIR" rev-parse --show-toplevel > /dev/null 2>&1; then
	git -C "$TARGET_DIR" init -q
	TEMP_GIT_REPO_CREATED=1
fi

remove_upstream_paths
echo "[apply] cleanup removed paths"

apply_transforms
echo "[apply] transform upstream sources"

apply_patch_stack

restore_custom_paths
echo "[apply] restore custom paths"

validate_mosdns_customizations
echo "[apply] validate mosdns customizations"

#!/bin/sh

workflow_writeback_config_git() {
	git config user.name "${WORKFLOW_GIT_USER_NAME:-github-actions[bot]}"
	git config user.email "${WORKFLOW_GIT_USER_EMAIL:-github-actions[bot]@users.noreply.github.com}"
}

workflow_writeback_has_staged_changes() {
	! git diff --cached --quiet --ignore-submodules --
}

workflow_writeback_head_sha() {
	git rev-parse HEAD
}

workflow_writeback_set_output() {
	output_name="$1"
	output_value="$2"
	output_file="${GITHUB_OUTPUT:-}"

	[ -n "$output_file" ] || return 0
	printf '%s=%s\n' "$output_name" "$output_value" >> "$output_file"
}

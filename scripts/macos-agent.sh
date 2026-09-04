#!/bin/bash

set -euo pipefail
umask 077

action="${1:-}"
label="${ORBIT_AGENT_LABEL:-com.leo.orbit.agent}"
legacy_label="${ORBIT_AGENT_LEGACY_LABEL:-com.leo.orbit.agent.dev}"
install_dir="${ORBIT_AGENT_INSTALL_DIR:-${HOME}/Library/Application Support/Orbit}"
plist="${ORBIT_AGENT_PLIST:-${HOME}/Library/LaunchAgents/${label}.plist}"
log_dir="${ORBIT_AGENT_LOG_DIR:-${HOME}/Library/Logs/Orbit}"
launchctl_bin="${LAUNCHCTL_BIN:-/bin/launchctl}"
plutil_bin="${PLUTIL_BIN:-/usr/bin/plutil}"
go_bin="${GO_BIN:-go}"
uid="$(/usr/bin/id -u)"
domain="gui/${uid}"
binary="${install_dir}/bin/orbit-agent"
stdout_log="${log_dir}/agent.stdout.log"
stderr_log="${log_dir}/agent.stderr.log"

binary_tmp=""
plist_tmp=""
backup_dir=""
had_binary=0
had_plist=0
was_loaded=0
was_legacy_loaded=0
rollback_needed=0

die() {
	printf 'orbit-agent: %s\n' "$*" >&2
	exit 1
}

require_executable() {
	if [[ "$1" == */* ]]; then
		[[ -x "$1" ]] || die "required executable not found: $1"
	else
		command -v "$1" >/dev/null 2>&1 || die "required executable not found: $1"
	fi
}

validate_path() {
	case "$2" in
		/*) ;;
		*) die "$1 must be an absolute path: $2" ;;
	esac
	case "$2" in
		*$'\n'*|*$'\r'*) die "$1 must not contain newlines" ;;
	esac
}

xml_escape() {
	printf '%s' "$1" | /usr/bin/sed \
		-e 's/&/\&amp;/g' \
		-e 's/</\&lt;/g' \
		-e 's/>/\&gt;/g'
}

is_loaded() {
	"$launchctl_bin" print "${domain}/$1" >/dev/null 2>&1
}

stop_label() {
	local target_label="$1"
	local output
	local attempt

	if ! is_loaded "$target_label"; then
		return 0
	fi
	if ! output="$("$launchctl_bin" bootout "${domain}/${target_label}" 2>&1)"; then
		if is_loaded "$target_label"; then
			printf 'orbit-agent: failed to stop %s: %s\n' "$target_label" "$output" >&2
			return 1
		fi
	fi
	for ((attempt = 0; attempt < 50; attempt++)); do
		if ! is_loaded "$target_label"; then
			return 0
		fi
		sleep 0.1
	done
	printf 'orbit-agent: timed out stopping %s\n' "$target_label" >&2
	return 1
}

stop_all_labels() {
	stop_label "$label"
	if [[ -n "$legacy_label" && "$legacy_label" != "$label" ]]; then
		stop_label "$legacy_label"
	fi
}

render_plist() {
	local label_xml
	local binary_xml
	local config_xml
	local stdout_xml
	local stderr_xml

	label_xml="$(xml_escape "$label")"
	binary_xml="$(xml_escape "$binary")"
	config_xml="$(xml_escape "$ORBIT_AGENT_CONFIG")"
	stdout_xml="$(xml_escape "$stdout_log")"
	stderr_xml="$(xml_escape "$stderr_log")"

	cat >"$plist_tmp" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${label_xml}</string>
  <key>ProgramArguments</key>
  <array>
    <string>${binary_xml}</string>
    <string>-config</string>
    <string>${config_xml}</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Background</string>
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>StandardOutPath</key>
  <string>${stdout_xml}</string>
  <key>StandardErrorPath</key>
  <string>${stderr_xml}</string>
</dict>
</plist>
EOF
}

restore_previous_install() {
	stop_label "$label" >/dev/null 2>&1 || true
	if [[ "$had_binary" -eq 1 ]]; then
		/bin/cp -p "${backup_dir}/orbit-agent" "$binary"
	else
		/bin/rm -f "$binary"
	fi
	if [[ "$had_plist" -eq 1 ]]; then
		/bin/cp -p "${backup_dir}/agent.plist" "$plist"
	else
		/bin/rm -f "$plist"
	fi
	if [[ "$was_loaded" -eq 1 && "$had_plist" -eq 1 ]]; then
		"$launchctl_bin" enable "${domain}/${label}" >/dev/null 2>&1 || true
		"$launchctl_bin" bootstrap "$domain" "$plist" >/dev/null 2>&1 || true
		"$launchctl_bin" kickstart -k "${domain}/${label}" >/dev/null 2>&1 || true
	fi
	if [[ "$was_legacy_loaded" -eq 1 && -f "$(/usr/bin/dirname "$plist")/${legacy_label}.plist" ]]; then
		"$launchctl_bin" bootstrap "$domain" "$(/usr/bin/dirname "$plist")/${legacy_label}.plist" >/dev/null 2>&1 || true
	fi
}

cleanup() {
	local status="$?"
	trap - EXIT
	if [[ "$status" -ne 0 && "$rollback_needed" -eq 1 ]]; then
		printf 'orbit-agent: update failed; restoring previous installation\n' >&2
		restore_previous_install || true
	fi
	[[ -z "$binary_tmp" ]] || /bin/rm -f "$binary_tmp"
	[[ -z "$plist_tmp" ]] || /bin/rm -f "$plist_tmp"
	if [[ -n "$backup_dir" ]]; then
		/bin/rm -f "${backup_dir}/orbit-agent" "${backup_dir}/agent.plist"
		/bin/rmdir "$backup_dir" 2>/dev/null || true
	fi
	exit "$status"
}

install_agent() {
	local repo_root="${ORBIT_REPO_ROOT:-}"
	local config="${ORBIT_AGENT_CONFIG:-}"
	local binary_dir="${install_dir}/bin"
	local plist_dir
	local legacy_plist

	[[ -n "$repo_root" ]] || die "ORBIT_REPO_ROOT is required"
	[[ -n "$config" ]] || die "ORBIT_AGENT_CONFIG is required"
	validate_path "ORBIT_REPO_ROOT" "$repo_root"
	validate_path "ORBIT_AGENT_CONFIG" "$config"
	[[ -d "$repo_root" ]] || die "repository directory not found: $repo_root"
	[[ -f "$config" && -r "$config" ]] || die "agent config is not a readable file: $config"
	require_executable "$go_bin"
	require_executable "$plutil_bin"

	plist_dir="$(/usr/bin/dirname "$plist")"
	legacy_plist="${plist_dir}/${legacy_label}.plist"
	/usr/bin/install -d -m 700 "$binary_dir" "$log_dir"
	if [[ ! -d "$plist_dir" ]]; then
		/usr/bin/install -d -m 700 "$plist_dir"
	fi
	binary_tmp="$(/usr/bin/mktemp "${binary_dir}/.orbit-agent.XXXXXX")"
	plist_tmp="$(/usr/bin/mktemp "${plist_dir}/.${label}.plist.XXXXXX")"
	backup_dir="$(/usr/bin/mktemp -d "${install_dir}/.agent-backup.XXXXXX")"

	(
		cd "$repo_root"
		"$go_bin" build -trimpath -o "$binary_tmp" ./cmd/orbit-agent
	)
	/bin/chmod 755 "$binary_tmp"
	"$binary_tmp" -config "$config" -check-config
	ORBIT_AGENT_CONFIG="$config"
	render_plist
	/bin/chmod 600 "$plist_tmp"
	"$plutil_bin" -lint "$plist_tmp" >/dev/null

	if [[ -f "$binary" ]]; then
		had_binary=1
		/bin/cp -p "$binary" "${backup_dir}/orbit-agent"
	fi
	if [[ -f "$plist" ]]; then
		had_plist=1
		/bin/cp -p "$plist" "${backup_dir}/agent.plist"
	fi
	if is_loaded "$label"; then
		was_loaded=1
	fi
	if [[ "$legacy_label" != "$label" ]] && is_loaded "$legacy_label"; then
		was_legacy_loaded=1
	fi

	rollback_needed=1
	stop_all_labels
	/bin/mv -f "$binary_tmp" "$binary"
	binary_tmp=""
	/bin/mv -f "$plist_tmp" "$plist"
	plist_tmp=""
	"$launchctl_bin" enable "${domain}/${label}"
	"$launchctl_bin" bootstrap "$domain" "$plist"
	"$launchctl_bin" kickstart -k "${domain}/${label}"
	is_loaded "$label" || die "launchd did not register ${label}"
	rollback_needed=0

	if [[ "$legacy_label" != "$label" ]]; then
		/bin/rm -f "$legacy_plist"
	fi
	/bin/rm -f "${backup_dir}/orbit-agent" "${backup_dir}/agent.plist"
	/bin/rmdir "$backup_dir"
	backup_dir=""
	printf 'Installed and started %s\n' "$label"
	printf '  binary: %s\n' "$binary"
	printf '  config: %s\n' "$config"
	printf '  plist:  %s\n' "$plist"
	printf '  logs:   %s\n' "$log_dir"
}

stop_agent() {
	stop_all_labels
	printf 'Stopped Orbit Agent (already stopped is OK).\n'
}

uninstall_agent() {
	local plist_dir
	local legacy_plist

	plist_dir="$(/usr/bin/dirname "$plist")"
	legacy_plist="${plist_dir}/${legacy_label}.plist"
	stop_all_labels
	/bin/rm -f "$plist" "$binary"
	if [[ "$legacy_label" != "$label" ]]; then
		/bin/rm -f "$legacy_plist"
	fi
	/bin/rmdir "${install_dir}/bin" "$install_dir" 2>/dev/null || true
	printf 'Uninstalled Orbit Agent. Logs were preserved in %s\n' "$log_dir"
}

[[ "$(/usr/bin/uname -s)" == "Darwin" ]] || die "this command supports macOS only"
[[ "$uid" -ne 0 ]] || die "run as the logged-in user, not root"
[[ "$label" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]] || die "invalid launchd label: $label"
[[ -z "$legacy_label" || "$legacy_label" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]] || \
	die "invalid legacy launchd label: $legacy_label"
validate_path "ORBIT_AGENT_INSTALL_DIR" "$install_dir"
validate_path "ORBIT_AGENT_PLIST" "$plist"
validate_path "ORBIT_AGENT_LOG_DIR" "$log_dir"
require_executable "$launchctl_bin"

trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

case "$action" in
	install) install_agent ;;
	stop) stop_agent ;;
	uninstall) uninstall_agent ;;
	*) die "usage: $0 {install|stop|uninstall}" ;;
esac

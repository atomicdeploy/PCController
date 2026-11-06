#!/usr/bin/env bash

set -Eeuo pipefail

tools_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
terminal_script="$tools_root/terminal.sh"
escape=$'\033'

fail() {
    printf 'terminal test failed: %s\n' "$*" >&2
    exit 1
}

colored="$(
    env -u NO_COLOR -u CLICOLOR -u FORCE_COLOR \
        -u PCCONTROLLER_DESKTOP_FORCE_COLOR -u NO_EMOJI \
        CLICOLOR_FORCE=1 PCCONTROLLER_DESKTOP_EMOJI=1 TERM=xterm-256color \
        bash -c 'source "$1"; rl_success "release ready"' bash "$terminal_script"
)"
expected_prefix=$'\033[1;32m\u2705 OK\033[0m: '
[[ "$colored" == "$expected_prefix"* ]] ||
    fail "forced success output does not use the expected SGR opener and reset"
[[ "${colored//[^$'\033']/}" == "$escape$escape" ]] ||
    fail "forced output contains an unexpected number of escape characters"

plain="$(
    env -u CLICOLOR -u FORCE_COLOR -u PCCONTROLLER_DESKTOP_FORCE_COLOR -u NO_EMOJI \
        NO_COLOR=1 CLICOLOR_FORCE=1 PCCONTROLLER_DESKTOP_EMOJI=1 TERM=xterm-256color \
        bash -c 'source "$1"; rl_success "release ready"' bash "$terminal_script"
)"
[[ "$plain" != *"$escape"* ]] ||
    fail "NO_COLOR output leaked an escape character"
[[ "$plain" == *"OK: release ready" ]] ||
    fail "plain output lost its readable status label"

ascii="$(
    env -u CLICOLOR -u CLICOLOR_FORCE -u FORCE_COLOR \
        -u PCCONTROLLER_DESKTOP_FORCE_COLOR -u PCCONTROLLER_DESKTOP_EMOJI \
        NO_COLOR=1 NO_EMOJI=1 \
        bash -c 'source "$1"; rl_warn "sample"' bash "$terminal_script" 2>&1
)"
[[ "$ascii" == "WARNING: sample" ]] ||
    fail "NO_EMOJI output is not the stable ASCII fallback"

redirected="$(
    env -u NO_COLOR -u CLICOLOR -u CLICOLOR_FORCE -u FORCE_COLOR \
        -u PCCONTROLLER_DESKTOP_FORCE_COLOR -u NO_EMOJI -u PCCONTROLLER_DESKTOP_EMOJI \
        TERM=xterm-256color LANG=C.UTF-8 \
        bash -c 'source "$1"; rl_info "sample"' bash "$terminal_script"
)"
[[ "$redirected" == "INFO: sample" ]] ||
    fail "redirected output must use the plain ASCII fallback by default"

disabled_color="$(
    env -u NO_COLOR -u CLICOLOR -u PCCONTROLLER_DESKTOP_FORCE_COLOR -u NO_EMOJI \
        FORCE_COLOR=0 CLICOLOR_FORCE=1 PCCONTROLLER_DESKTOP_EMOJI=0 TERM=xterm-256color \
        bash -c 'source "$1"; rl_success "sample"' bash "$terminal_script"
)"
[[ "$disabled_color" == "OK: sample" ]] ||
    fail "FORCE_COLOR=0 must override forced color"

for script in "$tools_root"/*.sh; do
    bash -n "$script"
    LC_ALL=C grep -n $'\033' "$script" >/dev/null 2>&1 &&
        fail "$(basename "$script") contains a literal ESC character"
done

source "$terminal_script"
rl_success "Bash parsing, VT-100, reset, fallback, and NO_COLOR checks passed"

#!/usr/bin/env bash

# Shared terminal presentation for PCControllerDesktop's Bash tooling. The source is
# ASCII-only; emoji glyphs are assembled from Unicode code points at runtime.

rl_terminal_reset=""
rl_terminal_cyan=""
rl_terminal_green=""
rl_terminal_yellow=""
rl_terminal_red=""

rl_environment_value() {
    local value="${!1:-}"
    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    printf '%s' "${value,,}"
}

rl_environment_enabled() {
    local value
    value="$(rl_environment_value "$1")"
    [[ -n "$value" && ! "$value" =~ ^(0|false|no|off)$ ]]
}

rl_color_requested() {
    if [[ -n "${NO_COLOR:-}" || "$(rl_environment_value TERM)" == "dumb" ]]; then
        return 1
    fi
    if [[ "$(rl_environment_value CLICOLOR)" == "0" ||
        "$(rl_environment_value FORCE_COLOR)" == "0" ]]; then
        return 1
    fi
    return 0
}

rl_force_color_requested() {
    if [[ -n "$(rl_environment_value FORCE_COLOR)" ]]; then
        rl_environment_enabled FORCE_COLOR
        return
    fi
    if rl_environment_enabled CLICOLOR_FORCE ||
        rl_environment_enabled PCCONTROLLER_DESKTOP_FORCE_COLOR; then
        return 0
    fi
    return 1
}

rl_terminal_init() {
    local force_color=false
    if rl_force_color_requested; then
        force_color=true
    fi

    if rl_color_requested &&
        { [[ "$force_color" == true ]] || [[ -t 1 ]]; }; then
        rl_terminal_reset=$'\033[0m'
        rl_terminal_cyan=$'\033[1;36m'
        rl_terminal_green=$'\033[1;32m'
        rl_terminal_yellow=$'\033[1;33m'
        rl_terminal_red=$'\033[1;31m'
    else
        rl_terminal_reset=""
        rl_terminal_cyan=""
        rl_terminal_green=""
        rl_terminal_yellow=""
        rl_terminal_red=""
    fi
}

rl_emoji_enabled() {
    local stream="${1:-stdout}"
    if rl_environment_enabled NO_EMOJI ||
        [[ "$(rl_environment_value TERM)" == "dumb" ]]; then
        return 1
    fi
    local emoji_preference
    emoji_preference="$(rl_environment_value PCCONTROLLER_DESKTOP_EMOJI)"
    if [[ -n "$emoji_preference" ]]; then
        case "$emoji_preference" in
            1|always|force|on|true|yes) return 0 ;;
            0|never|off|false|no) return 1 ;;
        esac
    fi
    local locale_value
    locale_value="${LC_ALL:-} ${LC_CTYPE:-} ${LANG:-}"
    locale_value="${locale_value,,}"
    if [[ "$locale_value" != *"utf-8"* && "$locale_value" != *"utf8"* ]]; then
        return 1
    fi
    if [[ "$stream" == "stderr" ]]; then
        [[ -t 2 ]]
        return
    fi
    [[ -t 1 ]]
}

rl_emoji() {
    local name="$1"
    local stream="${2:-stdout}"
    if ! rl_emoji_enabled "$stream"; then
        return
    fi
    case "$name" in
        info) printf '\u2139\uFE0F' ;;
        step) printf '\U0001F527' ;;
        success) printf '\u2705' ;;
        warning) printf '\u26A0\uFE0F' ;;
        error) printf '\u274C' ;;
        hash) printf '\U0001F510' ;;
    esac
}

rl_status() {
    local color="$1"
    local icon_name="$2"
    local label="$3"
    local message="$4"
    local stream="${5:-stdout}"
    local icon
    icon="$(rl_emoji "$icon_name" "$stream")"
    local prefix="$label"
    if [[ -n "$icon" ]]; then
        prefix="$icon $label"
    fi
    local formatted
    formatted="${color}${prefix}${rl_terminal_reset}: ${message}"
    if [[ "$stream" == "stderr" ]]; then
        printf '%s\n' "$formatted" >&2
    else
        printf '%s\n' "$formatted"
    fi
}

rl_info() {
    rl_status "$rl_terminal_cyan" info "INFO" "$*"
}

rl_step() {
    rl_status "$rl_terminal_cyan" step "STEP" "$*"
}

rl_success() {
    rl_status "$rl_terminal_green" success "OK" "$*"
}

rl_warn() {
    rl_status "$rl_terminal_yellow" warning "WARNING" "$*" stderr
}

rl_error() {
    rl_status "$rl_terminal_red" error "ERROR" "$*" stderr
}

rl_hash() {
    rl_status "$rl_terminal_green" hash "SHA-256" "$*"
}

rl_terminal_init

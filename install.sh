#!/usr/bin/env sh
set -eu

# ─── config ────────────────────────────────────────────────────────
APP_NAME="shmorby"
BIN_NAME="shmorby"
CFG_NAME="config.yaml"
GO_MIN_MAJ=1
GO_MIN_MIN=26

# Paths (may be overridden by flags)
PREFIX="${HOME}/.local"
BIN_DIR_OVERRIDE=""
DATA_DIR_OVERRIDE=""
DRY_RUN=0
DEPS_ONLY=0
DO_UNINSTALL=0
QUIET=0
PREFIX_EXPLICIT=0

# ─── helpers ───────────────────────────────────────────────────────
die()  { printf "[shmorby] \033[1;31m✖ %s\033[0m\n" "$1" >&2; exit 1; }
ok()   { [ "$QUIET" -eq 1 ] || printf "[shmorby] \033[1;32m✔ %s\033[0m\n" "$1"; }
info() { [ "$QUIET" -eq 1 ] || printf "[shmorby] \033[1;34mℹ %s\033[0m\n" "$1"; }
msg()  { [ "$QUIET" -eq 1 ] || printf "[shmorby] %s\n" "$1"; }
warn() { printf "[shmorby] \033[1;33m! %s\033[0m\n" "$1"; }

dry() {
    if [ "$DRY_RUN" -eq 1 ]; then
        info "[DRY-RUN] $1"
    fi
}

need_cmd() {
    command -v "$1" >/dev/null 2>&1
}

# ─── detect ─────────────────────────────────────────────────────────
detect_os() {
    _os="$(uname -s)"
    case "$_os" in
        Linux)  _os="linux" ;;
        Darwin) _os="darwin" ;;
        *)      die "unsupported OS: $_os" ;;
    esac
    dry "OS: $_os"
}

detect_distro() {
    if need_cmd brew; then
        _distro="brew"
    elif need_cmd apt; then
        _distro="apt"
    elif need_cmd dnf; then
        _distro="dnf"
    else
        _distro="none"
    fi
    dry "Distro: $_distro"
}

# ─── prereqs ───────────────────────────────────────────────────────
check_go() {
    if ! need_cmd go; then
        warn "Go not found"
        msg "  Install Go ${GO_MIN_MAJ}.${GO_MIN_MIN}+ from https://go.dev/dl/"
        return 1
    fi
    _ver="$(go version | sed 's/.*go//; s/ .*//')"
    _maj="${_ver%%.*}"
    _min="${_ver#*.}"; _min="${_min%%.*}"
    if [ "$_maj" -lt "$GO_MIN_MAJ" ] || { [ "$_maj" -eq "$GO_MIN_MAJ" ] && [ "$_min" -lt "$GO_MIN_MIN" ]; }; then
        warn "Go $GO_MIN_MAJ.$GO_MIN_MIN+ required, found $_ver"
        return 1
    fi
    ok "Checking: go → found $_ver"
    return 0
}

check_ollama() {
    if ! need_cmd ollama; then
        warn "Checking: ollama → NOT FOUND"
        if [ "$_distro" = "brew" ]; then
            msg "  Install with:  brew install ollama"
        else
            msg "  Install with:  curl -fsSL https://ollama.ai/install.sh | sh"
        fi
        msg "  Or visit:      https://ollama.ai/download"
        return 1
    fi
    ok "Checking: ollama → found"
    return 0
}

check_git() {
    if ! need_cmd git; then
        warn "Checking: git → NOT FOUND"
        return 1
    fi
    _v="$(git --version | sed 's/git version //')"
    ok "Checking: git → found $_v"
    return 0
}

check_make() {
    if ! need_cmd make; then
        warn "Checking: make → NOT FOUND"
        return 1
    fi
    ok "Checking: make → found"
    return 0
}

check_prereqs() {
    _all_ok=1
    check_go    || _all_ok=0
    check_ollama || _all_ok=0
    check_git   || _all_ok=0
    check_make  || _all_ok=0
    if [ "$_all_ok" -eq 0 ]; then
        warn "Some prerequisites are missing — install them before continuing"
    fi
}

# ─── embedding model ───────────────────────────────────────────────
# nomic-embed-text is required by shmorby's memory subsystem.
pull_embedding_model() {
    if ! need_cmd ollama; then
        return 0  # ollama not installed, skip
    fi
    _model="nomic-embed-text:latest"
    info "Pulling embedding model: $_model"
    if [ "$DRY_RUN" -eq 1 ]; then
        info "[DRY-RUN] ollama pull $_model"
    else
        if ollama pull "$_model" >/dev/null 2>&1; then
            ok "Pulled $_model"
        else
            warn "Could not pull $_model — pull manually after ollama starts"
        fi
    fi
}

# ─── flags ─────────────────────────────────────────────────────────
parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --dry-run)   DRY_RUN=1 ;;
            --deps-only) DEPS_ONLY=1 ;;
            --uninstall) DO_UNINSTALL=1 ;;
            --quiet)     QUIET=1 ;;
            --prefix)    PREFIX="$2"; PREFIX_EXPLICIT=1; shift ;;
            --bin-dir)   BIN_DIR_OVERRIDE="$2"; shift ;;
            --data-dir)  DATA_DIR_OVERRIDE="$2"; shift ;;
            -h|--help)   usage; exit 0 ;;
            *)           die "unknown option: $1" ;;
        esac
        shift
    done
}

usage() {
    cat <<'EOF'
Usage: ./install.sh [OPTIONS]
  --dry-run     Print actions without making changes
  --deps-only   Check and report prerequisites
  --uninstall   Remove shmorby binary and directories
  --quiet       Suppress non-error output
  --prefix DIR  Install prefix (default: ~/.local)
  --bin-dir DIR Binary install directory (default: $PREFIX/bin)
  --data-dir DIR Data directory (default: \$XDG_DATA_HOME/shmorby)
  -h, --help    Show this help
EOF
}

# ─── install ───────────────────────────────────────────────────────
install_app() {
    need_cmd go || die "Go is required to build shmorby"

    _src="$(cd "$(dirname "$0")" && pwd)"
    _dst="${_src}/bin/${BIN_NAME}"

    info "Building: go build -o ${BIN_DIR}/${BIN_NAME} ./cmd/shmorby"
    if [ "$DRY_RUN" -eq 1 ]; then
        info "[DRY-RUN] Build: go build ..."
    else
        (cd "$_src" && go build -o "$_dst" ./cmd/shmorby) || die "go build failed"
        ok "Build complete"
    fi

    # Install binary
    if [ "$DRY_RUN" -eq 1 ]; then
        ok "[DRY-RUN] Installing: ${BIN_DIR}/${BIN_NAME}"
    else
        $SUDO mkdir -p "$BIN_DIR" || die "cannot create $BIN_DIR"
        $SUDO install -m 755 "$_dst" "${BIN_DIR}/${BIN_NAME}" || die "install binary failed"
        ok "Installing: ${BIN_DIR}/${BIN_NAME}"
    fi

    # Create directories
    for d in "$CFG_DIR" "$DATA_DIR" "$STATE_DIR"; do
        if [ "$DRY_RUN" -eq 1 ]; then
            ok "[DRY-RUN] Mkdir: $d"
        else
            $SUDO mkdir -p "$d" || die "cannot create $d"
            ok "Creating: $d"
        fi
    done

    # Write minimal default config
    _cfg_path="${CFG_DIR}/${CFG_NAME}"
    if [ "$DRY_RUN" -eq 1 ]; then
        ok "[DRY-RUN] Write: $_cfg_path"
    elif [ -f "$_cfg_path" ]; then
        info "Config exists: $_cfg_path"
    else
        echo "provider: ollama" | $SUDO tee "$_cfg_path" >/dev/null || die "write config failed"
        $SUDO chmod 600 "$_cfg_path" || die "chmod config failed"
        ok "Writing: $_cfg_path"
    fi
}

# ─── uninstall ─────────────────────────────────────────────────────
uninstall() {
    _removed=0

    if [ -f "${BIN_DIR}/${BIN_NAME}" ]; then
        if [ "$DRY_RUN" -eq 1 ]; then
            ok "[DRY-RUN] Removed ${BIN_DIR}/${BIN_NAME}"
        else
            $SUDO rm -f "${BIN_DIR}/${BIN_NAME}" || die "remove binary failed"
            ok "Removed ${BIN_DIR}/${BIN_NAME}"
        fi
        _removed=$((_removed + 1))
    fi

    for d in "$CFG_DIR" "$DATA_DIR" "$STATE_DIR"; do
        if [ -d "$d" ]; then
            if [ "$DRY_RUN" -eq 1 ]; then
                ok "[DRY-RUN] Removed $d"
            else
                $SUDO rm -rf "$d" || die "remove $d failed"
                ok "Removed $d"
            fi
            _removed=$((_removed + 1))
        fi
    done

    if [ "$_removed" -eq 0 ]; then
        info "Nothing to uninstall"
    fi
}

# ─── main ──────────────────────────────────────────────────────────
main() {
    parse_args "$@"

    # Resolve paths
    # BIN_DIR: --bin-dir wins, --prefix controls fallback, XDG_BIN_HOME is default
    _xdg_bin="${XDG_BIN_HOME:-$(command -v brew >/dev/null 2>&1 && brew --prefix 2>/dev/null || echo "$HOME/.local")/bin}"
    BIN_DIR="${BIN_DIR_OVERRIDE:-${_xdg_bin}}"
    # If --prefix was explicitly set (not the default ~/.local), use it for BIN_DIR
    if [ "$PREFIX_EXPLICIT" -eq 1 ] && [ -z "$BIN_DIR_OVERRIDE" ]; then
        BIN_DIR="${PREFIX}/bin"
    fi
    CFG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/${APP_NAME}"
    DATA_DIR="${DATA_DIR_OVERRIDE:-${XDG_DATA_HOME:-$HOME/.local/share}/${APP_NAME}}"
    STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/${APP_NAME}"

    detect_os
    detect_distro

    # sudo only for system-wide installs (spec §93)
    _need_sudo=0
    case "${BIN_DIR}" in /usr/*|/opt/*|/etc/*) _need_sudo=1 ;; esac
    case "${CFG_DIR}" in /usr/*|/opt/*|/etc/*) _need_sudo=1 ;; esac
    SUDO=""
    if [ "$_need_sudo" -eq 1 ] && [ "$(id -u)" -ne 0 ]; then
        if command -v sudo >/dev/null 2>&1; then
            SUDO="sudo"
        fi
    fi

    if [ "$DO_UNINSTALL" -eq 1 ]; then
        uninstall
        exit 0
    fi

    if [ "$DEPS_ONLY" -eq 1 ]; then
        check_prereqs
        exit 0
    fi

    check_prereqs
    install_app
    pull_embedding_model

    msg ""
    ok "shmorby installed to ${BIN_DIR}/${BIN_NAME}"
    msg ""
    msg "  Add to PATH:  export PATH=\"${BIN_DIR}:\$PATH\""
    msg "  Run:          shmorby"
    msg "  Config:       ${CFG_DIR}/${CFG_NAME}"
}

main "$@"

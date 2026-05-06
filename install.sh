#!/bin/sh
# serverpilot-mcp installer for macOS and Linux.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/februality/serverpilot-mcp/main/install.sh | sh
#
# Flags (set as env vars or pass after `--`):
#   SP_VERSION       — pin a specific release tag (default: latest)
#   SP_INSTALL_DIR   — override install directory (default: /usr/local/bin)
#   SP_NO_WIZARD=1   — skip the post-install setup wizard
#   --unattended     — same as SP_NO_WIZARD=1
#   --yes            — skip confirmation prompts (e.g. existing-install upgrade)
#   --force          — install even if the same version is already present
#   CI=1             — auto-skips wizard (already true in most CI runners)

set -eu

REPO="februality/serverpilot-mcp"
BIN_NAME="serverpilot-mcp"
INSTALL_DIR="${SP_INSTALL_DIR:-/usr/local/bin}"
WANTED_VERSION="${SP_VERSION:-}"
ASSUME_YES=0
FORCE=0
SKIP_WIZARD=0

# Parse positional flags after `sh -s --`.
for arg in "$@"; do
    case "$arg" in
    --unattended) SKIP_WIZARD=1 ;;
    --yes) ASSUME_YES=1 ;;
    --force) FORCE=1 ;;
    *) echo "Unknown flag: $arg" >&2; exit 64 ;;
    esac
done

[ -n "${SP_NO_WIZARD:-}" ] && SKIP_WIZARD=1
[ -n "${CI:-}" ] && SKIP_WIZARD=1

err() { printf 'error: %s\n' "$*" >&2; exit 1; }
info() { printf '==> %s\n' "$*"; }

need_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        err "missing required command: $1"
    fi
}
need_cmd uname
need_cmd tar

# Pick a downloader.
if command -v curl >/dev/null 2>&1; then
    download() { curl -fsSL "$1" -o "$2"; }
    fetch_text() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
    download() { wget -qO "$2" "$1"; }
    fetch_text() { wget -qO- "$1"; }
else
    err "need curl or wget to download the binary"
fi

# Detect OS / arch and normalize to GoReleaser archive naming.
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    darwin|linux) ;;
    *) err "unsupported OS: $OS (this script handles macOS and Linux; use install.ps1 on Windows)" ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)  ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) err "unsupported architecture: $ARCH" ;;
esac

# Resolve version.
if [ -z "$WANTED_VERSION" ]; then
    info "Resolving latest release..."
    LATEST_JSON=$(fetch_text "https://api.github.com/repos/${REPO}/releases/latest") \
        || err "could not query GitHub API. Set SP_VERSION to a release tag explicitly."
    WANTED_VERSION=$(printf '%s' "$LATEST_JSON" | grep '"tag_name"' | head -1 | cut -d'"' -f4)
    [ -z "$WANTED_VERSION" ] && err "could not parse latest release tag"
fi
info "Installing $BIN_NAME $WANTED_VERSION for $OS/$ARCH"

# Skip if up-to-date.
if [ "$FORCE" -eq 0 ] && command -v "$BIN_NAME" >/dev/null 2>&1; then
    CURRENT=$("$BIN_NAME" version --short 2>/dev/null || echo "")
    WANTED_NUM=${WANTED_VERSION#v}
    if [ "$CURRENT" = "$WANTED_NUM" ] || [ "$CURRENT" = "$WANTED_VERSION" ]; then
        info "$BIN_NAME $CURRENT is already installed. Use --force to reinstall."
        exit 0
    fi
fi

# Download archive + checksums.
TMPDIR=$(mktemp -d 2>/dev/null || mktemp -d -t spmcp)
trap 'rm -rf "$TMPDIR"' EXIT
ARCHIVE="${BIN_NAME}_${OS}_${ARCH}.tar.gz"
URL_BASE="https://github.com/${REPO}/releases/download/${WANTED_VERSION}"
info "Downloading $ARCHIVE..."
download "${URL_BASE}/${ARCHIVE}"     "${TMPDIR}/${ARCHIVE}" \
    || err "download failed: ${URL_BASE}/${ARCHIVE}"
download "${URL_BASE}/checksums.txt"  "${TMPDIR}/checksums.txt" \
    || err "checksum file download failed"

# Verify SHA-256.
info "Verifying checksum..."
if command -v shasum >/dev/null 2>&1; then
    EXPECTED=$(grep " $ARCHIVE\$" "${TMPDIR}/checksums.txt" | awk '{print $1}')
    ACTUAL=$(shasum -a 256 "${TMPDIR}/${ARCHIVE}" | awk '{print $1}')
elif command -v sha256sum >/dev/null 2>&1; then
    EXPECTED=$(grep " $ARCHIVE\$" "${TMPDIR}/checksums.txt" | awk '{print $1}')
    ACTUAL=$(sha256sum "${TMPDIR}/${ARCHIVE}" | awk '{print $1}')
else
    err "need shasum or sha256sum for verification"
fi
[ -z "$EXPECTED" ] && err "no checksum entry for $ARCHIVE in checksums.txt"
[ "$EXPECTED" = "$ACTUAL" ] || err "checksum mismatch (expected $EXPECTED, got $ACTUAL)"

# Extract.
tar -xzf "${TMPDIR}/${ARCHIVE}" -C "$TMPDIR"
[ -f "${TMPDIR}/${BIN_NAME}" ] || err "binary $BIN_NAME not found in archive"
chmod +x "${TMPDIR}/${BIN_NAME}"

# Install. Try INSTALL_DIR first; fall back to ~/.local/bin.
install_to() {
    target_dir="$1"
    target_path="${target_dir}/${BIN_NAME}"
    mkdir -p "$target_dir" 2>/dev/null || return 1
    if mv "${TMPDIR}/${BIN_NAME}" "$target_path" 2>/dev/null; then
        return 0
    fi
    if command -v sudo >/dev/null 2>&1 && [ -t 0 ]; then
        info "Need sudo to write $target_path"
        sudo mv "${TMPDIR}/${BIN_NAME}" "$target_path" || return 1
        return 0
    fi
    return 1
}

if install_to "$INSTALL_DIR"; then
    INSTALLED_PATH="${INSTALL_DIR}/${BIN_NAME}"
elif install_to "${HOME}/.local/bin"; then
    INSTALLED_PATH="${HOME}/.local/bin/${BIN_NAME}"
    INSTALL_DIR="${HOME}/.local/bin"
    case ":${PATH}:" in
        *:"$INSTALL_DIR":*) : ;;
        *) printf '\nWARNING: %s is not on your PATH.\n  Add this to your shell profile:\n    export PATH="%s:$PATH"\n\n' \
            "$INSTALL_DIR" "$INSTALL_DIR" ;;
    esac
else
    err "could not install to $INSTALL_DIR or $HOME/.local/bin (try setting SP_INSTALL_DIR)"
fi

info "Installed: $INSTALLED_PATH"

# Reattach the terminal and launch the wizard. `curl ... | sh` pipes stdin
# from curl, so we have to reopen /dev/tty to read keystrokes.
if [ "$SKIP_WIZARD" -eq 0 ] && [ -e /dev/tty ]; then
    info "Launching setup wizard..."
    exec "$INSTALLED_PATH" setup </dev/tty
else
    cat <<EOF

Skipping interactive setup. Finish configuring with:

    $INSTALLED_PATH setup

Or set credentials manually with:

    export SERVERPILOT_CLIENT_ID=...
    export SERVERPILOT_API_KEY=...

EOF
fi

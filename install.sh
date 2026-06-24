#!/usr/bin/env bash

# Unified Installer for RunPod CLI Tool
#
# This script provides a unified approach to installing the RunPod CLI tool.
#
# Usage:
#   wget -qO- cli.runpod.net | bash
#
# Requirements:
#   - Bash shell
#   - Internet connection
#   - tar, wget, grep, sed (standard on most systems)
#
# Supported Platforms:
#   - Linux (amd64, arm64)
#   - macOS (Universal binary)

set -e

# ---------------------------- Environment Setup ----------------------------- #
detect_install_dir() {
    if [ "$EUID" -eq 0 ]; then
        INSTALL_DIR="/usr/local/bin"
    else
        # Tiered Path Discovery: Prefer directories already in PATH
        local preferred_dirs="$HOME/.local/bin $HOME/bin $HOME/.bin"
        INSTALL_DIR=""

        for dir in $preferred_dirs; do
            if [[ ":$PATH:" == *":$dir:"* ]] && ([ -d "$dir" ] && [ -w "$dir" ]); then
                INSTALL_DIR="$dir"
                break
            fi
        done

        # If none found in PATH, check if they exist and are writable
        if [ -z "$INSTALL_DIR" ]; then
            for dir in $preferred_dirs; do
                if [ -d "$dir" ] && [ -w "$dir" ]; then
                    INSTALL_DIR="$dir"
                    break
                fi
            done
        fi

        # Fallback to creating ~/.local/bin
        if [ -z "$INSTALL_DIR" ]; then
            INSTALL_DIR="$HOME/.local/bin"
            mkdir -p "$INSTALL_DIR"
        fi

        # High-visibility warning box
        local width
        width=$(tput cols 2>/dev/null || echo 80)
        [ "$width" -gt 80 ] && width=80
        local inner_width=$((width - 4))
        
        local line=""
        local i=0
        while [ $i -lt "$inner_width" ]; do
            line="${line}━"
            i=$((i + 1))
        done

        echo "┏━${line}━┓"
        printf "┃ %-${inner_width}s ┃\n" "USER-SPACE INSTALLATION DETECTED"
        echo "┣━${line}━┫"
        printf "┃ %-${inner_width}s ┃\n" "Target: $INSTALL_DIR"
        printf "┃ %-${inner_width}s ┃\n" ""
        printf "┃ %-${inner_width}s ┃\n" "To install for ALL USERS (requires root), please run:"
        printf "┃ %-${inner_width}s ┃\n" "sudo bash <(wget -qO- cli.runpod.io) # or curl -sL"
        echo "┗━${line}━┛"

        # Check if INSTALL_DIR is in PATH
        if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
            echo "Warning: $INSTALL_DIR is not in your PATH."
            echo "Add it to your profile (e.g., ~/.bashrc or ~/.zshrc):"
            echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
        fi
    fi
}

# -------------------------------- Check Root -------------------------------- #
check_root() {
    if [ "$EUID" -ne 0 ]; then
        echo "Note: Running as non-root. Installing to user-space."
    fi
}

# ------------------------------ Brew Installer ------------------------------ #
install_with_brew() {
    local package=$1
    echo "Installing $package with Homebrew..."
    local original_user
    original_user=$(logname 2>/dev/null || echo "$SUDO_USER")
    
    if [[ -n "$original_user" && "$original_user" != "root" ]]; then
        su - "$original_user" -c "brew install \"$package\""
    else
        brew install "$package"
    fi
}

# ------------------------- Install Required Packages ------------------------ #
check_system_requirements() {
    local missing_pkgs=""
    # Essential tools for downloading and extracting
    for cmd in wget tar grep sed; do
        if ! command -v "$cmd" >/dev/null 2>&1; then
            missing_pkgs="$missing_pkgs $cmd"
        fi
    done

    if [ -n "$missing_pkgs" ]; then
        echo "Error: Missing required commands: $missing_pkgs"
        exit 1
    fi
}

# ----------------------------- runpodctl Version ---------------------------- #
fetch_latest_version() {
    local version_url="https://api.github.com/repos/runpod/runpodctl/releases/latest"
    # Using grep/sed instead of jq for zero-dependency parsing
    # - Robust extraction that doesn't depend on indentation or whitespace
    VERSION=$(wget -q -O- "$version_url" | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')
    
    # Ensure we got a plausible semantic version tag (e.g., v1.2.3)
    case "$VERSION" in
        v[0-9]*) ;; # Valid format
        *)
            echo "Failed to fetch a valid latest version of runpodctl (got: '${VERSION:-<empty>}')."
            exit 1
            ;;
    esac

    echo "Latest version of runpodctl: $VERSION"
}

# ------------------------------- Download URL ------------------------------- #
download_url_constructor() {
    local os_type=$(uname -s | tr '[:upper:]' '[:lower:]')
    local arch_type=$(uname -m)

    if [[ "$os_type" == "darwin" ]]; then
        # macOS uses a universal binary (all architectures)
        DOWNLOAD_URLS=("https://github.com/runpod/runpodctl/releases/download/${VERSION}/runpodctl-darwin-all.tar.gz")
    elif [[ "$os_type" == "linux" ]]; then
        if [[ "$arch_type" == "x86_64" ]]; then
            arch_type="amd64"
        elif [[ "$arch_type" == "aarch64" || "$arch_type" == "arm64" ]]; then
            arch_type="arm64"
        else
            echo "Unsupported Linux architecture: $arch_type"
            exit 1
        fi
        
        # URL 1: Clean name (PR #235) - runpodctl-linux-amd64.tar.gz
        DOWNLOAD_URLS=("https://github.com/runpod/runpodctl/releases/download/${VERSION}/runpodctl-${os_type}-${arch_type}.tar.gz")
    else
        echo "Unsupported operating system: $os_type"
        exit 1
    fi
}

# ----------------------------- Homebrew Support ----------------------------- #
try_brew_install() {
    if [[ "$(uname -s)" != "Darwin" ]]; then
        return 1
    fi

    if ! command -v brew >/dev/null 2>&1; then
        echo "Homebrew not detected. Falling back to binary installation..."
        return 1
    fi

    echo "macOS detected. Attempting to install runpodctl via Homebrew..."
    if install_with_brew "runpod/runpodctl/runpodctl"; then
        echo "runpodctl installed successfully via Homebrew."
        exit 0
    fi

    echo "Homebrew installation failed or was skipped. Falling back to binary..."
    return 1
}

# ---------------------------- Download & Install ---------------------------- #
download_and_install_cli() {
    # Define a unique name for the downloaded archive within our sandbox.
    local cli_archive_file_name="runpodctl.tar.gz"
    local success=false

    # Create an isolated temporary directory for downloading and extracting the binary.
    # Attempts to use 'mktemp' for a secure, unique path; falls back to a PID-based
    # path in /tmp if 'mktemp' is unavailable.
    local tmp_dir
    if command -v mktemp >/dev/null 2>&1; then
        # Handle variations between GNU and BSD (macOS) mktemp
        tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t 'runpodctl-XXXXXX')
    else
        tmp_dir="/tmp/runpodctl-install-$$"
        mkdir -p "$tmp_dir"
    fi
    
    # Register an EXIT trap to ensure the temporary directory is nuked regardless of script outcome.
    trap 'rm -rf "$tmp_dir"' EXIT

    # Determine if wget supports --show-progress (introduced in wget 1.16+)
    local wget_progress_flag=""
    if wget --help | grep -q 'show-progress'; then
        # Use -q (quiet) + --show-progress + bar for a clean, non-spammy progress bar.
        wget_progress_flag="-q --show-progress --progress=bar:force:noscroll"
    fi

    for url in "${DOWNLOAD_URLS[@]}"; do
        echo "Attempting to download runpodctl from $url ..."
        if wget $wget_progress_flag "$url" -O "$tmp_dir/$cli_archive_file_name"; then
            success=true
            break
        fi
    done

    if [ "$success" = false ]; then
        echo "Failed to download runpodctl from any provided URLs."
        exit 1
    fi

    local cli_file_name="runpodctl"
    # Extract to the hermetic sandbox
    tar -C "$tmp_dir" -xzf "$tmp_dir/$cli_archive_file_name" "$cli_file_name" || { echo "Failed to extract $cli_file_name."; exit 1; }
    chmod +x "$tmp_dir/$cli_file_name"

    # Relocate to the final destination using -f (force) to bypass any host-level aliases
    # that might cause the script to hang waiting for user input.
    if ! mv -f "$tmp_dir/$cli_file_name" "$INSTALL_DIR/"; then
        echo "Failed to move $cli_file_name to $INSTALL_DIR/."
        exit 1
    fi
    echo "runpodctl installed successfully to $INSTALL_DIR."
}

# ---------------------------------------------------------------------------- #
#                                     Main                                     #
# ---------------------------------------------------------------------------- #
echo "Installing runpodctl..."

# 1. Prioritize Homebrew on macOS
if try_brew_install; then
    exit 0
fi

# 2. Resilient Binary Installation (Universal Fallback)
detect_install_dir
check_root
check_system_requirements
fetch_latest_version
download_url_constructor
download_and_install_cli

#EOF install.sh

#!/usr/bin/env bash

# Unified Installer for RunPod CLI Tool
#
# This script provides a unified approach to installing the RunPod CLI tool.
#
# Usage:
#   wget -qO- cli.runpod.io | bash
#
# Requirements:
#   - Bash shell
#   - Internet connection
#   - Homebrew (for macOS users)
#   - jq (for JSON processing, will be installed automatically)
#
# Supported Platforms:
#   - Linux (amd64)
#   - macOS (Intel and Apple Silicon)

set -e
REQUIRED_PKGS=("jq")  # Add all required packages to this list, separated by spaces.


# -------------------------------- Check Root -------------------------------- #
check_root() {
    if [ "$EUID" -ne 0 ]; then
        echo "Please run as root with sudo."
        exit 1
    fi
}

# ------------------------------ Brew Installer ------------------------------ #
install_with_brew() {
    local package=$1
    echo "Installing $package with Homebrew..."
    local original_user=$(logname)
    su - "$original_user" -c "brew install $package"
}

# ------------------------- Install Required Packages ------------------------ #
install_package() {
    local package=$1
    echo "Installing $package..."

    case $OSTYPE in
        linux-gnu*)
            if [[ -f /etc/debian_version ]]; then
                apt-get update && apt-get install -y "$package"
            elif [[ -f /etc/redhat-release ]]; then
                yum install -y "$package"
            elif [[ -f /etc/fedora-release ]]; then
                dnf install -y "$package"
            else
                echo "Unsupported Linux distribution for automatic installation of $package."
                exit 1
            fi
            ;;
        darwin*)
            install_with_brew "$package"
            ;;
        *)
            echo "Unsupported OS for automatic installation of $package."
            exit 1
            ;;
    esac
}

check_system_requirements() {
    local all_installed=true

    for pkg in "${REQUIRED_PKGS[@]}"; do
        if ! command -v "$pkg" >/dev/null 2>&1; then
            echo "$pkg is not installed."
            install_package "$pkg"
            all_installed=false
        fi
    done

    if [ "$all_installed" = true ]; then
        echo "All system requirements satisfied."
    fi
}

# ---------------------------- RunPod CLI Version ---------------------------- #
fetch_latest_version() {
    local version_url="https://api.github.com/repos/runpod/runpodctl/releases/latest"
    VERSION=$(wget -q -O- "$version_url" | jq -r '.tag_name')
    if [ -z "$VERSION" ]; then
        echo "Failed to fetch the latest version of RunPod CLI."
        exit 1
    fi
    echo "Latest version of RunPod CLI: $VERSION"
}

# ------------------------------- Download URL ------------------------------- #
download_url_constructor() {
    local os_type=$(uname -s | tr '[:upper:]' '[:lower:]')
    local arch_type=$(uname -m)

    if [[ "$os_type" == "darwin" ]]; then
        if [[ "$arch_type" == "x86_64" ]]; then
            arch_type="amd64"  # For Intel-based Mac
        elif [[ "$arch_type" == "arm64" ]]; then
            arch_type="arm64"  # For ARM-based Mac (Apple Silicon)
        else
            echo "Unsupported macOS architecture: $arch_type"
            exit 1
        fi
    elif [[ "$os_type" == "linux" ]]; then
        arch_type="amd64"  # Assuming amd64 architecture for Linux
    else
        echo "Unsupported operating system: $os_type"
        exit 1
    fi

    DOWNLOAD_URL="https://github.com/runpod/runpodctl/releases/download/${VERSION}/runpod-${os_type}-${arch_type}"
}

# ---------------------------- Download & Install ---------------------------- #
download_and_install_cli() {
    local cli_file_name="runpod"
    if ! wget -q --show-progress "$DOWNLOAD_URL" -O "$cli_file_name"; then
        echo "Failed to download $cli_file_name."
        exit 1
    fi
    chmod +x "$cli_file_name"
    if ! mv "$cli_file_name" /usr/local/bin/; then
        echo "Failed to move $cli_file_name to /usr/local/bin/."
        exit 1
    fi
    echo "RunPod CLI installed successfully."
}


# ---------------------------------------------------------------------------- #
#                                     Main                                     #
# ---------------------------------------------------------------------------- #
echo "Installing RunPod CLI..."

check_root
check_system_requirements
fetch_latest_version
download_url_constructor
download_and_install_cli

#!/usr/bin/env bash

# Unified installer for RunPod CLI tool.
# wget -qO- cli.runpod.io | sudo bash

check_root() {
    if [ "$EUID" -ne 0 ]; then
        echo "Please run as root with sudo."
        exit 1
    fi
}

REQUIRED_PKGS=("jq")  # Add all required packages to this list

# Function to install a package based on OS
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
            brew install "$package"
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

fetch_latest_version() {
    local version_url="https://api.github.com/repos/runpod/runpodctl/releases/latest"
    VERSION=$(wget -q -O- "$version_url" | jq -r '.tag_name')
    echo "Latest version of RunPod CLI: $VERSION"
    if [ -z "$VERSION" ]; then
        echo "Failed to fetch the latest version of RunPod CLI."
        exit 1
    fi
}

set_download_url() {
    case $OSTYPE in
        linux-gnu*)
            DOWNLOAD_URL="https://github.com/runpod/runpodctl/releases/download/${VERSION}/runpodctl-linux-amd"
            ;;
        darwin*)
            DOWNLOAD_URL="https://github.com/runpod/runpodctl/releases/download/${VERSION}/runpodctl-darwin-arm"
            ;;
        cygwin*|msys*|win32*)
            echo "Windows OS detected. Exiting as manual installation is required."
            exit 1
            ;;
        *)
            echo "Unknown OS detected, exiting..."
            exit 1
            ;;
    esac
}

download_and_install_cli() {
    local cli_file_name="runpodctl"
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

# Main execution flow

echo "Installing RunPod CLI..."

check_root
check_system_requirements
fetch_latest_version
set_download_url
download_and_install_cli

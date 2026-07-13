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
#   - Homebrew (for macOS users)
#   - jq (for JSON processing, will be installed automatically)
#   - sha256sum on Linux or shasum on macOS (usually preinstalled)
#
# Supported Platforms:
#   - Linux (amd64)
#   - macOS (Intel and Apple Silicon)

set -e
REQUIRED_PKGS=("jq")  # Add all required packages to this list, separated by spaces.

VERSION=""
DOWNLOAD_URL=""
ARCHIVE_FILENAME=""


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

# ----------------------------- runpodctl Version ---------------------------- #
fetch_latest_version() {
    local version_url="https://api.github.com/repos/runpod/runpodctl/releases/latest"
    VERSION=$(wget -q -O- "$version_url" | jq -r '.tag_name')
    if [ -z "$VERSION" ]; then
        echo "Failed to fetch the latest version of runpodctl."
        exit 1
    fi
    echo "Latest version of runpodctl: $VERSION"
}

# ------------------------------- Download URL ------------------------------- #
download_url_constructor() {
    local os_type=$(uname -s | tr '[:upper:]' '[:lower:]')
    local arch_type=$(uname -m)

    if [[ "$os_type" == "darwin" ]]; then
        # macOS uses a universal binary (all architectures)
        ARCHIVE_FILENAME="runpodctl-darwin-all.tar.gz"
        DOWNLOAD_URL="https://github.com/runpod/runpodctl/releases/download/${VERSION}/${ARCHIVE_FILENAME}"
        return
    elif [[ "$os_type" == "linux" ]]; then
        if [[ "$arch_type" == "x86_64" ]]; then
            arch_type="amd64"
        elif [[ "$arch_type" == "aarch64" || "$arch_type" == "arm64" ]]; then
            arch_type="arm64"
        else
            echo "Unsupported Linux architecture: $arch_type"
            exit 1
        fi
    else
        echo "Unsupported operating system: $os_type"
        exit 1
    fi

    ARCHIVE_FILENAME="runpodctl-${os_type}-${arch_type}.tar.gz"
    DOWNLOAD_URL="https://github.com/runpod/runpodctl/releases/download/${VERSION}/${ARCHIVE_FILENAME}"
}

# ------------------------------- Checksum Helpers ---------------------------- #
checksum_file_name() {
    echo "checksums_${VERSION#v}_sha256.txt"
}

calculate_sha256() {
    local file_path=$1

    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$file_path" | awk '{print $1}'
        return
    fi

    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$file_path" | awk '{print $1}'
        return
    fi

    echo "No SHA-256 checksum tool found. Install sha256sum or shasum." >&2
    return 1
}

checksum_for_archive() {
    local checksum_path=$1
    awk -v archive="$ARCHIVE_FILENAME" '$2 == archive { print $1; found=1; exit } END { if (!found) exit 1 }' "$checksum_path"
}

verify_download_checksum() {
    local archive_path=$1
    local checksum_path=$2
    local expected
    local actual

    if ! expected=$(checksum_for_archive "$checksum_path"); then
        echo "Checksum not found for $ARCHIVE_FILENAME."
        return 1
    fi

    actual=$(calculate_sha256 "$archive_path")
    expected=$(echo "$expected" | tr '[:upper:]' '[:lower:]')
    actual=$(echo "$actual" | tr '[:upper:]' '[:lower:]')

    if [[ "$actual" != "$expected" ]]; then
        echo "Checksum mismatch for $ARCHIVE_FILENAME."
        return 1
    fi

    echo "Checksum verified for $ARCHIVE_FILENAME."
}

# ---------------------------- Download & Install ---------------------------- #
download_and_install_cli() {
    local cli_archive_file_name="$ARCHIVE_FILENAME"
    local checksum_file
    local download_dir
    checksum_file=$(checksum_file_name)
    download_dir=$(mktemp -d)

    trap 'rm -rf "$download_dir"; trap - RETURN' RETURN

    local archive_path="$download_dir/$cli_archive_file_name"
    if ! wget -q --progress=bar "$DOWNLOAD_URL" -O "$archive_path"; then
        echo "Failed to download $cli_archive_file_name."
        return 1
    fi

    local checksum_path="$download_dir/$checksum_file"
    local checksum_url="https://github.com/runpod/runpodctl/releases/download/${VERSION}/${checksum_file}"
    if ! wget -q --progress=bar "$checksum_url" -O "$checksum_path"; then
        echo "Failed to download $checksum_file."
        return 1
    fi

    if ! verify_download_checksum "$archive_path" "$checksum_path"; then
        return 1
    fi

    local cli_file_name="runpodctl"
    if ! tar -xzf "$archive_path" -C "$download_dir" "$cli_file_name"; then
        echo "Failed to extract $cli_file_name from $cli_archive_file_name."
        return 1
    fi
    if ! chmod +x "$download_dir/$cli_file_name"; then
        echo "Failed to mark $cli_file_name as executable."
        return 1
    fi
    if ! mv "$download_dir/$cli_file_name" /usr/local/bin/; then
        echo "Failed to move $cli_file_name to /usr/local/bin/."
        return 1
    fi
    echo "runpodctl installed successfully."
}


# ---------------------------------------------------------------------------- #
#                                     Main                                     #
# ---------------------------------------------------------------------------- #
main() {
    echo "Installing runpodctl..."

    check_root
    check_system_requirements
    fetch_latest_version
    download_url_constructor
    download_and_install_cli
}

if [[ "${RUNPODCTL_INSTALL_TEST:-}" != "1" ]]; then
    main
fi

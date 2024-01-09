#!/usr/bin/env bash

# Unified installer for RunPOd CLI tool.

# Verify the script is run with root privileges
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root with sudo."
    exit 1
fi

# Fetch the latest version of RunPod CLI
VERSION=$(wget -q -O- https://api.github.com/repos/runpod/runpodctl/releases/latest | jq -r '.tag_name')
echo "Fetched version: $VERSION"  # Debugging line
if [ -z "$VERSION" ]; then
    echo "Failed to fetch the latest version of RunPod CLI."
    exit 1
fi

# Detect Operating System and set up environment
OS_TYPE=$(uname -s)
REQUIRED_PKG="jq"
DOWNLOAD_URL=""

setup_environment() {
    case $OS_TYPE in
        Linux*)
            if ! command -v jq >/dev/null 2>&1; then
                echo "No $REQUIRED_PKG. Setting up $REQUIRED_PKG..."
                if [[ -f /etc/debian_version ]]; then
                    apt-get update && apt-get install -y jq
                elif [[ -f /etc/redhat-release ]]; then
                    yum install -y jq
                elif [[ -f /etc/fedora-release ]]; then
                    dnf install -y jq
                else
                    echo "Unsupported Linux distribution for automatic installation."
                    exit 1
                fi
            else
                echo "$REQUIRED_PKG already installed, skipping..."
            fi
            DOWNLOAD_URL="https://github.com/runpod/runpodctl/releases/download/${VERSION}/runpodctl-linux-amd"
            ;;
        Darwin*)
            if ! command -v jq >/dev/null 2>&1; then
                echo "No $REQUIRED_PKG. Setting up $REQUIRED_PKG..."
                brew install jq
            else
                echo "$REQUIRED_PKG already installed, skipping..."
            fi
            DOWNLOAD_URL="https://github.com/runpod/runpodctl/releases/download/${VERSION}/runpodctl-darwin-arm"
            ;;
        CYGWIN*|MINGW32*|MSYS*|MINGW*)
            echo "Please manually install jq for Windows."
            DOWNLOAD_URL="https://github.com/runpod/runpodctl/releases/download/${VERSION}/runpodctl-win-amd"
            exit 1
            ;;
        *)
            echo "Unsupported OS: $OS_TYPE"
            exit 1
            ;;
    esac
}

setup_environment

# Download the CLI tool
CLI_FILE_NAME="runpodctl"
if ! wget "$DOWNLOAD_URL" -O "$CLI_FILE_NAME"; then
    echo "Failed to download $CLI_FILE_NAME."
    exit 1
fi

# Install the CLI tool
chmod +x "$CLI_FILE_NAME"
if ! mv "$CLI_FILE_NAME" /usr/local/bin/; then
    echo "Failed to move $CLI_FILE_NAME to /usr/local/bin/."
    exit 1
fi

echo "RunPod CLI installed successfully."

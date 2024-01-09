#!/usr/bin/env bash

# Unified installer for RunPOd CLI tool.

# -------------------------------- Verify Root ------------------------------- #
if [ "$EUID" -ne 0 ]; then
  echo "Please run as root with sudo."
  exit
fi


# ---------------------------------------------------------------------------- #
#                              System Requirements                             #
# ---------------------------------------------------------------------------- #

# ------------------------------------ jq ------------------------------------ #
REQUIRED_PKG="jq"
if ! dpkg-query -W -f='${Status}' "$REQUIRED_PKG" 2>/dev/null | grep -q "install ok installed"; then
    echo "No $REQUIRED_PKG. Setting up $REQUIRED_PKG..."
    apt-get update
    apt-get install -y "$REQUIRED_PKG"
else
    echo "$REQUIRED_PKG already installed, skipping..."
fi

# ---------------------------------------------------------------------------- #
#                        Grab Latest RunPod CLI Release                        #
# ---------------------------------------------------------------------------- #

# ----------------------------- Latest Release Tag ---------------------------- #
VERSION=$(wget -q -O- https://api.github.com/repos/runpod/runpodctl/releases/latest | jq -r '.name')
if [ -z "$VERSION" ]; then
    echo "Failed to fetch the latest version of RunPod CLI."
    exit
fi

# ---------------------------- OS and Download URL --------------------------- #
case "$OSTYPE" in
    "linux-gnu"*)
        DOWNLOAD_URL="https://github.com/runpod/runpodctl/releases/download/${VERSION}/runpodctl-linux-amd"
        ;;
    "darwin"*)
        DOWNLOAD_URL="https://github.com/runpod/runpodctl/releases/download/${VERSION}/runpodctl-darwin-arm"
        ;;
    "cygwin"|"msys"|"win32")
        DOWNLOAD_URL="https://github.com/runpod/runpodctl/releases/download/${VERSION}/runpodctl-win-amd"
        ;;
    *)
        echo "Unknown OS detected, exiting..."
        exit 1
        ;;
esac

# ----------------------------- Download Package ----------------------------- #
CLI_FILE_NAME="runpodctl"
if ! wget "$DOWNLOAD_URL" -O "$CLI_FILE_NAME"; then
    echo "Failed to download $CLI_FILE_NAME."
    exit 1
fi

# ----------------------------- Install Package ------------------------------ #
chmod +x "$CLI_FILE_NAME"
if ! mv "$CLI_FILE_NAME" /usr/local/bin/; then
    echo "Failed to move $CLI_FILE_NAME to /usr/local/bin/."
    exit 1
fi

echo "RunPod CLI installed successfully."

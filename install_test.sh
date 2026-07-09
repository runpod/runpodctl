#!/usr/bin/env bash
set -euo pipefail

RUNPODCTL_INSTALL_TEST=1 source ./install.sh

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

ARCHIVE_FILENAME="runpodctl-linux-amd64.tar.gz"
ARCHIVE_PATH="$TMPDIR/$ARCHIVE_FILENAME"
CHECKSUM_PATH="$TMPDIR/checksums.txt"

printf 'release archive bytes' > "$ARCHIVE_PATH"
EXPECTED=$(calculate_sha256 "$ARCHIVE_PATH")
printf '%s  %s\n' "$EXPECTED" "$ARCHIVE_FILENAME" > "$CHECKSUM_PATH"

verify_download_checksum "$ARCHIVE_PATH" "$CHECKSUM_PATH"

printf 'tampered archive bytes' > "$ARCHIVE_PATH"
if verify_download_checksum "$ARCHIVE_PATH" "$CHECKSUM_PATH"; then
    echo "expected checksum mismatch to fail"
    exit 1
fi

ARCHIVE_FILENAME="missing.tar.gz"
if verify_download_checksum "$ARCHIVE_PATH" "$CHECKSUM_PATH"; then
    echo "expected missing checksum entry to fail"
    exit 1
fi

#!/usr/bin/env bash
set -euo pipefail

RUNPODCTL_INSTALL_TEST=1 source ./install.sh

TEST_TMPDIR=$(mktemp -d)
trap 'rm -rf "$TEST_TMPDIR"' EXIT

ARCHIVE_FILENAME="runpodctl-linux-amd64.tar.gz"
ARCHIVE_PATH="$TEST_TMPDIR/$ARCHIVE_FILENAME"
CHECKSUM_PATH="$TEST_TMPDIR/checksums.txt"

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

assert_no_installer_artifacts() {
    local work_dir=$1
    local leftovers

    leftovers=$(find "$work_dir" -maxdepth 1 \( \
        -name 'runpodctl-*.tar.gz' -o \
        -name 'checksums_*_sha256.txt' -o \
        -name 'runpodctl' \
    \) -print)

    if [[ -n "$leftovers" ]]; then
        echo "installer left artifacts in $work_dir:"
        echo "$leftovers"
        exit 1
    fi
}

run_download_cleanup_success_test() {
    local work_dir="$TEST_TMPDIR/success-cwd"
    local installed_dir="$TEST_TMPDIR/success-installed"
    mkdir -p "$work_dir" "$installed_dir"

    (
        cd "$work_dir"
        VERSION="v9.9.9"
        ARCHIVE_FILENAME="runpodctl-linux-amd64.tar.gz"
        DOWNLOAD_URL="https://example.test/$ARCHIVE_FILENAME"

        local downloaded_archive_path=""
        wget() {
            local output=""
            while [[ $# -gt 0 ]]; do
                if [[ "$1" == "-O" ]]; then
                    output=$2
                    shift 2
                    continue
                fi
                shift
            done

            case "${output##*/}" in
                "$ARCHIVE_FILENAME")
                    downloaded_archive_path=$output
                    printf 'release archive bytes' > "$output"
                    ;;
                "$(checksum_file_name)")
                    local digest
                    digest=$(calculate_sha256 "$downloaded_archive_path")
                    printf '%s  %s\n' "$digest" "$ARCHIVE_FILENAME" > "$output"
                    ;;
                *)
                    echo "unexpected wget output path: $output" >&2
                    return 1
                    ;;
            esac
        }

        tar() {
            local extract_dir="."
            while [[ $# -gt 0 ]]; do
                if [[ "$1" == "-C" ]]; then
                    extract_dir=$2
                    shift 2
                    continue
                fi
                shift
            done
            printf '#!/usr/bin/env sh\n' > "$extract_dir/runpodctl"
        }

        chmod() {
            :
        }

        mv() {
            command mv "$1" "$installed_dir/"
        }

        download_and_install_cli >/dev/null
    )

    assert_no_installer_artifacts "$work_dir"
    if [[ ! -f "$installed_dir/runpodctl" ]]; then
        echo "expected runpodctl to be installed"
        exit 1
    fi
}

run_download_cleanup_failure_test() {
    local work_dir="$TEST_TMPDIR/failure-cwd"
    mkdir -p "$work_dir"

    set +e
    (
        set -e
        cd "$work_dir"
        VERSION="v9.9.9"
        ARCHIVE_FILENAME="runpodctl-linux-amd64.tar.gz"
        DOWNLOAD_URL="https://example.test/$ARCHIVE_FILENAME"

        wget() {
            local output=""
            while [[ $# -gt 0 ]]; do
                if [[ "$1" == "-O" ]]; then
                    output=$2
                    shift 2
                    continue
                fi
                shift
            done

            case "${output##*/}" in
                "$ARCHIVE_FILENAME")
                    printf 'release archive bytes' > "$output"
                    ;;
                "$(checksum_file_name)")
                    printf '%064d  %s\n' 0 "$ARCHIVE_FILENAME" > "$output"
                    ;;
                *)
                    echo "unexpected wget output path: $output" >&2
                    return 1
                    ;;
            esac
        }

        tar() {
            echo "tar should not run after checksum failure" >&2
            return 1
        }

        download_and_install_cli >/dev/null
    ) >/dev/null 2>&1
    local status=$?
    set -e

    if [[ $status -eq 0 ]]; then
        echo "expected checksum failure to abort install"
        exit 1
    fi

    assert_no_installer_artifacts "$work_dir"
}

run_download_cleanup_success_test
run_download_cleanup_failure_test

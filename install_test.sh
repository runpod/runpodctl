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
    local expected_download_dir="$TEST_TMPDIR/success-download"
    mkdir -p "$work_dir" "$installed_dir"

    (
        cd "$work_dir"
        VERSION="v9.9.9"
        ARCHIVE_FILENAME="runpodctl-linux-amd64.tar.gz"
        DOWNLOAD_URL="https://example.test/$ARCHIVE_FILENAME"

        mktemp() {
            mkdir -p "$expected_download_dir"
            printf '%s\n' "$expected_download_dir"
        }

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
    if [[ -e "$expected_download_dir" ]]; then
        echo "installer left download directory $expected_download_dir"
        exit 1
    fi
    if [[ ! -f "$installed_dir/runpodctl" ]]; then
        echo "expected runpodctl to be installed"
        exit 1
    fi
}

run_download_cleanup_calculation_failure_test() {
    local work_dir="$TEST_TMPDIR/calculation-failure-cwd"
    local expected_download_dir="$TEST_TMPDIR/calculation-failure-download"
    local tar_called="$TEST_TMPDIR/calculation-failure-tar-called"
    local command_output_path="$TEST_TMPDIR/calculation-failure-output"
    local archive_filename="runpodctl-linux-amd64.tar.gz"
    local output
    mkdir -p "$work_dir"

    set +e
    (
        set -e
        cd "$work_dir"
        VERSION="v9.9.9"
        ARCHIVE_FILENAME="$archive_filename"
        DOWNLOAD_URL="https://example.test/$ARCHIVE_FILENAME"

        mktemp() {
            mkdir -p "$expected_download_dir"
            printf '%s\n' "$expected_download_dir"
        }

        wget() {
            local output_path=""
            while [[ $# -gt 0 ]]; do
                if [[ "$1" == "-O" ]]; then
                    output_path=$2
                    shift 2
                    continue
                fi
                shift
            done

            case "${output_path##*/}" in
                "$ARCHIVE_FILENAME")
                    printf 'release archive bytes' > "$output_path"
                    ;;
                "checksums_9.9.9_sha256.txt")
                    printf '%064d  %s\n' 0 "$ARCHIVE_FILENAME" > "$output_path"
                    ;;
                *)
                    echo "unexpected wget output path: $output_path" >&2
                    return 1
                    ;;
            esac
        }

        calculate_sha256() {
            return 1
        }

        tar() {
            touch "$tar_called"
            return 1
        }

        download_and_install_cli
    ) > "$command_output_path" 2>&1
    local status=$?
    set -e
    output=$(<"$command_output_path")

    if [[ $status -eq 0 ]]; then
        echo "expected checksum calculation failure to abort install"
        exit 1
    fi
    if [[ "$output" != *"Failed to calculate checksum for $archive_filename."* ]]; then
        echo "expected checksum calculation failure message, got:"
        echo "$output"
        exit 1
    fi

    assert_no_installer_artifacts "$work_dir"
    if [[ -e "$expected_download_dir" ]]; then
        echo "installer left download directory $expected_download_dir"
        exit 1
    fi
    if [[ -e "$tar_called" ]]; then
        echo "tar ran after checksum calculation failure"
        exit 1
    fi
}

run_download_cleanup_success_test
run_download_cleanup_calculation_failure_test

#!/usr/bin/env bash
set -euo pipefail

RUNPODCTL_INSTALL_TEST=1 source ./install.sh

TEST_TMPDIR=$(mktemp -d)
trap 'rm -rf "$TEST_TMPDIR"' EXIT

for failed_download in version archive checksum; do
    marker="$TEST_TMPDIR/$failed_download-installed"
    calls="$TEST_TMPDIR/$failed_download-calls"
    set +e
    (
        set -e
        check_root() { :; }
        check_system_requirements() { :; }
        jq() { printf 'v9.9.9\n'; }
        tar() { touch "$marker"; }
        chmod() { touch "$marker"; }
        mv() { touch "$marker"; }

        curl() {
            if [[ "$1" != "--proto" || "$2" != "=https" ||
                  "$3" != "--proto-redir" || "$4" != "=https" ||
                  "$5" != "-fsSL" || "$6" != https://* ]]; then
                echo "download must require verified https" >&2
                touch "$TEST_TMPDIR/unsafe-download"
                return 1
            fi

            local stage
            case "$6" in
                */releases/latest) stage=version ;;
                *.tar.gz) stage=archive ;;
                *_sha256.txt) stage=checksum ;;
                *) return 1 ;;
            esac
            printf '%s\n' "$stage" >> "$calls"
            if [[ "$stage" == "$failed_download" ]]; then
                return 1
            fi
            if [[ "$stage" == version ]]; then
                printf '{"tag_name":"v9.9.9"}\n'
            else
                [[ "$7" == "-o" ]] || return 1
                printf 'release archive bytes' > "$8"
            fi
        }

        main
    ) > "$TEST_TMPDIR/$failed_download-output" 2>&1
    status=$?
    set -e

    if [[ $status -eq 0 || -e "$marker" || -e "$TEST_TMPDIR/unsafe-download" ]]; then
        cat "$TEST_TMPDIR/$failed_download-output"
        echo "expected $failed_download download failure to abort before installation"
        exit 1
    fi
    if [[ "$(tail -n 1 "$calls")" != "$failed_download" ]]; then
        echo "installer continued after $failed_download download failure"
        exit 1
    fi
done

echo "https download policy and failure handling passed"

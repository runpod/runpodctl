#!/usr/bin/env bash
#
# nix/tests/integration.sh — runpodctl
#
# Exercises every flake target on this system: `nix run .#test-integration`.
# Targets are enumerated from the flake (`nix eval … --apply builtins.attrNames`),
# never listed here, so a new package, check or app is covered without an edit.
#
# Each step prints PASS/FAIL/SKIP with its duration; the full output of each
# is kept under $LOGDIR (default: a temp dir, printed at the end). The run
# ends with a summary table and exits non-zero on any FAIL. A SKIP is never a
# pass: it names what could not run here and why.
#
# Steps that mutate the tree (the negative checks, update-vendor-hash, the
# planted fuzz crasher) work on a throwaway git copy of the working tree, so
# the checkout is never touched.
#
set -euo pipefail

sys="$(nix eval --impure --raw --expr builtins.currentSystem)"
logdir="${LOGDIR:-$(mktemp -d -t runpodctl-integration.XXXXXX)}"
mkdir -p "$logdir"
scratch="$(mktemp -d -t runpodctl-integration-work.XXXXXX)"
trap 'rm -rf "$scratch"' EXIT

results=()
failures=0

# run_step ID DESCRIPTION COMMAND… runs COMMAND with its output in
# $logdir/ID.log. The command runs as a background job and is waited on:
# errexit is ignored inside a function called from an `||` list, but not in
# an asynchronous one, so each step function can rely on `set -e`.
run_step() {
  local id="$1" desc="$2"
  shift 2
  local log="$logdir/$id.log" start rc=0 status
  start="$(date +%s)"
  "$@" > "$log" 2>&1 &
  wait "$!" || rc=$?
  local took
  took="$(($(date +%s) - start))s"
  if [ "$rc" -eq 0 ]; then
    status=PASS
  else
    status=FAIL
    failures=$((failures + 1))
  fi
  results+=("$status|$id|$desc|$took")
  echo "$status $id: $desc ($took)"
  if [ "$rc" -ne 0 ]; then
    tail -n 25 "$log" | sed 's/^/    /'
  fi
}

skip_step() {
  results+=("SKIP|$1|$2: $3|-")
  echo "SKIP $1: $2 ($3)"
}

# attr_names KIND prints the attribute names of KIND.$sys, one per line.
attr_names() {
  nix eval --json ".#$1.$sys" --apply builtins.attrNames | jq -r '.[]'
}

# expect_fail_with PATTERN COMMAND… succeeds only if COMMAND fails and its
# output matches PATTERN, so a step can't pass on an unrelated error.
expect_fail_with() {
  local pattern="$1" log
  shift
  log="$(mktemp)"
  if "$@" > "$log" 2>&1; then
    cat "$log"
    echo "expected a failure, but it succeeded: $*"
    return 1
  fi
  cat "$log"
  if ! grep -q -- "$pattern" "$log"; then
    echo "failed, but not with \"$pattern\": $*"
    return 1
  fi
  echo "failed as expected ($pattern): $*"
}

# make_copy DIR copies the working tree's tracked files (with their working
# tree contents: what lib.fileset.gitTracked gives the flake) into DIR and
# commits it, so a step can mutate it and `git reset --hard` back. untracked
# files stay out: the flake never sees them, so a copy with them would test
# a different tree.
make_copy() {
  local dest="$1" f
  mkdir -p "$dest"
  while IFS= read -r -d '' f; do
    if [ -e "$f" ] || [ -L "$f" ]; then
      cp -a --parents "$f" "$dest"
    fi
  done < <(git ls-files -z --cached)
  git -C "$dest" init -q
  git -C "$dest" add -A
  git -C "$dest" -c user.name=integration -c user.email=integration@localhost commit -qm base
}

reset_copy() {
  git -C "$1" reset -q --hard
  git -C "$1" clean -fdq
}

mapfile -t packages < <(attr_names packages)
mapfile -t checks < <(attr_names checks)
mapfile -t apps < <(attr_names apps)
echo "system $sys: ${#packages[@]} packages, ${#checks[@]} checks, ${#apps[@]} apps"
echo "logs in $logdir"

# ---- 1. flake shape ---------------------------------------------------------
step_flake_shape() {
  nix flake show --json > "$scratch/show.json"
  # determinate nix prints the flake-schemas shape, where each output's
  # systems sit under .inventory.<output>.output.children.
  jq -e --arg sys "$sys" '
    (if has("inventory") then .inventory | map_values(.output.children) else . end) as $o
    | all("packages", "checks", "apps", "devShells"; $o[.][$sys] != null)
  ' "$scratch/show.json"
  nix flake check --no-build --all-systems
}
run_step flake-shape "flake show + eval of every system (incl. the go.mod assert)" step_flake_shape

# ---- 2. packages ------------------------------------------------------------
step_package() {
  nix build --no-link --print-out-paths ".#packages.$sys.$1"
}
# An ungated check (passthru.ungatedReason) is expected to fail: XFAIL is
# not counted, and a pass is a FAIL that says to gate it.
known_failure() {
  nix eval --raw ".#packages.$sys.$1" --apply 'd: d.passthru.ungatedReason or ""'
}
step_known_failure() {
  if nix build --no-link -L ".#packages.$sys.$1"; then
    echo "$1 passes now: move it from ungated to gated in nix/checks/default.nix"
    return 1
  fi
}
for p in "${packages[@]}"; do
  reason="$(known_failure "$p")"
  if [ -z "$reason" ]; then
    run_step "package-$p" "nix build .#$p" step_package "$p"
    continue
  fi
  run_step "package-$p" "known failure ($reason)" step_known_failure "$p"
  if [ "${results[-1]%%|*}" = PASS ]; then
    results[-1]="XFAIL${results[-1]#PASS}"
  fi
done

step_binary() {
  local out bin got rev
  out="$(nix build --no-link --print-out-paths ".#packages.$sys.runpodctl")"
  bin="$out/bin/runpodctl"
  rev="$(nix flake metadata --json | jq -r '(.dirtyRevision // .revision // "unknown")')"
  case "$rev" in
    *-dirty) rev="${rev:0:7}-dirty" ;;
    unknown) ;;
    *) rev="${rev:0:7}" ;;
  esac
  got="$("$bin" --version)"
  echo "$got"
  case "$got" in
    *"-$rev"*) ;;
    *) echo "version does not carry the flake revision $rev"; return 1 ;;
  esac
  file -L "$bin" | tee "$scratch/file.txt"
  if [ "$(uname -s)" = Linux ]; then
    grep -q 'statically linked' "$scratch/file.txt"
    grep -q 'stripped' "$scratch/file.txt"
    if grep -q 'not stripped' "$scratch/file.txt"; then
      echo "binary is not stripped"
      return 1
    fi
  else
    # file(1) doesn't report stripping for Mach-O, and darwin binaries always
    # link libSystem. stripped: no defined symbols, only dyld imports; and
    # every dylib comes from the os, none from /nix/store.
    if [ -n "$(nm -U "$bin")" ]; then
      echo "binary is not stripped"
      return 1
    fi
    otool -L "$bin" | tail -n +2 | tee "$scratch/dylibs.txt"
    if grep -vqE '^[[:space:]]+/(usr/lib|System/Library)/' "$scratch/dylibs.txt"; then
      echo "binary links a dylib from outside the os"
      return 1
    fi
  fi
  # nixpkgs' Go looks up /etc/services, mime types and zoneinfo in these
  # data packages; anything else (a toolchain, a libc) is a leak.
  local extra
  extra="$(nix path-info -r "$out" | grep -v -e "^$out\$" -e '-iana-etc-' -e '-mailcap-' -e '-tzdata-' || true)"
  nix path-info -r "$out"
  if [ -n "$extra" ]; then
    echo "unexpected runtime dependencies:"
    echo "$extra"
    return 1
  fi
}
run_step binary "runpodctl: version stamp, static, stripped, data-only closure" step_binary

# ---- 3. checks --------------------------------------------------------------
step_check() {
  nix build --no-link -L ".#checks.$sys.$1"
}
for c in "${checks[@]}"; do
  run_step "check-$c" "nix build .#checks.$c" step_check "$c"
done

# ---- 4. negative checks -----------------------------------------------------
neg="$scratch/negative"
make_copy "$neg"

step_neg_gofmt() {
  reset_copy "$neg"
  printf 'package duration\nfunc  probe( ) {}\n' > "$neg/internal/duration/probe.go"
  git -C "$neg" add -A
  expect_fail_with "internal/duration/probe.go" nix build --no-link -L "$neg#checks.$sys.gofmt"
}
run_step negative-gofmt "an unformatted file fails gofmt" step_neg_gofmt

step_neg_vet() {
  reset_copy "$neg"
  printf 'package duration\n\nimport "fmt"\n\nfunc probe() { fmt.Printf("%%d\\n", "x") }\n' > "$neg/internal/duration/probe.go"
  git -C "$neg" add -A
  expect_fail_with "format %d has arg" nix build --no-link -L "$neg#checks.$sys.go-vet"
}
run_step negative-vet "a printf mismatch fails go-vet" step_neg_vet

step_neg_lint() {
  reset_copy "$neg"
  printf 'package duration\n\nimport "os"\n\n// Probe drops an error.\nfunc Probe() { os.Remove("x") }\n' > "$neg/internal/duration/probe.go"
  git -C "$neg" add -A
  expect_fail_with "internal/duration/probe.go" nix build --no-link -L "$neg#checks.$sys.lint-baseline"
}
run_step negative-lint "a new finding fails lint-baseline" step_neg_lint

step_neg_gomod() {
  reset_copy "$neg"
  sed -i 's/^go .*/go 1.99.0/' "$neg/go.mod"
  git -C "$neg" add -A
  expect_fail_with "go.mod requires go 1.99.0" nix eval --raw "$neg#packages.$sys.runpodctl.drvPath"
}
run_step negative-gomod "go.mod above the pinned Go fails evaluation" step_neg_gomod

# ---- 5. apps ----------------------------------------------------------------
# Every app's program exists once the dev shell (which carries every command)
# is built; the ones that are safe to run here are then run for real.
step_app_programs() {
  nix build --no-link ".#devShells.$sys.default"
  local a program
  for a in "${apps[@]}"; do
    program="$(nix eval --raw ".#apps.$sys.$a.program")"
    nix build --no-link "$program" 2> /dev/null || true
    if [ ! -x "$program" ]; then
      echo "app $a: $program is not executable"
      return 1
    fi
    echo "ok $a -> $program"
  done
}
run_step app-programs "every app resolves to an executable" step_app_programs

run_step app-default "nix run . -- --version" nix run . -- --version
run_step app-report "nix run .#static-analysis-report" nix run .#static-analysis-report

step_app_vendor_hash() {
  reset_copy "$neg"
  (cd "$neg" && nix run "$neg#update-vendor-hash")
  git -C "$neg" diff --exit-code -- nix/versions.nix
}
run_step app-update-vendor-hash "update-vendor-hash leaves a current hash unchanged" step_app_vendor_hash

step_app_lint_baseline() {
  reset_copy "$neg"
  (cd "$neg" && nix run "$neg#update-lint-baseline")
  git -C "$neg" diff --exit-code -- nix/checks/lint-baseline.txt
}
run_step app-update-lint-baseline "update-lint-baseline on a clean tree gives no diff" step_app_lint_baseline

step_app_guard() {
  expect_fail_with "run this from the runpodctl repo root" bash -c "cd '$scratch' && nix run '$PWD#check-fmt'"
}
run_step app-guard "commands refuse to run outside the repo root" step_app_guard

# ---- 6. dev shell -----------------------------------------------------------
step_dev_shell() {
  local names=()
  local a
  for a in "${apps[@]}"; do
    case "$a" in default | runpodctl) ;; *) names+=("$a") ;; esac
  done
  local go_version
  go_version="$(nix eval --raw ".#packages.$sys.runpodctl.passthru.go.version")"
  cat > "$scratch/dev-shell.sh" <<'EOF'
set -euo pipefail
go_version="$1"
shift
go version
go version | grep -q "go$go_version "
[ "$GOTOOLCHAIN" = local ]
[ "$CGO_ENABLED" = 0 ]
for tool in golangci-lint staticcheck gosec govulncheck nixfmt deadnix statix shellcheck actionlint benchstat; do
  command -v "$tool"
done
for cmd in "$@"; do
  command -v "$cmd" > /dev/null || { echo "missing command $cmd"; exit 1; }
done
runpodctl-help
EOF
  nix develop -c bash "$scratch/dev-shell.sh" "$go_version" "${names[@]}"
}
run_step dev-shell "nix develop: pinned Go, tools, every command, banner" step_dev_shell

# ---- 7. consumer UX ---------------------------------------------------------
step_profile() {
  nix profile install --profile "$scratch/profile" .#runpodctl
  "$scratch/profile/bin/runpodctl" --version
}
run_step consumer-profile "nix profile install into a throwaway profile" step_profile

step_no_git() {
  local plain="$scratch/no-git" out
  cp -a "$neg" "$plain"
  rm -rf "$plain/.git"
  # a tarball has no .git: the filesets fall back to the whole tree and the
  # version carries "unknown" in place of a commit.
  out="$(nix build --no-link --print-out-paths "path:$plain#runpodctl")"
  "$out/bin/runpodctl" --version | tee "$scratch/no-git.txt"
  grep -q -- '-unknown' "$scratch/no-git.txt"
}
run_step consumer-no-git "a tree without .git (a tarball) builds, stamped unknown" step_no_git

# ---- 8. OCI images ----------------------------------------------------------
images=()
for p in "${packages[@]}"; do
  case "$p" in oci-*) images+=("$p") ;; esac
done
engine=""
if docker info > /dev/null 2>&1; then
  engine=docker
elif command -v podman > /dev/null 2>&1; then
  engine=podman
fi

step_oci() {
  local p="$1" stream ref out err uid
  stream="$(nix build --no-link --print-out-paths ".#packages.$sys.$p")"
  ref="$("$stream" | "$engine" load | sed -n 's/^Loaded image[^:]*: //p' | tail -n1)"
  echo "loaded $ref"
  "$engine" run --rm "$ref" --version
  uid="$("$engine" image inspect --format '{{.Config.User}}' "$ref")"
  [ "$uid" = "65532:65532" ]
  # no api key: the error goes to stderr, stdout stays empty (CON-683).
  out="$scratch/$p.stdout" err="$scratch/$p.stderr"
  if "$engine" run --rm -e RUNPOD_API_KEY= "$ref" gpu list > "$out" 2> "$err"; then
    echo "gpu list without a key succeeded"
    return 1
  fi
  cat "$err"
  [ ! -s "$out" ]
  grep -q 'no_credentials\|api key' "$err"
  case "$p" in
    *-full)
      # captured, not piped to head: an early close is a SIGPIPE under pipefail.
      local rsync_version
      rsync_version="$("$engine" run --rm --entrypoint rsync "$ref" --version)"
      echo "${rsync_version%%$'\n'*}"
      "$engine" run --rm --entrypoint ssh "$ref" -V
      # a real connection attempt: ssh gets past its user lookup (it needs
      # the uid in /etc/passwd) and fails only on the closed port.
      expect_fail_with "Connection refused" "$engine" run --rm --entrypoint ssh "$ref" \
        -o BatchMode=yes -o ConnectTimeout=5 -p 1 127.0.0.1 true
      ;;
  esac
  "$engine" rmi "$ref" > /dev/null
}

# The slim image holds the binary, the data packages nixpkgs' Go reads
# (see step_binary) and the CA bundle, and nothing else. The stream script
# logs each layer's store paths, which is the list checked here.
step_oci_size() {
  local stream extra
  nix run .#oci-size-report
  stream="$(nix build --no-link --print-out-paths ".#packages.$sys.oci-runpodctl")"
  "$stream" 2> "$scratch/layers.log" > /dev/null
  grep -o "/nix/store/[^']*" "$scratch/layers.log" | sort -u | tee "$scratch/layers.txt"
  extra="$(grep -v -e '-runpodctl-' -e '-iana-etc-' -e '-mailcap-' -e '-tzdata-' -e '-nss-cacert-' "$scratch/layers.txt" || true)"
  if [ -n "$extra" ]; then
    echo "unexpected paths in the slim image:"
    echo "$extra"
    return 1
  fi
}

if [ "${#images[@]}" -eq 0 ]; then
  skip_step oci "OCI images" "no images on $sys"
elif [ -z "$engine" ]; then
  skip_step oci "OCI images" "neither a docker daemon nor podman is available"
else
  for p in "${images[@]}"; do
    run_step "oci-$p" "$engine: load, run, uid 65532, errors on stderr" step_oci "$p"
  done
  run_step oci-size "size report; slim image is the binary, its data and CA certs only" step_oci_size
fi

# ---- 9. fuzz ----------------------------------------------------------------
run_step fuzz-short "nix run .#fuzz -- --fuzztime 10s (every target)" nix run .#fuzz -- --fuzztime 10s

step_fuzz_crasher() {
  reset_copy "$neg"
  # reintroduce the day-count wrap and drop it from the seeds: only the
  # committed crasher can catch it now.
  sed -i 's/if n > maxDays {/if false {/' "$neg/internal/duration/duration.go"
  grep -q 'if false {' "$neg/internal/duration/duration.go"
  sed -i 's/ "213504d",//' "$neg/internal/duration/duration_fuzz_test.go"
  mkdir -p "$neg/internal/duration/testdata/fuzz/FuzzParse"
  printf 'go test fuzz v1\nstring("213504d")\n' > "$neg/internal/duration/testdata/fuzz/FuzzParse/planted"
  git -C "$neg" add -A
  expect_fail_with "FuzzParse/planted" nix build --no-link -L "$neg#checks.$sys.fuzz-seeds"
}
run_step fuzz-crasher "a committed crasher fails fuzz-seeds" step_fuzz_crasher

# ---- 10. static analysis report ---------------------------------------------
step_report() {
  local out measure tool code
  out="$(nix build --no-link --print-out-paths ".#packages.$sys.static-analysis-report")"
  measure="$(nix build --no-link --print-out-paths ".#packages.$sys.static-analysis-measure")"
  test -s "$out/report.md"
  jq -e '.tools | length > 0' "$out/report.json"
  for f in "$measure"/raw/*.exit; do
    tool="$(basename "$f" .exit)"
    code="$(cat "$f")"
    echo "$tool exit $code"
    grep -q "$tool" "$out/report.md"
    [ "$code" -le 1 ]
  done
}
run_step report "report.md + report.json; no measured tool crashed" step_report

# ---- 11. live API (read-only) -----------------------------------------------
step_live() {
  local bin
  bin="$(nix build --no-link --print-out-paths ".#packages.$sys.runpodctl")/bin/runpodctl"
  "$bin" gpu list | jq -e 'length > 0' > /dev/null
  "$bin" pod list | jq -e . > /dev/null
  "$bin" user | jq -e . > /dev/null
  if [ -n "$engine" ] && [ "${#images[@]}" -gt 0 ]; then
    local ref
    ref="$("$(nix build --no-link --print-out-paths ".#packages.$sys.oci-runpodctl")" | "$engine" load | sed -n 's/^Loaded image[^:]*: //p' | tail -n1)"
    "$engine" run --rm -e RUNPOD_API_KEY "$ref" gpu list | jq -e 'length > 0' > /dev/null
    "$engine" rmi "$ref" > /dev/null
  fi
  echo "gpu list, pod list, user: valid json"
}
if [ -n "${RUNPOD_API_KEY:-}" ]; then
  run_step live-api "read-only gpu/pod/user against the live api" step_live
else
  skip_step live-api "read-only gpu/pod/user against the live api" "RUNPOD_API_KEY is not set"
fi

# ---- 12. cross-system -------------------------------------------------------
skip_step cross-system "builds on the other two systems" "evaluated in flake-shape; built by the nix.yml matrix"

# ---- summary ----------------------------------------------------------------
echo
echo "| Status | Step | Description | Time |"
echo "| --- | --- | --- | ---: |"
for r in "${results[@]}"; do
  IFS='|' read -r status id desc took <<< "$r"
  echo "| $status | $id | $desc | $took |"
done
echo
echo "logs: $logdir"
if [ "$failures" -gt 0 ]; then
  echo "$failures step(s) failed"
  exit 1
fi
echo "all steps passed"

<!--
  nix/design.md — runpodctl
  Design for the Nix flake: layout, how the binary and OCI images are built,
  the dev shell, the hermetic checks, fuzzing, CI and the phased rollout.
  Written before the implementation; nix/README.md documents usage once it exists.
-->

# runpodctl — Nix flake design

This document describes how we plan to add a **[Nix](https://nixos.org)** flake to runpodctl, and why each decision was made. It's the first step of the rollout in [section 12](#12-phased-rollout): nothing here exists yet, and `nix/README.md` will be written once the commands it documents do.

> **The Nix path is optional and additive.** goreleaser, `release.yml`, `install.sh`, Homebrew and the existing `ci.yml` stay exactly as they are. Nobody has to install Nix to build, test or release runpodctl.

---

## 1. Goals and non-goals

### Goals

- **Run it from GitHub** — `nix run github:runpod/runpodctl -- --help`, and `nix profile install github:runpod/runpodctl`, with no clone and no Go toolchain.
- **A reproducible dev shell** — `nix develop` gives every contributor the same Go, linters, fuzzing and shell tooling, pinned by `flake.lock`.
- **Build the binary with Nix** — a hermetic `buildGoModule` build with the same flags goreleaser uses.
- **Small OCI images** — built with `dockerTools`, no base image, non-root.
- **Strict static analysis and fuzzing** — run hermetically by `nix flake check`, and in GitHub CI as a **non-blocking** check until the existing findings are fixed.

### Non-goals

- Replacing goreleaser. Releases, darwin signing and notarisation (quill), Homebrew and the release assets all stay in `release.yml`.
- Running `e2e/`. Those tests need `RUNPOD_API_KEY` and create real, billed resources, so they can never be a sandboxed check.
- Intel macs (`x86_64-darwin`).
- Publishing images to a registry. That's a follow-up decision (see [section 13](#13-open-questions-and-risks)).

### Platforms

| System | Package, dev shell, checks | OCI images |
| --- | --- | --- |
| `x86_64-linux` | ✅ | ✅ (`linux/amd64`) |
| `aarch64-linux` | ✅ | ✅ (`linux/arm64`) |
| `aarch64-darwin` | ✅ | — |

---

## 2. Layout

The flake follows the same modular design as xtcp2: a thin `flake.nix` orchestrator that delegates to files under `nix/`, grouped by concern. Modules are wired with plain `import ./x.nix { inherit … }`, never `callPackage`, so every argument a module receives is visible at its call site.

```text
flake.nix                     thin orchestrator: inputs, systems, outputs
nix/
  design.md                   this document
  README.md                   user guide (phase 6)
  default.nix                 per-system aggregator -> packages, devShells, checks, apps
  versions.nix                single source of truth: Go, vendorHash, ldflags, tools, fuzz time
  source.nix                  lib.fileset sources: the full Go tree, plus narrower per-check sets
  overlays.nix                overlays.default -> pkgs.runpodctl
  lib/
    mk-go-check.nix           runCommand helper for Go checks over the module cache
    mk-check.nix              writeShellApplication + runCommand helper for non-Go checks
    mk-oci-image.nix          streamLayeredImage wrapper
  packages/
    runpodctl.nix             buildGoModule
  containers/
    default.nix               oci-runpodctl, oci-runpodctl-full
  dev/
    commands.nix              table of check-* helpers (one source for shell and apps)
    shell.nix                 mkShell + runpodctl-help banner
  checks/
    default.nix               data table of checks -> mk-go-check / mk-check
    lint-baseline.txt         committed lint ratchet (phase 4)
  apps/
    default.nix               runners that need network or write to the tree
```

### `flake.nix`

- Inputs are `nixpkgs` (`nixos-unstable`) and `flake-utils`, both locked in `flake.lock`. Nothing else.
- `flake-utils.lib.eachSystem [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ]`.
- `overlays.default` sits outside the per-system block.
- The header comment carries a "quick reference" of the common commands, as xtcp2's does.

### Conventions

- Every `.nix` file opens with a `# nix/<path>.nix — runpodctl` header and a paragraph explaining *why* it exists.
- `nixfmt-rfc-style` formatting; unused arguments are prefixed with `_`.
- Derivations are named `runpodctl-<kebab>`. Attribute prefixes (`oci-*`, `lint-*`, `fuzz-*`) let the aggregator collect families with `lib.hasPrefix` instead of hand-written lists.
- Policy is passed as arguments at the call site (for example, which checks are gated), never buried inside a module.

---

## 3. Building the binary

`nix/packages/runpodctl.nix` wraps `buildGoModule`, overridden to use the Go from `versions.nix`:

| Setting | Value | Why |
| --- | --- | --- |
| `env.CGO_ENABLED` | `"0"` | matches goreleaser and the makefile; a static binary |
| `subPackages` | `[ "." ]` | the CLI is `main.go` at the root |
| `ldflags` | `-s -w -X main.Version=<version>` | identical to `.goreleaser.yml` |
| `GOFLAGS` | `-trimpath` | no build paths in the binary |
| `proxyVendor` | `true` | see below |
| `doCheck` | `false` | tests are separate checks, so a test change doesn't rebuild the package |
| `meta.mainProgram` | `runpodctl` | makes `nix run` and `lib.getExe` work |

**`proxyVendor = true`.** With it, `goModules` is a GOPROXY-format module cache rather than a `vendor/` tree. The repo has no `vendor/` directory and every build uses `-mod=mod`, so this matches how runpodctl is built today. It also lets `go mod verify` and `go mod tidy -diff` run inside the sandbox with `GOPROXY=file://<goModules>`, which a vendor tree can't support. *To confirm in phase 2:* that `tidy -diff` finds every module it needs in that cache.

**Embedded files.** `cmd/project/functions.go` embeds `starter_examples/*`, `starter_examples/*/.*` (dotfiles) and `exampleDockerfile`. `nix/source.nix` must keep all of them, and the `go-test` check fails loudly if one is missing, because the embed won't compile.

### Version

- runpodctl has a single version variable: `var Version = "v3.0.0"` (kept current by release-please) in `main.go`, set through `-X main.Version`.
- The flake parses that default with `builtins.match` and appends the commit: `"<base>-${self.shortRev or self.dirtyShortRev}"`. This mirrors goreleaser's `{{ .Version }}-{{ .ShortCommit }}`.
- If the parse fails (say, the line is reformatted), evaluation fails with a message naming `main.go`, instead of silently stamping `unknown`.
- The `version-stamp` check asserts that the built binary reports the flake rev.

### `vendorHash`

- The hash of the module cache lives in `versions.nix`.
- Any `go.mod`/`go.sum` change, including Dependabot PRs, breaks the Nix build until the hash is updated.
- `nix run .#update-vendor-hash` rebuilds with a fake hash, reads the `got: sha256-…` line and rewrites `versions.nix`.
- This drift is the main maintenance cost of the flake, and the main reason CI starts as non-blocking ([section 10](#10-ci)).

---

## 4. One Go version

The Go version is pinned in three places today (`go.mod`, `ci.yml`, `release.yml`). The flake must not become a fourth that drifts.

- **nixpkgs lags Go point releases.** The locked `nixos-unstable` ships Go **1.26.8**, which carries the ten `net/http`, `crypto/tls`, `net/textproto` and `os` advisories fixed in 1.26.9 (see PR #371). So `versions.nix` overrides `go_1_26` to 1.26.9, fetched from `go.dev/dl` by hash, as xtcp2 does:

  ```nix
  go = pkgs.go_1_26.overrideAttrs (_old: rec {
    version = "1.26.9";
    src = pkgs.fetchurl {
      url = "https://go.dev/dl/go${version}.src.tar.gz";
      hash = "sha256-…";
    };
  });
  ```

- **Enforced at evaluation time.** `versions.nix` reads the `go` directive from `go.mod` and `assert`s `lib.versionAtLeast go.version goModVersion`. The directive is a minimum, so this is Go's own semantics, and it lets the flake build with 1.26.9 while `go.mod` on main still says 1.26.6 (#371 bumps it; the two are then equal). A `go.mod` bump past the flake's Go fails `nix flake check` immediately, with a message saying which file to edit.
- **`GOTOOLCHAIN=local`** in every check and build, so Go never tries to download a toolchain in the sandbox.
- **Recommendation for the workflows:** `actions/setup-go` with `go-version-file: go.mod`, so `go.mod` becomes the one hand-edited source and the flake asserts against it.

---

## 5. OCI images

`nix/containers/default.nix` builds two images through `nix/lib/mk-oci-image.nix`, a wrapper around `dockerTools.streamLayeredImage`. Images are linux only; each linux system builds its own architecture natively, so there's no cross-compilation and no qemu.

| Image | Contents | For |
| --- | --- | --- |
| `oci-runpodctl` | `runpodctl`, `dockerTools.caCertificates` | API commands: pods, serverless, templates, volumes, logs, send/receive |
| `oci-runpodctl-full` | slim + `rsync`, `openssh`, `coreutils` | the `project` sync path, which runs `rsync -e ssh …` |

### Both images

- No base image and no shell. The slim image is the binary plus a CA bundle, so its size is close to the binary's.
- **Non-root:**
  - uid/gid `65532`, `HOME=/home/runpodctl`.
  - The home directory and `/tmp` are created writable through `fakeRootCommands`.
  - This matters because `initConfig` always creates `~/.runpod/config.toml` (mode 0600), and `ssh` writes `~/.runpod/ssh/known_hosts`.
- `Env`: `SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt`, `HOME=/home/runpodctl`. CA roots are also compiled into the binary (`golang.org/x/crypto/x509roots/fallback`), so the bundle is a preference, not a requirement.
- `Entrypoint: [ "<store path>/bin/runpodctl" ]` (`lib.getExe`; the image has no `/bin`), so `docker run … oci-runpodctl pod list` behaves like the CLI.
- The API key is passed as `-e RUNPOD_API_KEY`. `viper.AutomaticEnv()` already honours it, so nothing has to be written into the image.
- OCI labels: `org.opencontainers.image.{source,revision,version}`.

**Multi-arch.** An `oci-push` app (skopeo) copies each arch's stream to a registry and joins them into one manifest list. It's an app rather than a check, because it needs network and credentials, and stays unused until we decide where images are published.

**Size.** The `oci-size-report` app measures each stream (`"$image" | wc -c`) and writes a table to the CI job summary, so regressions are visible in review.

---

## 6. Dev shell

`nix develop` provides:

| Group | Tools |
| --- | --- |
| Go | pinned `go`, `gopls`, `gotools`, `delve` |
| Static analysis | `golangci-lint`, `staticcheck` (`go-tools`), `gosec`, `govulncheck` |
| Runtime deps for tests | `openssh`, `rsync` (so the real-binary ssh/rsync tests run instead of skipping) |
| Shell, CI and Nix | `shellcheck`, `actionlint`, `nixfmt-rfc-style`, `deadnix`, `statix` |
| Misc | `jq`, `skopeo`, `benchstat` (`goperf`) |

### Helpers

- `nix/dev/commands.nix` is the single table of `check-*` commands, the flash pattern.
- Each one is built as a `writeShellApplication`, the xtcp2 pattern, so the helper scripts themselves are shellchecked and run under `set -euo pipefail`.
- The same table backs `nix run .#check-*`, so the shell and the apps can't disagree.
- On entry, a `runpodctl-help` banner lists the helpers.
- The `shellHook` only runs the banner and sets `CGO_ENABLED=0` and `GOTOOLCHAIN=local`; no other environment magic.

---

## 7. Checks

`nix flake check` runs everything below **hermetically**: sandboxed, offline, and independent of what's installed on the machine.

### How a Go check runs (`nix/lib/mk-go-check.nix`)

- Copy the narrowed source to a writable directory.
- Set `HOME=$TMPDIR`. `initConfig` writes `~/.runpod/config.toml` whenever `Execute()` runs, so a check must never see a real home.
- `GOFLAGS=-mod=mod`, `GOPROXY=file://<goModules>`, `GOTOOLCHAIN=local`, `CGO_ENABLED=0`, and `GOCACHE`/`GOMODCACHE` under `$TMPDIR`.
- Write the tool's output to `$out`, and on failure print it before exiting non-zero.
- Record provenance in `$out` (`command.sh`, `go-version.txt`), as xtcp2's linkmonitor checks do.
- On darwin, `__darwinAllowLocalNetworking = true`, because about 35 test files bind `127.0.0.1:0`.

**Each check gets its own fileset** from `nix/source.nix` (Go sources for Go checks, `*.nix` for Nix checks, `*.sh` for shellcheck, `.github/workflows` for actionlint). So editing a workflow doesn't re-run the Go tests.

### Tier 0: correctness (fast; gated from the start)

| Check | Runs |
| --- | --- |
| `go-vet` | `go vet ./...` and `go vet -tags e2e ./...` (e2e code is vetted, never run) |
| `gofmt` | `gofmt -l .` must print nothing |
| `go-mod-verify` | `go mod verify` |
| `go-test` | `go test -count=1 -cover ./...` with `openssh` and `rsync` on PATH |
| `cli-help-smoke` | the built binary: `--help`, `version`, and `<cmd> --help` for every top-level command. Must exit 0, write to stdout, and never put an error object on stdout (CON-683) |
| `version-stamp` | `runpodctl version` contains the flake rev |
| `fuzz-seeds` | every `Fuzz*` target's seed corpus (see [section 8](#8-fuzzing)) |
| `nixfmt` / `deadnix` / `statix` | the Nix layer itself: `nixfmt --check`, `deadnix --fail`, `statix check` |
| `shellcheck` | `shellcheck --severity=style` on every `*.sh`; no `# shellcheck disable` anywhere. `-o all` (about 150 optional findings in `install.sh`) is a follow-up |
| `actionlint` | every workflow under `.github/workflows` |

### Tier 1 and 2: pedantic static analysis

| Check | Runs |
| --- | --- |
| `lint-quick` | golangci-lint v2, `.golangci-quick.yml`: gofmt, goimports, govet, errcheck, ineffassign, unused, staticcheck |
| `lint-standard` | `.golangci.yml`: quick + gosec, gocritic, revive, noctx, contextcheck, bodyclose, errorlint, misspell, modernize, … |
| `lint-comprehensive` | `.golangci-comprehensive.yml`: standard + exhaustive, prealloc, dupl, goconst, unconvert, nakedret, … |
| `staticcheck` | `staticcheck -checks all ./...` |
| `gosec` | `gosec ./...`; every exclusion needs a written reason in the check file |

Rules for the golangci-lint configs (the same ones xtcp2 follows):

- `version: "2"`, `linters.default: none` with explicit lists, so a golangci-lint upgrade can't silently add or drop linters.
- `max-issues-per-linter: 0` and `max-same-issues: 0`, so nothing is hidden.
- `build-tags: [ e2e ]`, so the e2e package is linted too.
- Never pass `--timeout` on the command line; it silently overrides `run.timeout` in the config.

**The lint ratchet.** Turning the tiers on will report findings in existing code. Rather than block on fixing them all at once:

- `nix/checks/lint-baseline.txt` is committed with one line per finding, `<tool>\t<path> | <message> (<rule>)`: no line or column, and digits in messages read as `N`, so unrelated edits don't move a finding. Counts were the first idea, but they can't satisfy the next rule.
- The `lint-baseline` check compares the findings as a multiset. A new finding fails even if an old one was fixed in the same PR, and a fixed finding that is still listed fails too, so the file only ever shrinks through review.
- `nix run .#update-lint-baseline` rewrites the file. Review sees the diff.
- One measure derivation (`static-analysis-measure`) runs every tool once; the check and the report (`nix build .#static-analysis-report`) both read it.
- Findings are fixed in small per-package PRs. A fix is a code change. A `//nolint` is allowed only with a linter name and a written reason (enforced by golangci-lint's `nolintlint`).
- When the file is empty, the baseline file is deleted and the tiers gate directly.

### Race detector

- `go-test-race` (`CGO_ENABLED=1`, `gcc`, `-race`, linux only) is defined but **not gated** yet. `cmd/transfer`'s tests currently race inside upstream croc's global logger (`tcp.Run` calls `logger.SetLevel` while `tcp.PingServer` reads it).
- Once that's fixed, the check is gated, and `ci.yml` can add `-race` too.

**Not a check: `go mod tidy -diff`.** Tidy resolves test dependencies of dependencies, which the proxyVendor cache doesn't hold, so it can't run offline. It's the dev helper `check-tidy`.

**Not a check: `govulncheck`.** It needs the live vulnerability database, so it can't run in the sandbox. It's an app (`nix run .#govulncheck`, and `static-analysis-report-full` adds it to the report) and a step in the Nix CI workflow. It complements, but doesn't replace, the `govulncheck` step `ci.yml` already runs.

---

## 8. Fuzzing

runpodctl had no fuzz targets. The fuzzing layer is built so that adding one is the only step needed: no Nix edit, no list to update.

- **Discovery.** `go test -list '^Fuzz' <pkg>` for each package in `go list ./...`. There's no hard-coded target list to drift (xtcp2 hard-codes its targets; we deliberately don't).
- **`fuzz-seeds` check.** Runs each target with `-run '^Fuzz…$'`, i.e. the seed corpus plus `testdata/fuzz/` only. It's deterministic and fast, so it's safe to gate. It also means every crasher a long run finds, once committed to `testdata/fuzz/`, becomes a permanent regression test.
- **`nix run .#fuzz`.** Long fuzzing, outside the sandbox: `--fuzztime` (default from `versions.nix`), `--pkg` / `--target` filters, one target at a time, `GOMAXPROCS` capped. A failure leaves the crasher in `testdata/fuzz/` for triage and exits non-zero.
- **First targets.** These are the parsers and validators where hostile input reaches security decisions:

| Target | Invariant |
| --- | --- |
| `rsyncConnectionLost` (`cmd/project/ssh.go`) | a refused host key is never classified as a restarting pod |
| `rsyncRemoteShell` / `quoteSSHConfigValue` | rsync's and openssh's parsers split the result back into the original arguments |
| receive manifest validation, `foldKey` (`cmd/transfer/confine.go`) | no accepted manifest resolves outside the destination; case-fold/NFC aliases collide |
| `validateChunkRanges` (`cmd/transfer/croc.go`) | an accepted resume request is well-formed and its true sum (no overflow) is within the file's chunks |
| `internal/duration` parsing | accepted values are positive, and a day count is exactly that many days |

---

## 9. Benchmarks

Timings inside the sandbox are noisy and machine-dependent, so benchmarks are apps, never checks:

- `nix run .#bench`: `go test -run '^$' -bench . -benchmem -count 6 ./...`, output kept for comparison.
- `nix run .#bench-compare`: runs both the working tree and `origin/main`, then prints a `benchstat` comparison.

Today the only benchmarks are in `cmd/serverless/randomstring_bench_test.go`. New benchmarks are discovered automatically, like the fuzz targets.

---

## 10. CI

A new workflow, `.github/workflows/nix.yml`. `ci.yml` is not changed.

> **Non-blocking by design.** The job sets `continue-on-error: true` and is not a required status check, so a red Nix run never blocks a merge while the lint ratchet is non-zero and `vendorHash` updates are manual.

| Setting | Value |
| --- | --- |
| Triggers | `pull_request`, `push` to `main`, nightly `schedule` (for long fuzzing) |
| Permissions | `contents: read` |
| Actions | pinned by full commit SHA, with the version in a comment |
| Nix install | `cachix/install-nix-action`, `experimental-features = nix-command flakes` |
| Matrix | `ubuntu-latest` (x86_64-linux), `ubuntu-24.04-arm` (aarch64-linux), `macos-latest` (aarch64-darwin) |

### Steps per runner

1. `nix flake check -L`
2. `nix build .#runpodctl -L`, then `./result/bin/runpodctl --version`
3. Linux only: build both images, `docker load` them, run `--version` in each, and write `oci-size-report` to `$GITHUB_STEP_SUMMARY`.
4. `nix run .#govulncheck`

**Separate fuzz job** (x86_64-linux): `nix run .#fuzz -- --fuzztime <ci value>` on PRs, and a longer budget on the nightly schedule. Crashers are uploaded as an artifact.

**Making it blocking.** The workflow becomes a required check once all of these hold:

- the lint baseline is zero,
- `go-test-race` is gated,
- `vendorHash` updates are automated (for example, a bot pushing the new hash to Dependabot PRs).

---

## 11. Self-update under Nix

- `runpodctl update` downloads the latest release and replaces its own binary in place (`cmd/update.go`).
- A Nix-installed binary lives in the read-only `/nix/store`, so that fails, and it would undo Nix's pinning even if it worked.
- **Follow-up code change:** when the running executable resolves under `/nix/store`, `update` exits with a `usage_error` (on stderr, per CON-683) naming `nix profile upgrade` instead.
- **Until then:** `nix/README.md` documents the limitation.

---

## 12. Phased rollout

Each phase is one PR, small enough to review on its own:

| # | Phase | Delivers |
| --- | --- | --- |
| 1 | Design | this document |
| 2 | Core flake | `flake.nix`, `versions.nix`, `source.nix`, package, dev shell, Tier 0 checks, `nix.yml` (non-blocking) |
| 3 | Images | `oci-runpodctl`, `oci-runpodctl-full`, `oci-size-report`, `oci-push` |
| 4 | Static analysis | golangci-lint configs, `staticcheck`, `gosec`, lint baseline and ratchet |
| 5 | Fuzzing and benchmarks | the first fuzz targets, `fuzz-seeds`, `fuzz`, `bench`, `bench-compare` |
| 6 | Docs | `nix/README.md` in the flash style, linked from the top-level README |
| 7 | Hardening | per-package lint fixes, the croc race fix, the self-update guard, then `nix.yml` becomes required |

### Every phase

- `nix flake check` passes on all three systems.
- The built binary is smoke-tested against the live API with read-only commands (`gpu list`, `pod list`).
- `CLAUDE.md` gets any pitfall that can't be fixed in code (`vendorHash` drift is the likely first one).

---

## 13. Open questions and risks

- **`vendorHash` ownership.** Who updates it, and do we automate it on Dependabot PRs? This is the main ongoing cost.
- **Binary cache.** Cachix (shared with users, needs a token) or magic-nix-cache (CI only, no secrets)? Without one, every CI run rebuilds Go itself, because the 1.26.9 override isn't in `cache.nixos.org`.
- **Image publishing.** GHCR under `runpod/`, signing (cosign) and tags. Out of scope until someone needs the image.
- **Darwin sandbox.** Loopback tests need `__darwinAllowLocalNetworking`. If the darwin sandbox still rejects them, the affected checks become linux-only, and the reason is written in the check file.
- **`tools/upx`.** The committed 561 KB executable stays out of the Nix path. Nix images aren't upx-compressed, because stripping already gives most of the gain, and a compressed binary is slower to start and harder to scan.
- **PR #371 dependency.** `main`'s `go.mod` is still at 1.26.6 until #371 merges. The flake pins whatever `go.mod` says, and the assertion in [section 4](#4-one-go-version) keeps the two together.

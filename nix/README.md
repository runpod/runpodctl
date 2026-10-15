<!--
  nix/README.md — runpodctl
  Guide to the Nix flake: running runpodctl from GitHub, the dev shell, the
  hermetic checks, the OCI images and the static analysis report. Linked from
  the top-level README. The design and its reasoning are in nix/design.md.
-->

# runpodctl — Nix flake

This explains how to use **[Nix](https://nixos.org)** with **runpodctl**: run the CLI from GitHub with one command, get a reproducible dev environment, and run the same checks, static analysis and fuzzing that CI runs.

> **The Nix path is optional.** `go build`, the `makefile`, `ci.yml` and the goreleaser release are unchanged. Nix is here for those who want it.

**Platforms:** `x86_64-linux`, `aarch64-linux` and `aarch64-darwin` (Apple Silicon). The OCI images are linux only.

---

## 1. Install Nix

If you already have Nix with flakes enabled, skip to [section 2](#2-run-runpodctl-from-github).

```bash
# multi-user (recommended): https://nix.dev/manual/nix/latest/installation/
bash <(curl -L https://nixos.org/nix/install) --daemon
```

Then enable flakes in `~/.config/nix/nix.conf`:

```text
experimental-features = nix-command flakes
```

---

## 2. Run runpodctl from GitHub

```bash
nix run github:runpod/runpodctl -- pod list
nix profile install github:runpod/runpodctl     # onto your PATH
```

The binary is built exactly as goreleaser builds it (`CGO_ENABLED=0`, `-trimpath`, `-s -w`), and `runpodctl version` reports `<version>-<commit>`.

> **`runpodctl update` does not work on a Nix install.** It replaces its own binary, and `/nix/store` is read-only. Use `nix profile upgrade` instead.

---

## 3. Dev shell

```bash
nix develop
```

You get Go (the version pinned in `nix/versions.nix`, with `GOTOOLCHAIN=local` so it never downloads another), golangci-lint, staticcheck, gosec, govulncheck, gopls, delve, shellcheck, the Nix linters and benchstat. The shell prints its commands on entry (`runpodctl-help` prints them again). Each one is also an app, so `check-test` in the shell and `nix run .#check-test` outside it are the same thing.

| Group | Commands |
| --- | --- |
| check | `check-vet` `check-fmt` `check-tidy` `check-test` `check-race` `check-all` |
| lint | `lint-quick` `lint` `lint-comprehensive` `lint-fix` `check-staticcheck` `check-gosec` `govulncheck` `update-lint-baseline` `static-analysis-report` `static-analysis-report-full` |
| fuzz | `fuzz` `bench` `bench-compare` |
| nix | `update-vendor-hash` `test-integration` |
| oci | `oci-size-report` `oci-push` |

---

## 4. Packages

| Package | What |
| --- | --- |
| `runpodctl` (default) | the CLI |
| `oci-runpodctl` | OCI image: the CLI and CA certificates; no shell, uid 65532 |
| `oci-runpodctl-full` | the same plus rsync, openssh and coreutils, for `project` sync |
| `static-analysis-measure` | every static analysis tool's raw output, run once |
| `static-analysis-report` | `report.md` and `report.json` built from the measure |
| `staticreport` | the Go tool (`tools/staticreport`) behind the baseline and the report |
| `go-test-race` | the race detector, linux only, **not gated** (see below) |

The images are streamed, not stored:

```bash
nix build .#oci-runpodctl && ./result | docker load
docker run --rm -e RUNPOD_API_KEY runpodctl:<tag> pod list
```

The API key is passed at run time and never baked into a layer.

---

## 5. Checks

`nix flake check` runs these sandboxed and offline. Each check reads only the files it needs, so a docs edit rebuilds nothing.

| Check | Runs |
| --- | --- |
| `go-vet` | `go vet ./...`, plain and `-tags e2e` |
| `gofmt` | `gofmt -l .` prints nothing |
| `go-mod-verify` | `go mod verify` against the module cache |
| `go-test` | `go test -count=1 -cover ./...`, with real openssh and rsync |
| `fuzz-seeds` | every `Fuzz*` target's seeds and committed `testdata/fuzz/` crashers |
| `cli-help-smoke` | `--help` for every command the binary lists; no error object on stdout |
| `version-stamp` | the binary reports the flake's commit |
| `lint-baseline` | no new static analysis finding, and no fixed one still listed |
| `nixfmt` `deadnix` `statix` | the Nix files |
| `shellcheck` | every `*.sh`, and no `# shellcheck disable` anywhere |
| `actionlint` | every workflow |

`go-test-race` is a package rather than a check: `cmd/transfer`'s tests race inside upstream croc's global logger. It is gated once that is fixed; until then the integration test expects it to fail.

A single check, with its log:

```bash
nix build -L .#checks.x86_64-linux.go-test
cat result/output.txt
```

---

## 6. Static analysis

Three golangci-lint tiers, each a superset of the one before (`.golangci-quick.yml`, `.golangci.yml`, `.golangci-comprehensive.yml`), plus `staticcheck -checks all` and `gosec`.

**The report.**

```bash
nix run .#static-analysis-report          # hermetic tools, printed
nix run .#static-analysis-report-full     # + govulncheck (needs the network) -> ./static-analysis-report/
```

It has a summary per tool, a security section (govulncheck, gosec by rule and severity, staticcheck's SA checks), findings by package, the findings that are new against the baseline, and the details per tool. `report.json` holds the same data.

**The ratchet.** `nix/checks/lint-baseline.txt` lists every finding that existed when the tools were turned on, one per line, without line numbers, so unrelated edits don't move them. The `lint-baseline` check fails on a new finding *and* on a fixed finding that is still listed. After fixing findings:

```bash
nix run .#update-lint-baseline     # rewrites the file; commit the shrinking diff
```

Findings are fixed in code. A `//nolint` needs the linter's name and a written reason (`nolintlint` enforces it).

---

## 7. Fuzzing

Targets are discovered with `go test -list '^Fuzz'`, so adding a `Fuzz*` test is the only step.

```bash
nix run .#fuzz                                  # every target, 60s each
nix run .#fuzz -- --budget nightly --pkg transfer
nix run .#fuzz -- --fuzztime 5m --target FuzzValidateManifest
```

A failure leaves the input under the package's `testdata/fuzz/<target>/`. Commit it with the fix and `fuzz-seeds` keeps it as a regression test.

---

## 8. Maintenance

**`vendorHash`.** After any change to `go.mod` or `go.sum`, the build fails with a hash mismatch until you run:

```bash
nix run .#update-vendor-hash
```

**Go version.** `nix/versions.nix` pins Go and asserts it is at least `go.mod`'s `go` directive. Raising `go.mod` past it fails evaluation with a message naming the file to edit.

**Integration test.** `nix run .#test-integration` builds every package and check the flake exposes on this system, and runs the negative checks (a gofmt error, a vet error, a new lint finding and a planted fuzz crasher must each fail). It also runs the apps, the dev shell and the images under docker or podman. With `RUNPOD_API_KEY` set it adds read-only API calls (`gpu list`, `pod list`, `user`). It prints a PASS/FAIL/SKIP table.

---

## 9. CI

`.github/workflows/nix.yml` runs the checks, the binary, the images and the full report on all three systems, and fuzzes on pull requests and nightly. It is **non-blocking**: no job is a required check while the lint baseline is non-empty. The report is written to the job summary and uploaded as an artifact.

# Runpodctl evidence report

Date: 2026-02-04  
Branch: `refactor/cli-restructure` (baseline: `origin/main`)

## Executive summary

- There are 41 open issues in `runpod/runpodctl`, with repeated reports of install failures, broken core commands, and unreliable file transfer.
- Official Runpod docs still position `runpodctl` as the primary CLI for Pods and file transfers, including examples that assume commands like `runpodctl create pods`, `runpodctl get pod`, and `runpodctl send/receive`.
- External GitHub projects install and invoke `runpodctl`, which raises the impact of compatibility breaks.
- The current branch expands CLI coverage significantly (serverless, templates, volumes, registry, billing, user info, GPU types, datacenters, model repo, doctor) and adds output formatting, while keeping legacy command wrappers for key old syntax.

## Evidence of limitations and complaints (issues)

Open issue count: 41 (as of 2026-02-04).

### Installation and update problems

- #221 Download script not working  
  https://github.com/runpod/runpodctl/issues/221  
  > "The download script is broken. The generated download URL is wrong."
- #150 README install leaves old version installed  
  https://github.com/runpod/runpodctl/issues/150  
  > "runpodctl version -> still v1.8.0" after running the installer
- #149 TLS errors when running runpodctl inside a Pod  
  https://github.com/runpod/runpodctl/issues/149  
  > "x509: certificate signed by unknown authority"

### Core pod lifecycle and creation

- #189 CLI cannot create specific GPU types (works in UI)  
  https://github.com/runpod/runpodctl/issues/189  
  > "Error: There are no longer any instances available... But if I do the same from the runpod website... it works"
- #161 CPU pod creation fails because `gpuType` required  
  https://github.com/runpod/runpodctl/issues/161  
  > "Error: required flag(s) \"gpuType\" not set"
- #46 `runpodctl get pod` returns null  
  https://github.com/runpod/runpodctl/issues/46  
  > "Error: data is nil: {\"data\":{\"myself\":null}}"
- #45 `runpodctl start pod` returns error response  
  https://github.com/runpod/runpodctl/issues/45  
  > "Error: Something went wrong. Please try again later or contact support."

### Template and environment handling gaps

- #163 Template settings not applied  
  https://github.com/runpod/runpodctl/issues/163  
  > "disk / volume mount path / volume size are not applied"
- #162 Template requires `--imageName` even though UI has one  
  https://github.com/runpod/runpodctl/issues/162  
  > "--imageName should not be required when trying to create a pod from the CLI."
- #204 Env vars with equals not supported  
  https://github.com/runpod/runpodctl/issues/204  
  > "If the value contains an equals... the value rejected"

### Project workflow instability

- #195 Project files are not synced  
  https://github.com/runpod/runpodctl/issues/195  
  > "ERROR: Could not open requirements file... cp: cannot stat '.runpodignore'"
- #173 Inconsistent working directory between dev and prod  
  https://github.com/runpod/runpodctl/issues/173  
  > "In Development: /dev/<project> ... In Production: /prod/<project>/src"

### SSH and connection issues

- #228 `runpodctl ssh connect` outputs nothing  
  https://github.com/runpod/runpodctl/issues/228  
  > "It just exits 0 but doesn't log out or output anything"
- #179 Hardcoded `root` user breaks non-root images  
  https://github.com/runpod/runpodctl/issues/179  
  > "SSH client configuration is hardcoded to use the root user"

### File transfer reliability

- #185 Croc panic during transfers  
  https://github.com/runpod/runpodctl/issues/185  
  > "panic error ... sendData ... 430 GB"
- #38 Transfers stuck at 90%  
  https://github.com/runpod/runpodctl/issues/38  
  > "runpodctl always fails... stuck at 90%"

### Output/feature gaps

- #148 JSON output requested  
  https://github.com/runpod/runpodctl/issues/148  
  > "Is it possible to get runpodctl to return json?"
- #147 Balance info requested  
  https://github.com/runpod/runpodctl/issues/147  
  > "get balance information via runpodctl ... for monitoring"

## Official materials and usage references

### Runpod docs that assume runpodctl

- Runpod CLI overview: install and use `runpodctl`  
  https://docs.runpod.io/runpodctl/overview  
  Mentions `runpodctl config`, `runpodctl version`, and installation commands.
- Manage Pods doc uses `runpodctl create pods`, `runpodctl stop pod`, `runpodctl remove pods`, `runpodctl get pod`  
  https://docs.runpod.io/pods/manage-pods
- Transfer files doc positions `runpodctl` as the "quick, occasional transfers" method and shows `runpodctl send/receive`  
  https://docs.runpod.io/pods/storage/transfer-files
- Network volumes doc references `runpodctl send/receive` for migration and embeds a video tutorial  
  https://docs.runpod.io/storage/network-volumes  
  Video: https://www.youtube.com/embed/gnSLRrlBfcA
- "Choose a workflow" doc says every Pod comes with `runpodctl` preinstalled  
  https://docs.runpod.io/get-started/connect-to-runpod

### External GitHub usage (selected examples)

- `FurkanGozukara/Stable-Diffusion` uses `runpodctl stop pod` in quick commands  
  https://github.com/FurkanGozukara/Stable-Diffusion/blob/main/Useful-Commands.md
- `neural-maze/neural-hub` installs `runpodctl` in a setup script  
  https://github.com/neural-maze/neural-hub/blob/main/vision-rag-complex-pdf/infrastructure/bash_scripts/install_runpodctl.sh
- `wilsonzlin/hackerverse` installs `runpodctl` in a Dockerfile  
  https://github.com/wilsonzlin/hackerverse/blob/main/Dockerfile.runpod-base

### Web search note

Attempted open web search via `WebFetch` (Bing/DuckDuckGo) repeatedly timed out, so external web references are limited to GitHub and official Runpod docs.

## Compatibility check (docs vs new CLI)

The current branch keeps the binary as `runpodctl` and reorganizes commands into noun-verb groups. Legacy wrappers exist for core old commands.

| Documented `runpodctl` command | Status in new CLI | Replacement / notes |
| --- | --- | --- |
| `runpodctl config --apiKey` | Deprecated | `runpodctl doctor` (legacy `runpodctl config` still exists but hidden) |
| `runpodctl create pod` | Supported | `runpodctl pod create` (legacy `runpodctl create pod` hidden) |
| `runpodctl create pods` | Supported | legacy bulk-create via `runpodctl create pods` |
| `runpodctl get pod` | Supported | `runpodctl pod list` (legacy `runpodctl get pod` hidden) |
| `runpodctl get pod <id>` | Supported | `runpodctl pod get <id>` |
| `runpodctl get cloud` | Replaced | `runpodctl gpu list` and `runpodctl datacenter list` provide availability |
| `runpodctl start pod` | Supported | `runpodctl pod start` (legacy `runpodctl start pod` hidden) |
| `runpodctl stop pod` | Supported | `runpodctl pod stop` (legacy `runpodctl stop pod` hidden) |
| `runpodctl remove pod` | Supported | `runpodctl pod delete` (legacy `runpodctl remove pod` hidden) |
| `runpodctl remove pods` | Supported | legacy bulk-delete via `runpodctl remove pods` |
| `runpodctl send` / `receive` | Supported | `runpodctl send` / `runpodctl receive` (transfer subsystem retained) |
| `runpodctl ssh list-keys` | Supported | `runpodctl ssh list-keys` |
| `runpodctl ssh add-key` | Supported | `runpodctl ssh add-key` |
| `runpodctl ssh connect` | Deprecated | `runpodctl ssh info` (legacy alias exists) |
| `runpodctl update` | Supported | `runpodctl update` |
| `runpodctl version` | Supported | `runpodctl version` or `runpodctl --version` |

## New capabilities in the current branch (vs origin/main)

### Resource coverage expansion

- Serverless endpoints: `runpodctl serverless list/get/create/update/delete`
- Templates: `runpodctl template list/get/search/create/update/delete`
- Network volumes: `runpodctl volume list/get/create/update/delete`
- Container registry auth: `runpodctl registry list/get/create/delete`
- Model repository: `runpodctl model list/add/remove` with upload workflow support

### Account, availability, and cost visibility

- `runpodctl user` shows account info including balance and spend
- `runpodctl billing` history for pods, serverless, and network volumes
- `runpodctl gpu list` for GPU types and availability
- `runpodctl datacenter list` for availability by region

### Pod workflow improvements

- Unified `runpodctl pod` group with `list/get/create/update/start/stop/restart/reset/delete`
- Template-first pod creation with explicit flags for data centers, ports, and mount paths
- Standardized output formatting (`--output json|yaml`)

### Operational and UX improvements

- `runpodctl doctor` for configuration and SSH key setup (replaces `runpodctl config`)
- `runpodctl ssh info` shows SSH command + key status
- `runpodctl completion` for shell completions
- Hidden legacy commands preserve old `runpodctl` syntax to reduce breakage

## Risks, mitigations, and next steps

- Docs and install paths should continue to reference `runpodctl` to avoid user confusion.
- The most-used doc examples (`create pods`, `remove pods`) do not map cleanly to new commands; either re-expose bulk operations or update docs with new equivalents.
- Publish a migration guide:
  - Old -> new command mappings (table above)
  - Legacy compatibility window and deprecation timelines
- Re-run install instructions on base images to ensure the correct binary/version is installed.

<!-- Do not edit or remove this section -->
This document exists for non-obvious, error-prone shortcomings in the codebase, the model, or the tooling that an agent cannot figure out by reading the code alone. No architecture overviews, file trees, build commands, or standard behavior. When you encounter something that belongs here, first consider whether a code change could eliminate it and suggest that to the user. Only document it here if it can't be reasonably fixed.

---

## pitfalls

- templates are dual-source: official/community via graphql, user via rest; list/search merge results and apply search/pagination client-side; graphql failures are intentionally best-effort.
- graphql template shapes are inconsistent: `ports` may be string or array, `env` is key/value pairs; normalize before output and only return `readme/env/ports` on `template get`.
- `doctor` is the only mutating setup path (api key + ssh sync); onboarding/ssh changes must update both `cmd/doctor` and `internal/sshconnect` hints.
- legacy commands must preserve stdout and behavior exactly; deprecation warnings go to stderr only (exec is the most common regression).
- CON-683 exception to the above: errors must never reach stdout. `api/query.go` and `internal/api/graphql.go` return the typed `no_credentials` sentinel instead of printing; `cmd/model` returns every error to the `Execute` sink. still unconverted, and still violating this: `cmd/pod/*Pod.go`, `cmd/pods/*`, `cmd/cloud` (plaintext via `cobra.CheckErr`) and `cmd/project/*` (prints to stdout and exits 0 — was CON-816, now CON-874, which supersedes it and deletes the command rather than fixing it). see `TestLegacyCommandKeepsStdoutCleanWithoutAPIKey`.
- `cmd/project.go` is not wired into the cli; the hidden `project` command is created in `cmd/root.go` and wraps `cmd/project/*`.
- any test that `Execute()`s a *runnable* command must redirect `HOME` (and `USERPROFILE`) to `t.TempDir()`. `cobra.OnInitialize(initConfig)` fires from the hook chain, ahead of every `PersistentPreRunE`, and `initConfig` writes `~/.runpod/config.toml` whenever it can't read one — so such a test creates a real config, and on an unreadable one overwrites it with defaults and destroys the developer's stored api key. `--help`-only tests don't hit this: cobra returns before the initializers. cannot be fixed in the test alone — `OnInitialize` is a cobra package global that runs for any root you build.
- `initConfig` runs from `cobra.OnInitialize`, i.e. ahead of every `PersistentPreRunE`, and `cmd/config/config.go:114` binds `--apiKey` into viper at package init. so a *rejected* `config --apiKey=… --output=table` still persists that key to a freshly created `~/.runpod/config.toml`. no validation in the hook chain can prevent this; only moving `initConfig` after the hooks would.
- `--output` is validated in the root `PersistentPreRunE`, which means it only guards commands cobra actually runs. `--help`, `help <cmd>`, a bare parent, `--version`/`-v` and completion all resolve earlier and skip it; `help` additionally needs the explicit exemption in that hook so a bad `--output` can't hide the help that lists valid values.
- api accepts `gpuTypeIds` arrays, but the cli is intentionally singular (`--gpu-id`); multi-id fallback must be an explicit new flag.
- pod creation uses graphql (`podFindAndDeployOnDemand`) because the rest api rejects `startSsh` — do not move pod create to rest without confirming `startSsh` support.
- graphql `dataCenterId` is singular; rest uses `dataCenterIds` (plural). graphql `ports` is a comma-separated string; rest uses `[]string`. graphql `env` returns `["KEY=VALUE"]`; rest uses `map[string]string`. graphql takes a raw `dockerArgs` string; rest has no `dockerArgs` field (rejects it as an extra key) — shlex-split into `dockerStartCmd` instead (see `splitDockerArgs`).
- serverless create is graphql-only (`saveEndpoint`); the rest `/endpoints` create path is intentionally unused so model references + multi-region volumes + all flags work on one path. do not reintroduce rest routing.
- `saveEndpoint.gpuIds` wants gpu *pool* ids (e.g. `ADA_24`), not the gpu *type* ids (e.g. `NVIDIA A40`) that `gpu list` / `--gpu-id` use; translate via `serverlessGpuPools` (`ResolveServerlessGpuPoolID`). pod create uses type ids directly — different identifier space.
- `saveEndpoint` has no `computeType`: cpu is selected by sending `instanceIds` like `cpu3g-4-16` (gpu is the default when instanceIds is empty). `name` is required (`String!`, min 3) and is never auto-generated server-side — generate one client-side. flashboot is the `flashBootType` enum (`OFF`/`FLASHBOOT`), not a bool. multi-region volumes are `networkVolumeIds: [{networkVolumeId}]`, not a flat string array.

## constraints

- all text output must be lowercase and concise.
- default output format is json (for agent consumption); never change this default.
- e2e tests cost real money — always clean up resources with `t.Cleanup`.
- always e2e test cli changes before considering work done: build the binary, run the new/changed commands against the live api (`RUNPOD_API_KEY` is in the env), and clean up any created resources immediately after verifying the response. this is non-negotiable.
- keep the runpodctl skill in sync: https://github.com/runpod/runpod-plugins-official/tree/main/plugins/runpod/skills/runpodctl — update it whenever commands, flags, or behavior change.

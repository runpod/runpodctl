# CLAUDE.md — sharp edges & gotchas

## pitfalls

- templates are dual-source: official/community via graphql, user via rest; list/search merge results and apply search/pagination client-side; graphql failures are intentionally best-effort.
- graphql template shapes are inconsistent: `ports` may be string or array, `env` is key/value pairs; normalize before output and only return `readme/env/ports` on `template get`.
- `doctor` is the only mutating setup path (api key + ssh sync); onboarding/ssh changes must update both `cmd/doctor` and `internal/sshconnect` hints.
- legacy commands must preserve stdout and behavior exactly; deprecation warnings go to stderr only (exec is the most common regression).
- `cmd/project.go` is not wired into the cli; the hidden `project` command is created in `cmd/root.go` and wraps `cmd/project/*`.
- api accepts `gpuTypeIds` arrays, but the cli is intentionally singular (`--gpu-id`); multi-id fallback must be an explicit new flag.
- pod creation uses graphql (`podFindAndDeployOnDemand`) because the rest api rejects `startSsh` — do not move pod create to rest without confirming `startSsh` support.
- graphql `dataCenterId` is singular; rest uses `dataCenterIds` (plural). graphql `ports` is a comma-separated string; rest uses `[]string`. graphql `env` returns `["KEY=VALUE"]`; rest uses `map[string]string`.

## constraints

- all text output must be lowercase and concise.
- default output format is json (for agent consumption); never change this default.
- e2e tests cost real money — always clean up resources with `t.Cleanup`.
- keep the runpodctl skill in sync: https://github.com/runpod/skills/tree/main/runpodctl — update it whenever commands, flags, or behavior change.

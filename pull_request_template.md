# E-XXXX: Title of Pull Request

- resolves 17+ tracked issues across pods/serverless/templates/gpu/datacenter/billing/ssh/doctor/help.
- maintains full legacy command compatibility (stderr warnings only, stdout unchanged).
- keeps `runpodctl` as the primary binary; drop-in upgrade for existing users.
- expands api coverage with rest-first + graphql fallback and json/yaml output for automation.
- improves onboarding with `doctor` and auto-migration from `~/.runpod.yaml` to `~/.runpod/config.toml`.
- tests: full unit suite + full e2e suite, with e2e covering all cli entrypoints (legacy + new) and cleanup via `t.Cleanup`.

Test plan:
- [ ] go test ./...
- [ ] go test -tags e2e ./e2e

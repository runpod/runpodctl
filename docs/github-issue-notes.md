# GitHub Issue Notes (Pending)

- #31 Public IP filter (community cloud)
  - Add flag on pod create (e.g., `--public-ip` / `--require-public-ip`)
  - Map to API field that enforces public IP on community pods
  - Add E2E test that creates a community pod with public IP requirement

- #190 Global networking option
  - Add flag on pod create (e.g., `--global-network`)
  - Wire to API field once confirmed
  - Add E2E coverage

- #152 Python version for exec
  - Add `--python` flag to `runpod exec python` (default `python3`)
  - Use the flag when running remote commands
  - Add unit test for flag handling

- #118 Help text consistency
  - Normalize help strings (consistent casing)
  - Remove optional plurals like `(s)`
  - Update command help across CLI

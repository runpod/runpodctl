# skill sync check

`tools/skill-sync` validates that the Runpod agent skill stays aligned with the public `runpodctl` command surface without turning `SKILL.md` into a full flag reference.

The check verifies that:

- `SKILL.md` says live `runpodctl --help` output is authoritative for exact flags.
- Public top-level commands are mentioned.
- Fenced `runpodctl ...` examples use valid public command paths.
- Hidden or deprecated commands are not recommended in examples.

Run it from the `runpodctl` repo with a sibling checkout of `runpod/skills`:

```bash
go run ./tools/skill-sync --skill ../skills/runpodctl/SKILL.md
```

This is a local check only. Release automation can call this tool later once the checker behavior is stable.

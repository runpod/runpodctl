# Issue #152: Python Version Hardcoded

**GitHub:** https://github.com/runpod/runpodctl/issues/152
**Type:** Bug
**Priority:** High
**Effort:** 5 minutes
**Status:** ✅ RESOLVED

---

## Resolution

The `exec` command has been deprecated in the CLI restructure. Users who need to run remote commands should use:

```bash
# Get SSH connection info
runpod ssh info <pod-id>

# Use the provided SSH command to connect and run any command
ssh -i ~/.runpod/ssh/RunPod-Key-Go root@<ip> -p <port> "python3 /path/to/script.py"
```

This approach:
- Works with any Python version installed in the container
- Gives users full control over the remote command
- Doesn't require maintaining a separate `exec` code path

---

## Original Issue

**Author:** gontsharuk
**Created:** June 26, 2024
**Comments:** None

> In line 21 of `cmd/exec/functions.go` instead of just executing the default python version in the container by using `python3`, `python3.11` is used. This is a problem when using most of the runpod pytorch templates.

---

## Why Deprecation Was Chosen

1. **The `exec` command was limited** - Only supported Python, not arbitrary commands
2. **SSH provides full flexibility** - Users can run any command with any interpreter
3. **New CLI provides better SSH support** - `runpod ssh info` returns complete connection details
4. **Reduces maintenance burden** - No need to maintain python version detection logic

---

## Recommendation

**✅ RESOLVED - Close the GitHub issue**

The underlying problem (users couldn't run Python scripts properly) is solved by using SSH directly. The `exec` command deprecation removes the need to maintain hardcoded interpreter paths.

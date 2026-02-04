# Issue #152: Python Version Hardcoded

**GitHub:** https://github.com/runpod/runpodctl/issues/152
**Type:** Bug
**Priority:** High
**Effort:** 5 minutes

---

## Summary

The `runpodctl exec python` command has `python3.11` hardcoded, which breaks compatibility with most RunPod PyTorch templates that use `python3.10` or the system default.

---

## Original Issue

**Author:** gontsharuk
**Created:** June 26, 2024
**Comments:** None

> In line 21 of `cmd/exec/functions.go` instead of just executing the default python version in the container by using `python3`, `python3.11` is used. This is a problem when using most of the runpod pytorch templates.

---

## Current Code

File: `cmd/exec/functions.go`, line 21:

```go
if err := sshConn.RunCommand("python3.11 /tmp/" + file); err != nil {
```

---

## Recommended Fix

Change `python3.11` to `python3`:

```go
if err := sshConn.RunCommand("python3 /tmp/" + file); err != nil {
```

This uses whatever Python 3 version is the default in the container, which is the expected behavior.

---

## Why This Should Be Fixed

1. **It's a bug** - The hardcoded version breaks functionality
2. **Affects RunPod's own templates** - Most official PyTorch templates use python3.10
3. **Simple fix** - One line change, no architectural decisions needed
4. **No comments/debate** - Clear issue with clear solution

---

## Implementation Steps

1. Open `cmd/exec/functions.go`
2. Change line 21 from `python3.11` to `python3`
3. Test with a pod using an official PyTorch template
4. Commit and close issue

---

## Recommendation

**✅ YES - Fix this immediately**

This is objectively broken and the fix is trivial.

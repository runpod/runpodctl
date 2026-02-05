# Issue #190: Global Networking Option

**GitHub:** https://github.com/runpod/runpodctl/issues/190
**Type:** Feature Request
**Priority:** Low
**Effort:** 15 minutes
**Status:** ✅ RESOLVED

---

## Resolution

Global networking is now supported in the CLI via a dedicated flag on pod creation:

```bash
# Enable global networking (secure cloud only)
runpodctl pod create --global-networking --image ubuntu:latest --gpu-type-id "NVIDIA RTX 4090"
```

Notes:
- Only available for on-demand GPU pods on some secure cloud data centers.
- If you pin `--data-center-ids` and creation fails, try another secure data center or omit the pin.

---

## Original Issue

**Author:** YayL
**Created:** June 1, 2025

> Why is there no global networking option?

That's the entire issue. No description, no use case, no expected behavior.

---

## What is Global Networking?

RunPod's "Global Networking" feature allows pods in different data centers/regions to communicate with each other over a private network. This is useful for:
- Distributed training across regions
- Multi-region deployments
- Connecting pods that need to share data

---

## How It's Implemented

- New flag: `--global-networking` on `runpodctl pod create`
- Request field: `globalNetworking` in the REST create payload
- CLI validation enforces GPU + secure cloud constraints
- Errors are decorated with a hint when the API rejects global networking

---

## Recommendation

**✅ RESOLVED - Close the GitHub issue**

The CLI now supports the same global networking toggle as the UI (within API constraints).

# Issue #161: CPU Pod Support

**GitHub:** https://github.com/runpod/runpodctl/issues/161
**Type:** Feature Gap (GUI/CLI parity)
**Priority:** High
**Effort:** 30 minutes

---

## Summary

Users cannot create CPU-only pods via CLI because `--gpuType` is a required flag. This works fine in the web UI. Users want to run CPU-intensive tasks without paying for GPU.

---

## Original Issue

**Author:** gwpl (Grzegorz Wierzowiecki)
**Created:** September 12, 2024

> Dear Maintainers,
>
> I am trying to deploy CPU pod (for testing),
>
> and as much as tool seems working with GPU pods, for CPU pods I get:
>
> `Error: required flag(s) "gpuType" not set`
>
> Extra user wish: additionally, probably providing command lines examples in WebUI or documentation would be also helpful for convenient parameter settings or to have templates to tweak or follow.
>
> Thank you for great service.

---

## Comments

**matanyall** (Nov 3, 2024):
> Yeah, we need cpu serverless support here if we are going to be able to use this feature of runpod

**gwpl (author)** (Nov 3, 2024):
> But I didn't want to spin on serverless... I wanted to spin up instances, perform on them CPU intense tasks, and bring them down.

---

## Current Behavior

```bash
$ runpod pod create --image ubuntu:latest
Error: required flag(s) "gpuType" not set
```

The CLI requires `--gpuType` even when the user wants a CPU-only pod.

---

## Recommended Fix

Add a `--compute-type` flag to `pod create`:

```bash
# GPU pod (current behavior, still works)
runpod pod create --image ubuntu:latest --gpu-type-id "NVIDIA RTX 4090"

# CPU pod (new)
runpod pod create --image ubuntu:latest --compute-type CPU
```

---

## Implementation Steps

1. **Modify `cmd/pod/create.go`:**
   - Add flag: `--compute-type` with values `GPU` (default) or `CPU`
   - Change validation: only require `--gpu-type-id` when `--compute-type` is `GPU`

2. **Modify `internal/api/pods.go`:**
   - Add `ComputeType` field to `PodCreateRequest` struct
   - Ensure it's passed to the API

3. **Test:**
   - Create a CPU pod via CLI
   - Verify it appears in web UI as CPU pod
   - Verify GPU pods still work

---

## Why This Should Be Fixed

1. **GUI/CLI parity gap** - Web UI supports CPU pods, CLI should too
2. **Cost savings for users** - CPU pods are cheaper for non-GPU workloads
3. **Legitimate use case** - Data processing, web servers, testing
4. **Multiple users asking** - 2 people commented with agreement
5. **RunPod supports this** - It's not a new feature request, just CLI catching up

---

## Recommendation

**✅ YES - Implement this**

This closes a real feature gap between the GUI and CLI. Users have a legitimate need to create CPU pods programmatically for cost optimization.

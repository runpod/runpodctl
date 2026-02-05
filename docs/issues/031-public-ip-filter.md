# Issue #31: Public IP Filter for Community Cloud

**GitHub:** https://github.com/runpod/runpodctl/issues/31
**Type:** Feature Request
**Priority:** Medium
**Effort:** 15 minutes
**Status:** ✅ RESOLVED

---

## Summary

The CLI now supports `--public-ip` on `runpod pod create`, which maps to the REST `supportPublicIp` field for community cloud.

---

## Original Issue

**Author:** hyperknot (Zsolt Ero)
**Created:** March 24, 2023 (almost 3 years old!)

> Right now, there is no way to make sure a newly created instance will have a public IP, when selecting from the community cloud. Please add a feature like on the UI.

*Included screenshot of web UI showing "Public IP" filter option*

---

## Comments (5 total)

**all-mute** (Mar 2024): "+1"

**lipsumar** (May 2024): "+1 - and Internet Speed"

**pdlje82** (May 2024):
> +1
> As I am developing on a runpod instance, I need the public IP / SSH over exposed TCP, otherwise my IDE cant connect to the instance

**jojje (contributor)** (Oct 2024) - **Important clarification:**
> From what I can tell, community cloud pods **always get public IP addresses**.
>
> However a SSH connection can not be made because **the SSH daemon isn't running** in the pod.
>
> That said, I have a [PR #165](https://github.com/runpod/runpodctl/pull/165) open for review that would solve this last hurdle.

jojje then demonstrates:
- Community pod created → gets public IP ✅
- SSH connection fails → "Connection refused" (SSH not running)
- With his PR's `--startSSH` flag → SSH works ✅

---

## Resolution

The CLI now exposes a dedicated flag for the public IP requirement:

```bash
runpod pod create --cloud-type community --public-ip --image ubuntu:22.04 --gpu-type-id "NVIDIA GeForce RTX 3090"
```

Notes:
- `--public-ip` only affects **community** cloud. Secure cloud pods always have public IPs.
- If `--cloud-type SECURE` is set, the CLI prints a no-op note.

---

## Related PR

**PR #165** by jojje adds `--startSSH` flag:
```bash
runpodctl create pod --startSSH --communityCloud --gpuType "NVIDIA GeForce RTX 3070" ...
```

This ensures SSH daemon runs, allowing IDE connections.

**Status of PR #165:** Check if it was merged or needs review.

---

## Implementation Details

- New flag: `--public-ip` on `runpod pod create`
- Request field: `supportPublicIp` in the REST create payload
- Unit + E2E coverage added

---

## Recommendation

**✅ RESOLVED - Close the GitHub issue**

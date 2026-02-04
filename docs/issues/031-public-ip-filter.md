# Issue #31: Public IP Filter for Community Cloud

**GitHub:** https://github.com/runpod/runpodctl/issues/31
**Type:** Feature Request (needs clarification)
**Priority:** Medium
**Effort:** 15-30 minutes (if we decide to implement)

---

## Summary

User requested ability to filter for public IP when creating pods on community cloud. However, a contributor clarified that community cloud pods **always** get public IPs - the real issue may be SSH daemon not running.

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

## Analysis

The issue has **two possible interpretations:**

1. **Original interpretation:** "I want to filter for pods that will have public IP"
   - But jojje says community pods ALWAYS get public IPs
   - So this filter might be unnecessary for community cloud

2. **Real pain point:** "I can't SSH into my pod"
   - This is what pdlje82 actually needs (IDE connection)
   - jojje's PR #165 (`--startSSH`) addresses this

**Questions to answer:**
- Do **secure cloud** pods sometimes NOT have public IPs?
- If yes, then a `--public-ip` filter makes sense for secure cloud
- If no, then the issue should be closed with explanation + merge PR #165

---

## Related PR

**PR #165** by jojje adds `--startSSH` flag:
```bash
runpodctl create pod --startSSH --communityCloud --gpuType "NVIDIA GeForce RTX 3070" ...
```

This ensures SSH daemon runs, allowing IDE connections.

**Status of PR #165:** Check if it was merged or needs review.

---

## Recommended Actions

1. **Check PR #165 status** - If not merged, consider merging it
2. **Clarify the issue** - Ask: "Does secure cloud ever lack public IP?"
3. **Decide:**
   - If secure cloud always has public IP → Close issue, point to PR #165
   - If secure cloud sometimes lacks public IP → Add `--public-ip` filter

---

## Implementation (if needed)

Add to `cmd/pod/create.go`:
```go
createCmd.Flags().BoolVar(&requirePublicIP, "public-ip", false, "require public IP (for secure cloud)")
```

Add to `internal/api/pods.go` `PodCreateRequest`:
```go
PublicIPFilter bool `json:"publicIpFilter,omitempty"`
```

---

## Why This Needs Clarification First

1. **Contributor says it's not needed** for community cloud
2. **Real issue might be SSH**, which PR #165 fixes
3. **3 years old** with no official response - suggests low priority
4. **Don't want to add unnecessary flags** that confuse users

---

## Recommendation

**⚠️ INVESTIGATE FIRST**

1. Check if PR #165 was merged
2. Verify if secure cloud pods need this filter
3. Then decide whether to implement or close with explanation

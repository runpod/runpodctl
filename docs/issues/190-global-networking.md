# Issue #190: Global Networking Option

**GitHub:** https://github.com/runpod/runpodctl/issues/190
**Type:** Feature Request (unclear)
**Priority:** Low
**Effort:** 15 minutes (if we decide to implement)

---

## Summary

User asks why there's no global networking option in the CLI. No context provided, no community engagement.

---

## Original Issue

**Author:** YayL
**Created:** June 1, 2025

> Why is there no global networking option?

That's the entire issue. No description, no use case, no expected behavior.

---

## Comments

None.

---

## What is Global Networking?

RunPod's "Global Networking" feature allows pods in different data centers/regions to communicate with each other over a private network. This is useful for:
- Distributed training across regions
- Multi-region deployments
- Connecting pods that need to share data

In the web UI, this appears as a toggle when creating a pod.

---

## Analysis

**Problems with this issue:**
1. **No context** - We don't know why they need it
2. **No use case** - What are they trying to accomplish?
3. **No community engagement** - Zero comments, zero +1s
4. **Very recent** - Only 8 months old, but no traction

**The feature itself is straightforward:**
- Just a boolean flag: `--global-networking`
- Add to create request: `GlobalNetworking: true`
- Low implementation effort

**But should we prioritize it?**
- Only 1 person asked
- No explanation of need
- Might be a niche requirement

---

## Recommended Actions

**Option A: Ask for clarification**
Comment on the issue:
> Hi @YayL, could you share your use case for global networking via CLI? Understanding how you'd use this feature would help us prioritize it. Thanks!

**Option B: Just implement it**
It's a simple boolean flag. If RunPod supports it in the API, we could just add it without much debate.

**Option C: Deprioritize**
With no community interest and no explanation, focus on higher-priority issues first.

---

## Implementation (if needed)

Add to `cmd/pod/create.go`:
```go
var globalNetworking bool

createCmd.Flags().BoolVar(&globalNetworking, "global-networking", false, "enable global networking between pods")
```

Add to `internal/api/pods.go` `PodCreateRequest`:
```go
GlobalNetworking bool `json:"globalNetworking,omitempty"`
```

---

## Why This Should Wait

1. **No demonstrated need** - Only 1 person, no explanation
2. **No community validation** - Zero engagement in 8 months
3. **Higher priority issues exist** - #152 and #161 are more impactful
4. **Unknown if commonly needed** - Might be very niche

---

## Recommendation

**❌ NOT YET - Ask for details first**

Comment on the issue asking for the use case. If they respond with a compelling reason, or if others +1 it, then consider implementing. Otherwise, deprioritize in favor of clearer, more impactful issues.

If you want to be proactive, implementing it is low-effort (15 min), but without understanding the need, it's hard to know if we're solving a real problem.

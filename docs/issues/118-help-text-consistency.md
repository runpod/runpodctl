# Issue #118: Help Text Consistency

**GitHub:** https://github.com/runpod/runpodctl/issues/118
**Type:** Polish/Documentation
**Priority:** Low
**Effort:** 20-30 minutes

---

## Summary

Help command strings are inconsistent - some start with capital letters, others don't. Also uses "(s)" for optional plurals which violates Google's style guide.

---

## Original Issue

**Author:** rachfop (Patrick Rachford)
**Created:** February 23, 2024

Shows the help output:
```
Available Commands:
  completion  Generate the autocompletion script for the specified shell
  config      Manage CLI configuration
  create      create a resource          <-- lowercase
  exec        Execute commands in a pod  <-- capitalized
  get         get resource               <-- lowercase
  help        Help about any command
  project     Manage RunPod projects
  receive     receive file(s), or folder <-- uses (s)
  remove      remove a resource
  send        send file(s), or folder    <-- uses (s)
  ssh         SSH keys and commands
  start       start a resource
  stop        stop a resource
  update      update runpodctl
```

> Some start with caps others do not. Should be consistent.
> Also dont use (s):
>
> "Don't put optional plurals in parentheses. Instead, use either plural or singular constructions and keep things consistent throughout your documentation."
>
> https://developers.google.com/style/plurals-parentheses

---

## Comments

None.

---

## Analysis

**The issues are valid:**
1. **Inconsistent capitalization** - "create" vs "Execute" vs "Manage"
2. **"(s)" pattern** - "file(s)" violates style guides
3. **Professional appearance** - Inconsistency looks sloppy

**However:**
- This is purely cosmetic
- No functional impact
- No users complaining about it affecting their work
- The author appears to be documentation-focused (references Google style guide)

---

## Current State (New CLI)

Let me check if the new CLI has the same issues:

```
Available Commands:
  billing        view billing history
  completion     install shell completion
  datacenter     list datacenters
  doctor         diagnose and fix cli issues
  gpu            list available gpu types
  help           Help about any command
  model          manage model repository
  network-volume manage network volumes
  pod            manage gpu pods
  receive        receive file(s) or folder    <-- still has (s)
  registry       manage container registry auth
  send           send file(s) or folder       <-- still has (s)
  serverless     manage serverless endpoints
  ssh            manage ssh keys and connections
  template       manage templates
  update         update runpod cli
  user           show account info
  version        print the version
```

**New CLI status:**
- ✅ Capitalization is now consistent (all lowercase)
- ❌ Still uses "(s)" in send/receive

---

## Recommended Fix

Update `cmd/transfer/transfer.go` (or wherever send/receive are defined):

**Before:**
```go
Short: "receive file(s) or folder"
Short: "send file(s) or folder"
```

**After:**
```go
Short: "receive files or folders"
Short: "send files or folders"
```

Or use singular:
```go
Short: "receive a file or folder"
Short: "send a file or folder"
```

---

## Files to Modify

1. `cmd/transfer/transfer.go` - send and receive short descriptions
2. Any other files with "(s)" pattern (search for it)

---

## Why This Is Low Priority

1. **No functional impact** - CLI works the same either way
2. **No user complaints** - Only 1 person mentioned it, no +1s
3. **Cosmetic only** - Doesn't affect usability
4. **Already partially fixed** - New CLI has consistent capitalization

---

## Recommendation

**⚠️ LOW PRIORITY - Do eventually**

This is valid feedback but should not be prioritized over functional issues like #152 and #161.

**When to do it:**
- During a "cleanup" pass before a major release
- When you have spare time between higher-priority tasks
- As a good first issue for a new contributor

**Don't:**
- Rush this before more important fixes
- Spend more than 20-30 minutes on it

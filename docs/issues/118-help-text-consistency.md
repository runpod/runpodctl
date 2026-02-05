# Issue #118: Help Text Consistency

**GitHub:** https://github.com/runpod/runpodctl/issues/118
**Type:** Polish/Documentation
**Priority:** Low
**Effort:** 20-30 minutes
**Status:** ✅ RESOLVED

---

## Summary

Help command strings were inconsistent (capitalization and "(s)" plurals). The CLI now uses consistent lowercase help text and avoids "(s)" in command descriptions.

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

**New CLI status:**
- ✅ Capitalization is consistent (all lowercase)
- ✅ No "(s)" in send/receive or other command descriptions
**How to verify:**
- `runpod --help` shows lowercase descriptions
- `send`/`receive` descriptions use proper plural wording

---

## Resolution

Command help text is now consistent and lowercase across the CLI. `(s)` patterns were removed in favor of proper plurals.

---

## Files Updated

1. `cmd/transfer/transfer.go` and `cmd/croc/*.go` - send/receive descriptions
2. Command help strings across `cmd/` for consistent lowercase

---

## Why This Is Low Priority

1. **No functional impact** - CLI works the same either way
2. **No user complaints** - Only 1 person mentioned it, no +1s
3. **Cosmetic only** - Doesn't affect usability
4. **Already partially fixed** - New CLI has consistent capitalization

---

## Recommendation

**✅ RESOLVED - Close the GitHub issue**

Help text is now consistent and avoids optional plurals.

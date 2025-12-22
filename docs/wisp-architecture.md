# Wisp Architecture: Ephemeral Molecule Storage

> Status: Design Spec v1 - December 2024

## Overview

**Wisps** are ephemeral molecule execution traces - the "steam" in Gas Town's engine
metaphor. This document specifies where wisps are stored, how they're managed, and
which roles use them.

## Core Principle

**Wisps are local operational state, not project history.**

| Artifact | Storage | Git Tracked | Purpose |
|----------|---------|-------------|---------|
| Issues | `.beads/issues.jsonl` | Yes | Permanent project history |
| Wisps | `.beads-ephemeral/issues.jsonl` | **No** | Transient execution traces |
| Digests | `.beads/issues.jsonl` | Yes | Compressed summaries of squashed wisps |

## Storage Architecture

### Directory Structure

```
~/gt/gastown/
├── mayor/rig/                    # Mayor's canonical clone
│   ├── .beads/                   # CANONICAL rig beads (versioned)
│   │   ├── issues.jsonl          # Permanent issues + digests
│   │   ├── config.yaml
│   │   └── .gitignore            # Excludes .beads-ephemeral
│   │
│   └── .beads-ephemeral/         # GITIGNORED - local wisps
│       └── issues.jsonl          # In-progress ephemeral molecules
│
├── refinery/rig/                 # Refinery's clone
│   ├── .beads/                   # Inherits from mayor/rig
│   └── .beads-ephemeral/         # Refinery's local wisps
│
├── witness/                      # Witness (no clone needed)
│   └── .beads-ephemeral/         # Witness's local wisps
│
└── polecats/<name>/              # Polecat worktrees
    ├── .beads/                   # Inherits from mayor/rig
    └── .beads-ephemeral/         # Polecat's local wisps (if using wisps)
```

### Key Points

1. **`.beads-ephemeral/` is gitignored** - Never synced, never versioned
2. **Each execution context has its own ephemeral store** - Process isolation
3. **Digests go to canonical `.beads/`** - Permanent record after squash
4. **Wisps are deleted after squash/burn** - No accumulation

### Gitignore Entry

Add to `.beads/.gitignore`:
```
.beads-ephemeral/
```

Or add to rig-level `.gitignore`:
```
**/.beads-ephemeral/
```

## Wisp Lifecycle

```
bd mol bond <proto> --ephemeral
         │
         ▼
┌─────────────────────────┐
│  .beads-ephemeral/      │
│  └── issues.jsonl       │  ← Wisp created here
│      └── {id, ephemeral: true, ...}
└────────────┬────────────┘
             │
    ┌────────┴────────┐
    ▼                 ▼
bd mol burn      bd mol squash
    │                 │
    ▼                 ▼
(deleted)        ┌─────────────────────────┐
                 │  .beads/issues.jsonl    │
                 │  └── {id, type: digest} │  ← Digest here
                 └─────────────────────────┘
```

## Role Assignments

### Roles That Use Wisps

These roles have repetitive/cyclic work that would accumulate without wisps:

| Role | Molecule | Storage Location | Squash Frequency |
|------|----------|------------------|------------------|
| **Deacon** | mol-deacon-patrol | mayor/rig/.beads-ephemeral/ | Per cycle |
| **Witness** | mol-witness-patrol | witness/.beads-ephemeral/ | Per cycle |
| **Refinery** | mol-refinery-cycle | refinery/rig/.beads-ephemeral/ | Per cycle |

### Roles That Use Regular Molecules

These roles do discrete work with audit value:

| Role | Molecule | Storage | Reason |
|------|----------|---------|--------|
| **Polecat** | mol-polecat-work | .beads/issues.jsonl | Each assignment is a deliverable |
| **Mayor** | (ad-hoc) | .beads/issues.jsonl | Coordination has history value |
| **Crew** | (ad-hoc) | .beads/issues.jsonl | User work needs audit trail |

### Decision Matrix

| Question | Answer | Use |
|----------|--------|-----|
| Is this work repetitive/cyclic? | Yes | Wisp |
| Does the outcome matter more than the trace? | Yes | Wisp |
| Would this accumulate unbounded over time? | Yes | Wisp |
| Is this a discrete deliverable? | Yes | Regular Mol |
| Might I need to reference this later? | Yes | Regular Mol |
| Does this represent user-requested work? | Yes | Regular Mol |

## Patrol Pattern

Every role using wisps must implement this pattern:

```go
func patrolCycle() {
    // 1. Bond ephemeral molecule
    mol := bdMolBond("mol-<role>-patrol", "--ephemeral")

    // 2. Execute cycle steps
    for _, step := range mol.Steps {
        executeStep(step)
        bdMolStep(step.ID, "--complete")
    }

    // 3. Generate summary (agent cognition)
    summary := generateCycleSummary()

    // 4. Squash - REQUIRED (this is the cleanup)
    bdMolSquash(mol.ID, "--summary", summary)
    // Wisp deleted from .beads-ephemeral/
    // Digest created in .beads/issues.jsonl

    // 5. Sleep until next cycle
    time.Sleep(patrolInterval)
}
```

**Critical**: Without step 4 (squash), wisps become technical debt.

## Beads Implementation Requirements

For this architecture to work, Beads needs:

### New Commands

```bash
# Bond with ephemeral flag
bd mol bond <proto> --ephemeral
# Creates in .beads-ephemeral/ instead of .beads/

# List ephemeral molecules
bd wisp list
# Shows in-progress wisps

# Garbage collect orphaned wisps
bd wisp gc
# Cleans up wisps from crashed processes
```

### Storage Behavior

| Command | With `--ephemeral` | Without |
|---------|-------------------|---------|
| `bd mol bond` | Creates in `.beads-ephemeral/` | Creates in `.beads/` |
| `bd mol step` | Updates in ephemeral | Updates in permanent |
| `bd mol squash` | Deletes from ephemeral, creates digest in permanent | Creates digest in permanent |
| `bd mol burn` | Deletes from ephemeral | Marks abandoned in permanent |

### Config

```yaml
# .beads/config.yaml
ephemeral:
  enabled: true
  directory: ../.beads-ephemeral  # Relative to .beads/
  auto_gc: true                    # Clean orphans on bd init
```

## Crash Recovery

If a patrol crashes mid-cycle:

1. **Wisp persists in `.beads-ephemeral/`** - Provides recovery breadcrumb
2. **On restart, agent can:**
   - Resume from last step (if step tracking is granular)
   - Or burn and start fresh (simpler for patrol loops)
3. **`bd wisp gc` cleans orphans** - Wisps older than threshold with no active process

### Orphan Detection

A wisp is orphaned if:
- `process_id` field exists and process is dead
- OR `updated_at` is older than threshold (e.g., 1 hour)
- AND molecule is not complete

## Digest Format

When a wisp is squashed, the digest captures the outcome:

```json
{
  "id": "gt-xyz.digest-001",
  "type": "digest",
  "title": "Deacon patrol cycle @ 2024-12-21T10:30:00Z",
  "description": "Checked 3 witnesses, 2 refineries. All healthy. Processed 5 mail items.",
  "parent": "gt-xyz",
  "squashed_from": "gt-xyz.wisp-001",
  "created_at": "2024-12-21T10:32:00Z"
}
```

Digests are queryable:
```bash
bd list --type=digest --parent=gt-deacon-patrol
# Shows all patrol cycle summaries
```

## Migration Path

For existing Gas Town installations:

1. **Add `.beads-ephemeral/` to gitignore** (immediate)
2. **Update patrol runners to use `--ephemeral`** (as patched)
3. **No migration of existing data** - Fresh start for ephemeral storage

## Open Questions

1. **Digest retention**: Should old digests be pruned? How old?
2. **Wisp schema**: Do wisps need additional fields (process_id, host, etc.)?
3. **Cross-process visibility**: Should `bd wisp list` show all wisps or just current process?

## Related Documents

- [architecture.md](architecture.md) - Overall Gas Town architecture
- [patrol-system-design.md](../../../docs/patrol-system-design.md) - Patrol system design
- [molecules.md](molecules.md) - Molecule system details

## Implementation Tracking

- **Beads**: bd-kwjh (Wisp storage: ephemeral molecule tracking)
- **Gas Town**: gt-3x0z.9 (mol-deacon-patrol uses ephemeral)

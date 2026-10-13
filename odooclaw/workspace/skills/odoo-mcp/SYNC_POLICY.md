# Synchronization Policy: odoo-mcp

## Canonical vs Derived Declaration

**Canonical repo**: `https://github.com/nicolasramos/odooclaw-mcp`  
**Derived copy**: `odooclaw/workspace/skills/odoo-mcp/` (embedded in `nicolasramos/odooclaw`)

The embedded copy is DERIVED. It MUST NOT be edited manually. All modifications
to the tool server code are made in the canonical repo and propagated to the
embedded copy via the sync script.

## How to Regenerate the Derived Copy

```bash
# Clone the canonical repo (one-time)
git clone https://github.com/nicolasramos/odooclaw-mcp.git /tmp/odooclaw-mcp

# Run sync (from the odooclaw repo root)
./scripts/sync-odoo-mcp.sh

# Or preview mode to see what would change without writing
./scripts/sync-odoo-mcp.sh --dry-run
```

The script copies these paths from canonical → embedded:
- `src/odoo_mcp/**` — all source code
- `tests/**` — all tests
- `requirements.txt` — dependencies

## CI Protection

The workflow `.github/workflows/odoo-mcp-sync.yml` runs:
- On every push/PR that modifies embedded files
- Daily at 06:00 UTC (Mondays)

If it detects divergence between the two repos, the **build FAILS**.

## Embedded-Only Files

The following files exist only in the embedded copy and are NOT synced:
- `FIXES_SUMMARY.md` — local fix history
- `QA_RUNBOOK.md` — QA runbook specific to the embedded context
- `TOOL_INVENTORY.md` — generated inventory (reference)
- `SYNC_POLICY.md` — this policy document
- `README.md` — contextual documentation (differs by repo: Spanish/embedded vs English/standalone)

These files are specific to the embedded usage context within `odooclaw` and
have no counterpart in the canonical repo.

## Divergence History

As of 2026-10-13: 54 homonymous files, 0 identical. The solution declares
the canonical repo as source of truth and derives the embedded copy.

After sync: 82 homonymous files, 81 identical (README.md excluded by context).

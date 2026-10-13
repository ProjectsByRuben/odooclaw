#!/usr/bin/env bash
# sync-odoo-mcp.sh — Regenerates the embedded odoo-mcp copy from the canonical repo.
#
# Usage: scripts/sync-odoo-mcp.sh [--dry-run]
#
# This script copies the tool server source code from the canonical repo
# (nicolasramos/odooclaw-mcp) to the embedded copy in
# odooclaw/workspace/skills/odoo-mcp/.
#
# The embedded copy is DERIVED and MUST NOT be edited manually.
# If you need to change tool server code, edit the canonical repo and run this script.

set -euo pipefail

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN=true
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
EMBEDDED_DIR="$REPO_ROOT/odooclaw/workspace/skills/odoo-mcp"
CANONICAL_DIR="${ODOO_MCP_CANONICAL_DIR:-/tmp/odooclaw-mcp}"

# Verify canonical repo exists
if [ ! -d "$CANONICAL_DIR/.git" ]; then
    echo "ERROR: Canonical repo not found at $CANONICAL_DIR" >&2
    echo "Clone it with: git clone https://github.com/nicolasramos/odooclaw-mcp.git $CANONICAL_DIR" >&2
    exit 1
fi

echo "=== Sync odoo-mcp ==="
echo "Canonical: $CANONICAL_DIR"
echo "Embedded:  $EMBEDDED_DIR"
echo ""

# --- Sync shared source files (src/, tests/, requirements.txt) ---
(cd "$CANONICAL_DIR" && find src tests requirements.txt -type f) | while read -r relpath; do
    src_file="$CANONICAL_DIR/$relpath"
    dst_file="$EMBEDDED_DIR/$relpath"

    if [ ! -f "$src_file" ]; then
        echo "SKIP (not in canonical): $relpath"
        continue
    fi

    if $DRY_RUN; then
        if [ -f "$dst_file" ] && diff -q "$src_file" "$dst_file" > /dev/null 2>&1; then
            echo "OK       $relpath"
        else
            echo "SYNC     $relpath"
        fi
    else
        mkdir -p "$(dirname "$dst_file")"
        cp "$src_file" "$dst_file"
        echo "SYNC     $relpath"
    fi
done

# --- Build file lists for embedded-only detection ---
{
    find "$EMBEDDED_DIR/src" "$EMBEDDED_DIR/tests" "$EMBEDDED_DIR/requirements.txt" -type f 2>/dev/null | sed "s|^$EMBEDDED_DIR/||"
    if [ -f "$EMBEDDED_DIR/README.md" ]; then
        echo "README.md"
    fi
} | sort -u > /tmp/odoo-mcp-embedded-files

{
    (cd "$CANONICAL_DIR" && find src tests requirements.txt -type f 2>/dev/null | sed 's|^\./||')
} | sort -u > /tmp/odoo-mcp-canonical-files

# --- Remove embedded-only files that should not exist ---
comm -23 /tmp/odoo-mcp-embedded-files /tmp/odoo-mcp-canonical-files | while read -r relpath; do
    # Skip documentation files that are embedded-only by design
    case "$relpath" in
        FIXES_SUMMARY.md|QA_RUNBOOK.md|TOOL_INVENTORY.md|SYNC_POLICY.md|README.md)
            echo "KEEP   $relpath (embedded-only documentation)"
            continue
            ;;
    esac

    if $DRY_RUN; then
        echo "DELETE $relpath"
    else
        rm -f "$EMBEDDED_DIR/$relpath"
        echo "DELETE $relpath"
    fi
done

rm -f /tmp/odoo-mcp-embedded-files /tmp/odoo-mcp-canonical-files

if $DRY_RUN; then
    echo ""
    echo "Dry-run mode: no changes written."
else
    echo ""
    echo "Sync complete. Review with: git diff $EMBEDDED_DIR"
fi

#!/usr/bin/env bash
# sync-odoo-mcp.sh — Copies the embedded odoo-mcp copy to the canonical repo.
#
# Usage: scripts/sync-odoo-mcp.sh [--dry-run]
#
# This script copies the tool server source code from the embedded copy in
# odooclaw/workspace/skills/odoo-mcp/ to the canonical repo at
# $ODOO_MCP_CANONICAL_DIR (default: /tmp/odooclaw-mcp).
#
# The canonical repo is DERIVED from the embedded copy. Do NOT edit the
# canonical repo directly — always edit the embedded copy and run this script.

set -euo pipefail

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN=true
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
EMBEDDED_DIR="$REPO_ROOT/odooclaw/workspace/skills/odoo-mcp"
CANONICAL_DIR="${ODOO_MCP_CANONICAL_DIR:-/tmp/odooclaw-mcp}"

# Verify embedded dir exists
if [ ! -d "$EMBEDDED_DIR" ]; then
    echo "ERROR: Embedded copy not found at $EMBEDDED_DIR" >&2
    exit 1
fi

# Verify canonical repo exists (as a git working tree)
if [ ! -d "$CANONICAL_DIR/.git" ]; then
    echo "ERROR: Canonical repo not found at $CANONICAL_DIR" >&2
    echo "Clone it with: git clone https://github.com/nicolasramos/odooclaw-mcp.git $CANONICAL_DIR" >&2
    exit 1
fi

echo "=== Sync odoo-mcp ==="
echo "Source (embedded): $EMBEDDED_DIR"
echo "Target (canonical): $CANONICAL_DIR"
echo ""

# --- Copy shared source files from embedded to canonical ---
(cd "$EMBEDDED_DIR" && find src tests requirements.txt -type f) | while read -r relpath; do
    src_file="$EMBEDDED_DIR/$relpath"
    dst_file="$CANONICAL_DIR/$relpath"

    if [ ! -f "$src_file" ]; then
        echo "SKIP (not in embedded): $relpath"
        continue
    fi

    if $DRY_RUN; then
        if [ -f "$dst_file" ] && diff -q "$src_file" "$dst_file" > /dev/null 2>&1; then
            echo "OK       $relpath"
        else
            echo "COPY     $relpath"
        fi
    else
        mkdir -p "$(dirname "$dst_file")"
        cp "$src_file" "$dst_file"
        echo "COPY     $relpath"
    fi
done

# --- Remove canonical-only files that should not exist ---
{
    (cd "$CANONICAL_DIR" && find src tests requirements.txt -type f 2>/dev/null | sort)
} > /tmp/odoo-mcp-canon-files

{
    (cd "$EMBEDDED_DIR" && find src tests requirements.txt -type f 2>/dev/null | sort)
} > /tmp/odoo-mcp-emb-files

comm -13 /tmp/odoo-mcp-emb-files /tmp/odoo-mcp-canon-files | while read -r relpath; do
    if $DRY_RUN; then
        echo "DELETE $relpath"
    else
        rm -f "$CANONICAL_DIR/$relpath"
        echo "DELETE $relpath"
    fi
done

rm -f /tmp/odoo-mcp-canon-files /tmp/odoo-mcp-emb-files

if $DRY_RUN; then
    echo ""
    echo "Dry-run mode: no changes written."
else
    echo ""
    echo "Sync complete. Review with: git -C $CANONICAL_DIR diff"
fi

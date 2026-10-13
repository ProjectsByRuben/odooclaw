#!/usr/bin/env bash
# sync-odoo-mcp.sh — Regenera la copia embebida desde el repo canónico
#
# Uso: scripts/sync-odoo-mcp.sh [--dry-run]
#
# Este script copia el código fuente del servidor de herramientas desde
# el repo canónico (nicolasramos/odooclaw-mcp) a la copia embebida en
# odooclaw/workspace/skills/odoo-mcp/.
#
# La copia embebida es DERIVADA y NO debe editarse a mano.
# Si necesitas cambiar código del servidor de herramientas, edítalo
# en el repo canónico y ejecuta este script.

set -euo pipefail

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN=true
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
EMBEDDED_DIR="$REPO_ROOT/odooclaw/workspace/skills/odoo-mcp"
CANONICAL_DIR="${ODOO_MCP_CANONICAL_DIR:-/tmp/odooclaw-mcp}"

# Verificar que el repo canónico existe
if [ ! -d "$CANONICAL_DIR/.git" ]; then
    echo "ERROR: El repo canónico no se encuentra en $CANONICAL_DIR" >&2
    echo "Clónalo con: git clone https://github.com/nicolasramos/odooclaw-mcp.git $CANONICAL_DIR" >&2
    exit 1
fi

# Directorio de destino temporal (para comparar antes de sobrescribir)
SYNC_TMP="$REPO_ROOT/.tmp-sync-odoo-mcp"
mkdir -p "$SYNC_TMP"

echo "=== Sync odoo-mcp ==="
echo "Canónico:  $CANONICAL_DIR"
echo "Destino:   $EMBEDDED_DIR"
echo ""

# Copiar solo los archivos de código fuente (excluyendo .git, docs, CI, etc.)
# Lo que se sync: src/ + tests/ + requirements.txt
(cd "$CANONICAL_DIR" && find src tests requirements.txt -type f) | while read -r relpath; do
    src_file="$CANONICAL_DIR/$relpath"
    dst_file="$EMBEDDED_DIR/$relpath"

    if [ ! -f "$src_file" ]; then
        echo "SKIP (no existe en canónico): $relpath"
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

if $DRY_RUN; then
    echo ""
    echo "Modo dry-run: no se han escrito cambios."
else
    echo ""
    echo "Sync completado. Revisa los cambios con: git diff $EMBEDDED_DIR"
fi

# Limpiar temporal
rm -rf "$SYNC_TMP"

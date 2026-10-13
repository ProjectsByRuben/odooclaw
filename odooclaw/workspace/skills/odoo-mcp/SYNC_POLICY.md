# Política de sincronización: odoo-mcp

## Declaración de canonicidad

**Repo canónico**: `https://github.com/nicolasramos/odooclaw-mcp`  
**Copia derivada**: `odooclaw/workspace/skills/odoo-mcp/` (dentro de `nicolasramos/odooclaw`)

La copia embebida es DERIVADA. NO debe editarse a mano. Todas las modificaciones
al servidor de herramientas se realizan en el repo canónico y se propagan a la
copia embebida mediante el script de sync.

## Cómo regenerar la copia derivada

```bash
# Clonar el repo canónico (una vez)
git clone https://github.com/nicolasramos/odooclaw-mcp.git /tmp/odooclaw-mcp

# Ejecutar el sync (desde la raíz del repo odooclaw)
./scripts/sync-odoo-mcp.sh

# O modo dry-run para ver qué cambiaría sin escribir
./scripts/sync-odoo-mcp.sh --dry-run
```

El script copia los siguientes paths del canónico → embebido:
- `src/odoo_mcp/**` — todo el código fuente
- `tests/**` — todos los tests
- `requirements.txt` — dependencias

## Protección CI

El workflow `.github/workflows/odoo-mcp-sync.yml` se ejecuta:
- En cada push/PR que modifique archivos embebidos
- Diariamente a las 06:00 UTC (lunes)

Si detecta divergencia entre ambos repos, **falla la build**.

## Archivos no sincronizados

Los siguientes archivos existen solo en la copia embebida y NO se sync:
- `FIXES_SUMMARY.md` — historial de fixes locales
- `QA_RUNBOOK.md` — runbook de QA específico del contexto embebido
- `TOOL_INVENTORY.md` — inventario generado (fuente de referencia)

Estos archivos son específicos del contexto de uso dentro de `odooclaw` y no
tienen análogo en el repo canónico.

## Si necesitas modificar el servidor de herramientas

1. Edita los archivos en `nicolasramos/odooclaw-mcp` (repo canónico)
2. Haz commit y push en ese repo
3. Ejecuta `./scripts/sync-odoo-mcp.sh` en `odooclaw`
4. Haz commit de los cambios sincronizados en `odooclaw`
5. Verifica que la CI pasa

## Historial de divergencia

Al 2026-10-13: 54 ficheros homónimos, 0 idénticos. La solución es declarar
el repo canónico como fuente de verdad y derivar la copia embebida.

## Excepciones

Los siguientes archivos NO se comparan en la CI (difieren por contexto, no por error):
- `README.md` — la documentación contextual difiere entre repos (español vs inglés, enfoque embebido vs standalone)

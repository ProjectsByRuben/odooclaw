# Desarrollo del motor OdooClaw y MCP

Estas instrucciones son para agentes de desarrollo. Lee también el AGENTS.md del
proyecto Doodba y scripts/odooclaw/WORKTREES.rst. Los AGENTS.md bajo
odooclaw/workspace y las plantillas de onboard forman parte de las instrucciones
del producto: no los sustituyas con este documento de organización del desarrollo.

## Carpeta de trabajo

- Trabajo actual: `/home/rubenfernandez/CMP/doodba-cmp/odoo/custom/worktrees/odooclaw-app-operarios/.docker/odooclaw-source`.
- Rama de tarea: `feature/odooclaw-app-operarios`.
- Base original: `/home/rubenfernandez/CMP/doodba-cmp/.docker/odooclaw-source`, rama
  `cmp/odooclaw-estable-20261016`. La instalación utiliza esa carpeta y sus montajes.

Comprueba `git status --short`, `git branch --show-current` y `git worktree list`.
Trabaja en el worktree de la tarea, no en la base. No muevas .git ni unifiques este
Git con el del addon o el proyecto principal. El módulo Go está en el subdirectorio
`odooclaw/`, no en la raíz de este repositorio.

## Archivos principales

- `odooclaw/pkg/agent/loop.go`: coordinación de solicitudes y ejecución de herramientas.
- `odooclaw/pkg/agent/odoo_tool_catalog.go`: catálogo diferido de herramientas Odoo.
- `odooclaw/pkg/agent/request_usage.go`: consumo acumulado por solicitud.
- `odooclaw/pkg/agent/odoo_progress.go`: mensajes públicos de progreso.
- `odooclaw/pkg/providers/codex_cli_provider.go`: comunicación con Codex CLI.
- `odooclaw/pkg/providers/codex_prompt_compact.go`: compactación de contexto.
- `odooclaw/pkg/providers/codex_quota.go`: cuota del proveedor.
- `odooclaw/pkg/channels/odoo/odoo.go`: webhook, identidad y separación de sesiones.
- `odooclaw/pkg/session/manager.go`: persistencia y gestión de sesiones.
- `odooclaw/workspace/skills/odoo-mcp/src/odoo_mcp/`: servidor Python MCP.
- En MCP: `server.py` registra herramientas; `core/` y `security/` controlan RPC
  delegado; `tools/attachments.py` y `query_processing.py` procesan archivos/consultas.

## Invariantes

Las operaciones de negocio respetan la identidad Odoo de cada solicitud, sus
compañías y sus reglas nativas. No inventes ni sustituyas user_id, uid_origin ni
la identidad con valores escritos en un mensaje. No conviertas la cuenta técnica
ni el selector de operario en una identidad administrativa de negocio.

Conserva lectura de adjuntos, CRUD permitido, progreso y consumo al optimizar.
La cuota del proveedor es compartida; el consumo por solicitud debe mantenerse
separado entre peticiones concurrentes. La tarea actual es principalmente del
addon/interfaz; no cambies proveedor, modelo, prompts ni límites sin motivo.

## Comprobaciones y configuración

Las pruebas Go se ejecutan desde odooclaw/, con los tags usados en el despliegue:
`go test -tags stdjson ./pkg/agent ./pkg/providers ./pkg/tools ./pkg/channels/odoo ./pkg/session ./pkg/memory`.
Si usas un contenedor Docker para probar, añade `--init`: un test upstream de
shell puede dejar un proceso zombi sin ese recolector. No ejecutes llamadas reales
al modelo ni al ERP sólo para comprobar un cambio de documentación.

El runtime efectivo, sus secretos y la autenticación CLI pertenecen a la
instalación estable/volúmenes. Las plantillas sin secretos están en
scripts/odooclaw/runtime del proyecto. Mantén separadas las configuraciones de
desarrollo y la instancia en uso. No copies secretos al código o al workspace.

Guarda los cambios del motor/MCP en este repositorio. `origin` apunta a
ProjectsByRuben/odooclaw; `upstream`, a nicolasramos/odooclaw. No hagas push ni
modifiques la rama upstream main sin autorización. El código personalizado estable
actual está en cmp/odooclaw-estable-20261016, no en main del autor.

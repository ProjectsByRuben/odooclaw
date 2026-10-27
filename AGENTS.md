# Desarrollo del motor OdooClaw y MCP

Estas instrucciones son para agentes de desarrollo. Lee también el AGENTS.md del
proyecto Doodba y scripts/odooclaw/WORKTREES.rst. Los AGENTS.md bajo
odooclaw/workspace y las plantillas de onboard forman parte de las instrucciones
del producto: no los sustituyas con este documento de organización del desarrollo.

## Base consolidada

Modelos por conversación, contexto explícito del registro, archivos elegidos y
utilidades locales se han reunido en la rama `cmp/odooclaw-estable-20261016`.
La etiqueta histórica del 16 de octubre se conserva. No integrar `upstream/main`.
El desarrollo está en `odoo/custom/worktrees/odooclaw/desarrollo`, rama
`dev/local` de cada componente. Crear worktrees de tarea para cambios posteriores
si hace falta. Los cambios de un worktree no se despliegan automáticamente.

El runtime utiliza la base estable y una imagen con el binario consolidado.
No actualizar CLI/MCP ni autenticación como parte de una compilación de Go.
El catálogo de modelos no consume inferencia. Conservar opciones por solicitud,
no mutar modelo global ni config.toml compartido. Ver el PROGRESS.md de Doodba.

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
separado entre peticiones concurrentes. La aplicación propia está implementada en el
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

## Optimización conservadora añadida

odoo_session_questions.go reconoce y combina frases completas de saludo,
identidad, empresa, permisos generales y configuración del modelo.
odoo_session_reply.go genera respuestas con hechos verificados
a partir del session_info del servidor, comprobando UID y canal; modelo y
configuración se leen del model_settings explícito de la solicitud. Los ACL
incompletos vuelven al agente y la tabla aclara sus límites por registro. No hay inferencia
ni herramientas en esa ruta; mantiene historial y consumo real cero. Peticiones con trabajo de negocio, adjuntos o identidad ausente siguen por el
agente normal. No ampliar el
reconocedor sin pruebas que preserven peticiones de negocio. compactToolJSON
sólo cambia formato y escapes HTML JSON, sin decodificar/reconstruir documentos,
campos repetidos ni números. No se recortan historial, herramientas ni documentos.

Las combinaciones sólo se responden localmente si TODAS las cláusulas están
reconocidas y tienen datos completos. No ignorar acciones, calificadores,
solicitudes de formato, otros usuarios ni adjuntos. Ver PROGRESS.md y pruebas
de composición antes de ampliar frases. ACL por área son generales; no autorizan
registros concretos. Mantener historial y consumo exacto 0 sin proveedor.


## Utilidades sin inferencia (2026-10-21)

La base consolidada incorpora contexto-registro y modelos-chat juntos. Las utilidades de
hora/fecha, aritmética, porcentaje, unidades, agradecimiento y ayuda están en
odoo_utility_reply.go y sus pruebas. Se invocan tras validar identidad de sesión,
sin adjuntos, y sólo con coincidencia de la petición completa. No interceptar
trabajo de negocio ni referencias a registros. Horarios IANA con DST embebido,
fecha/hora calculadas en cada petición; zona de sesión suministrada por Odoo.
Países con múltiples husos requieren ciudad. El camino existente conserva historial,
telemetría real cero y respuestas. No cambiar proveedor, herramientas ni permisos.

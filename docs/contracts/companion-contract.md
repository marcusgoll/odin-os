# Companion Contract

Companions are durable role contracts scoped to a workspace. They are not provider-specific prompt bundles.

Each companion record stores:

- `workspace_id`
- `key`
- `title`
- `kind`
- `charter`
- `status`
- `initiative_scope_json`
- `tool_policy_json`
- `memory_policy_json`
- `planning_policy_json`

Workspace linkage:

- `workspaces.default_companion_key` points at the workspace's default companion key.
- `initiatives.owner_companion_id` points at the companion assigned to lead an initiative.

Task 3 keeps the contract intentionally thin:

- bootstrap or reuse a default operator companion per workspace
- list companions for a workspace
- assign a companion to an initiative
- treat workspace policy defaults and companion or initiative overlays as fail-closed governance inputs

# cfipros and marcusgoll Overlay Path

`cfipros` and `marcusgoll` are enrolled in `odin-os` so n8n workflow cutover can happen project by project instead of as a single legacy switch.

Current starting point:

- `runtime_owner: legacy_odin`
- `primary_controller: legacy_odin`
- transition intent: `shadow`
- no default mutation authority for `odin_os`
- branch and worktree safeguards mirror the `pbs` policy baseline

Expected transition path:

1. `shadow`
   Odin observes the project, records compare data, and creates no routine mutations.
2. `limited_action`
   Odin may run only explicit bounded actions on task-owned branches and worktrees.
3. `cutover`
   Odin becomes the primary controller for routine queued work after the shadow and limited-action exit criteria are met.

Operator notes:

- Keep legacy automation primary for both repositories until shadow reports are stable.
- Do not activate `cfipros` or `marcusgoll` n8n workflow cutover until their transition state has been moved deliberately out of legacy ownership.
- Advance one repository at a time so rollback boundaries stay obvious.

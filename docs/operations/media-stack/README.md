# Media Stack Playbooks

These playbooks define how Odin supervises a self-hosted media stack on top of the existing homelab substrate.

The media profile is bounded:

- Odin may observe and classify failures continuously.
- Odin may take only explicitly safe automatic actions.
- approval-required actions stay behind operator confirmation.
- forbidden actions remain outside automation entirely.

Each playbook follows the same structure:

- `Trigger`
- `Evidence`
- `Safe Actions`
- `Approval-Required Actions`
- `Rollback Trigger`
- `Closeout`

Use these playbooks together with `docs/contracts/media-stack-operations.md` and `docs/contracts/homelab-operations.md`.

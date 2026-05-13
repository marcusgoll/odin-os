---
title: Legacy systemd service disposition
status: active
date: 2026-05-09
---

# Legacy systemd service disposition

The canonical Odin OS user service is `odin-os.service`, configured through
`~/.config/odin/odin-os.env` and installed from `scripts/install-service.sh`.
The legacy `odin.service` and `odin.env` names remain compatibility assets only.
They are not the operator-facing service name for new deployment docs, runbooks,
or proofs.

No live service should be changed from this document alone. Migration from the
legacy unit to the canonical unit requires an explicit operator approval on the
target host.

## Asset disposition

| Path | Disposition | Reason |
| --- | --- | --- |
| `deploy/systemd/odin-os.service` | Canonical service unit. Keep. | Hardened user-service path and current deployment authority. |
| `deploy/systemd/odin-os.env.example` | Canonical env template. Keep. | New machine-local env files should be derived from this template. |
| `scripts/install-service.sh` | Canonical installer. Keep. | Installs `odin-os.service` and `odin-os.env`; supports dry-run and no-start install. |
| `scripts/start.sh`, `scripts/stop.sh`, `scripts/healthcheck.sh` | Canonical service helpers. Keep. | Default to `odin-os.service` and `odin-os.env` while allowing explicit env overrides. |
| `deploy/systemd/odin.service` | Legacy compatibility unit. Retain for now, remove later. | Existing operators may still need to inspect or migrate old installs. Do not use for new deployment. |
| `deploy/systemd/odin.env.example` | Legacy compatibility env template. Retain for now, remove later. | Existing legacy installs may need a source template during migration. New env files should use `odin-os.env.example`. |
| `scripts/dev/install-systemd-service.sh` | Legacy compatibility installer. Retain for now, replace or delete later. | It installs and starts `odin.service`; changing it could surprise existing legacy users. New installs must use `scripts/install-service.sh`. |

## Reference inventory

| Reference | Current role | Disposition |
| --- | --- | --- |
| `docs/DEPLOYMENT.md` | Deployment authority. | Names `odin-os.service` as canonical and links to this disposition. |
| `docs/operations/cutover-readiness.md` | Active operator checklist. | Must use `odin-os.service` and `odin-os.env.example`. |
| `docs/operations/always-on-cutover-checklist.md` | Active operator checklist. | Must use `odin-os.service`, `odin-os.env.example`, and `systemctl --user ... odin-os.service`. |
| `docs/contracts/homelab-operations.md` | Active operations contract. | Must list canonical service artifacts first; legacy artifacts are compatibility-only. |
| `docs/operations/marcus-live-x-post-runbook.md` | Active social operator runbook. | Must source `~/.config/odin/odin-os.env`; legacy env naming is not a new-runbook default. |
| `CONTEXT.md` | Durable domain decisions. | Must record that `odin-os.service` and `odin-os.env` are canonical after migration. Historical notes may mention the former `odin.env` choice only as superseded context. |
| `docs/security/BROWNFIELD_SECURITY_REVIEW.md` | Security review snapshot with follow-up findings. | Keep the finding, but point the recommendation at this disposition and the canonical install path. |
| `docs/brownfield/RISK_REGISTER.md` | Brownfield risk snapshot. | Update active risk language to say the legacy path is compatibility-only and migration goes through the canonical unit. |
| `docs/brownfield/COMPONENT_INVENTORY.md` | Historical component inventory. | Leave historical references unless the inventory is refreshed wholesale. |
| `docs/brownfield/AUDIT.md` | Historical audit. | Leave historical references as evidence of the old deployment state. |
| `docs/brownfield/ARCHITECTURE_GAP_ANALYSIS.md` | Historical gap analysis. | Leave historical references; the active disposition is this document. |
| `docs/brownfield/CURRENT_TO_TARGET_MAP.md` | Historical target mapping. | Leave historical references unless the brownfield map is refreshed wholesale. |
| `docs/brownfield/MIGRATION_PLAN.md` | Historical migration plan. | Leave historical references as previous plan evidence. |
| `docs/audits/phase-gap-matrix.md` | Historical audit matrix. | Leave historical references as captured audit findings. |
| `docs/audits/phase-16-reality-audit.md` | Historical audit. | Leave historical references as captured audit findings. |
| `docs/audits/2026-04-24-legacy-shim-tmux-model-runtime-audit.md` | Historical live-state audit. | Leave historical references to the failed root `odin.service` as evidence. |
| `docs/audits/2026-04-24-odin-orchestrator-capability-parity.md` | Historical parity audit. | Leave historical references to the failed root `odin.service` as evidence. |
| `docs/plans/2026-04-22-marcus-x-first-live-post-plan.md` and related 2026-04-22 plan variants | Historical planning artifacts. | Leave as historical plan records; do not use them as current deployment instructions. |

## Migration rule

For a host that still runs `odin.service`:

1. Inventory the live unit and env file with read-only `systemctl --user cat`
   and filesystem inspection.
2. Copy the required non-secret-compatible values from `~/.config/odin/odin.env`
   into `~/.config/odin/odin-os.env`.
3. Install the canonical unit with `scripts/install-service.sh --dry-run` first,
   then `scripts/install-service.sh` only after operator approval.
4. Start or switch services only after explicit human approval for that host.
5. Verify the canonical runtime with `systemctl --user status odin-os.service`
   and the real `odin` command path.

Do not alias `odin.service` to `odin-os.service` automatically. A silent alias
would obscure which unit owns the live daemon and could leave old env-file
assumptions in place.

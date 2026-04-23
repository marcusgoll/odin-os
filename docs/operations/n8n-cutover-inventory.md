# Live n8n Odin Cutover Inventory

The live n8n container is healthy on `127.0.0.1:5678`.

This inventory captures the active legacy Odin-facing workflows currently visible in the live export. The current live set contains `dispatch_envelope` and `legacy_helper` traffic, and no active `legacy_script` workflow has been confirmed yet.

| ID | Workflow | Class |
| --- | --- | --- |
| `oi13ZPX3Egb4fd5y` | `Marcusgoll CI Alert` | `dispatch_envelope` |
| `4KjvoOA1MmLSXVlg` | `Odin Core Update` | `dispatch_envelope` |
| `RcrEilzx1jPSK3Cm` | `Odin Performance Audit` | `dispatch_envelope` |
| `4zUxuXrUZzCUhYGi` | `Odin Sentry Alert` | `dispatch_envelope` |
| `b4dd67ed-d7cf-494f-8227-ba0d06c156ba` | `Odin Task Dispatch` | `dispatch_envelope` |
| `a85c8ef0-f08b-4cd2-a3fc-f537fa2331b9` | `Odin Telegram Bot` | `legacy_helper` |
| `oEg3mbj6YTAKDhhh` | `PBS CI Alert` | `legacy_helper` |
| `rRwuzKGfOlKV6Gck` | `PBS GitHub Alert` | `legacy_helper` |
| `MvNzObXScU8lXc1r` | `Uptime Kuma Telegram Alerts` | `legacy_helper` |

Observed transport patterns:

- `dispatch_envelope`: workflows that base64-decode task envelopes and forward them through SSH ingress to `orchestrator@172.17.0.1`
- `legacy_helper`: workflows that call helper verbs such as `dedup-check` and `nonce-update`
- `legacy_script`: supported by the exporter for `/var/odin` shell-script paths, but not yet present in the confirmed active set

Notes:

- These workflows use legacy SSH ingress with `/home/node/.ssh/odin_ingress` and `orchestrator@172.17.0.1`.
- Some workflows push base64-decoded task envelopes into legacy Odin.
- Some workflows call helper verbs like `dedup-check` and `nonce-update`.
- The exporter should keep supporting `legacy_script` if an active workflow directly executes `/home/orchestrator/odin-orchestrator` shell scripts in a later export.

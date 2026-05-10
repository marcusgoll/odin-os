# External Intake Contract

`odin-os` accepts normalized external task intake through `odin intake enqueue`.

The normalized payload shape is:

```json
{
  "schema_version": 1,
  "source": "n8n",
  "type": "ci_failure",
  "project_key": "pbs",
  "title": "Investigate PBS CI failure",
  "action_key": "",
  "dedup_key": "ci_failure:pbs:1234",
  "requested_by": "n8n",
  "payload": {}
}
```

Field rules:

- `schema_version` is fixed at `1` for the first normalized intake contract.
- `source` is required and identifies the upstream system, such as `n8n`.
- `type` is required and names the intake class, such as `ci_failure`.
- `project_key` is required and must resolve to a registered project.
- `title` is required and becomes the human-readable task title.
- `action_key` is optional and is reserved for limited-action intake lanes.
- `dedup_key` is optional but should be populated whenever the source can provide a stable replay-safe key.
- `requested_by` defaults to `source` when the caller does not supply a more specific actor.
- `payload` is a required JSON object in the normalized intake envelope.

## Universal Source Envelope

External sources normalize into `source_family`, `external_object_id`, `event_kind`, `observed_at`, `subject`, `body` or `summary`, `actor`, `source_uri`, `evidence_refs`, and namespaced `adapter_facts`.

Source adapters normalize source facts. Odin core owns `dedupe_key`, `dedupe_recipe_version`, lifecycle state, and promotion boundaries.

Raw intake processing may create a Reviewable Intake Proposal. It must not create executable Work Items, Run Attempts, dispatches, approvals, or external mutations by default.

CLI flags:

- `odin intake enqueue --source <source> --project <key> --title <title> --type <type>`
- Optional flags: `--action-key`, `--dedup-key`, `--requested-by`, `--payload-file <path|->`, `--json`

Validation rules:

- `--source`, `--project`, `--title`, and `--type` are required.
- `--dedup-key` must be free of whitespace and control characters.
- `--payload-file` may be `-` to indicate stdin will be consumed by a later stage.
- This parser validates file-backed `--payload-file` inputs only; stdin-backed payload validation happens in the intake execution stage.
- A file-backed `--payload-file` must contain a valid JSON object.

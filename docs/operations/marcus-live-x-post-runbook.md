# Marcus Live X Post Runbook

Use this runbook for the first Marcus-live X post loop only.

This runbook is intentionally narrow:

- one primary X post
- Marcus approves and publishes inside Odin
- LinkedIn stays manual
- replies stay suggestion-only
- one successful post ends the run

If a step says "stop", do not improvise a side path inside the same runbook.

## 1. Bootstrap the shell

From `/home/orchestrator/odin-os`:

```bash
cd /home/orchestrator/odin-os
set -a
. ~/.config/odin/odin-os.env
set +a
printf 'ODIN_ROOT=%s\n' "$ODIN_ROOT"
printf 'ODIN_HUGINN_VISUAL_DRIVER=%s\n' "$ODIN_HUGINN_VISUAL_DRIVER"
printf 'ODIN_HUGINN_X_PUBLISH_DRIVER=%s\n' "$ODIN_HUGINN_X_PUBLISH_DRIVER"
printf 'ODIN_HUGINN_X_POST_DRIVER=%s\n' "$ODIN_HUGINN_X_POST_DRIVER"
test -n "$ODIN_ROOT"
test -x "$ODIN_HUGINN_VISUAL_DRIVER"
test -x "$ODIN_HUGINN_X_PUBLISH_DRIVER"
test -x "$ODIN_HUGINN_X_POST_DRIVER"
```

Stop condition:
If any variable is empty or any driver path is not executable, stop and repair `~/.config/odin/odin-os.env` before continuing.

## 2. Run top-level preflight

```bash
cd /home/orchestrator/odin-os
./bin/odin healthcheck
./bin/odin doctor --json
```

Stop condition:
If either command fails or reports a blocker you have not consciously accepted, stop and repair the runtime before continuing.

## 3. Enter the Odin shell and reset session state

```bash
cd /home/orchestrator/odin-os
./bin/odin
/scope global
/project
/mode
/workflow clear
/skill clear
/workflow
/skill
```

Required readback:

- `/project` shows `current=none`
- `/mode` shows `mode=ask`
- `/workflow` shows `current=none`
- `/skill` shows `current=none`

## 4. Validate and select the social assets

```text
/workflow validate marcus-social-growth-workflow
/skill validate marcus-x-drafting-assistant
/workflow use marcus-social-growth-workflow
/skill use marcus-x-drafting-assistant
/workflow
/skill
```

Required readback:

- `/workflow validate ...` reports `workflow=marcus-social-growth-workflow status=ready`
- `/skill validate ...` reports `skill=marcus-x-drafting-assistant status=ready`
- `/workflow` shows `current=marcus-social-growth-workflow`
- `/skill` shows `current=marcus-x-drafting-assistant`

Stop condition:
If either asset is not ready, stop and repair the authored asset before continuing.

## 5. Run headed compose preflight

```text
/tool run browser_visual_audit target_url=https://x.com/compose/post label=x-compose-preflight headless=false
```

Required manual confirmation:

- returned `final_url` is `https://x.com/compose/post`
- the screenshot shows Marcus's logged-in X compose page

Stop condition:
If the compose page is not visibly correct, stop the loop, repair the X session manually outside Odin, `exit`, and restart from step 1 in a fresh shell session.

If the compose preflight lands on the X login page instead of a logged-in compose view, expose the trusted browser session and complete login there before rerunning step 5:

```bash
cd /home/orchestrator/odin-os
set -a
. ~/.config/odin/odin-os.env
set +a
scripts/ops/odin-trusted-browser-access.sh start
```

Expected readback:

- `status=ready`
- `chrome_profile=$ODIN_ROOT/browser-state/chrome-profile`
- `novnc_url=http://127.0.0.1:6080` when noVNC assets are available on this machine
- otherwise keep the reported localhost VNC surface and use a VNC client directly

The X login session is intentionally saved in that persistent Chrome profile. For X via Google OAuth, log in once in the trusted browser session and reuse the same `ODIN_ROOT` on later runs.

After Marcus finishes the X login in the trusted browser session:

```bash
cd /home/orchestrator/odin-os
set -a
. ~/.config/odin/odin-os.env
set +a
scripts/ops/odin-trusted-browser-access.sh stop
```

Then rerun step 5.

## 6. Ask for one primary draft

Send one plain ask-mode prompt after `/skill use marcus-x-drafting-assistant`:

```text
Draft one primary X post only.
Topic: <topic>
Audience: <audience>
Lesson or opinion: <real lesson or opinion>
Required facts: <facts Marcus has confirmed>
Style constraints: <tone and style constraints>
```

## 7. Review the persisted pending draft

```text
/memory list type=social_draft field.approval=pending
/memory show <draft-id>
```

Use the `memory=<draft-id>` from the list output as the canonical draft ID for `/memory show`.

## 8. Reject or approve the draft

If Marcus does not approve the draft:

```text
/memory resolve <draft-id> result=rejected reason=<kebab-case-tag>
```

Example:

```text
/memory resolve 12 result=rejected reason=too-generic
```

Then return to step 6 and ask for a fresh draft.

If Marcus approves the draft:

```text
/memory resolve <draft-id> result=approved
/memory list type=social_outcome field.result=approved field.channel=x field.content_kind=post
/memory show <outcome-id>
```

Use the `memory=<outcome-id>` from the approved-outcome list output as the canonical outcome ID.

## 9. Publish the approved outcome

```text
/memory publish <outcome-id> via=huginn_x
```

Stop condition:
If `/memory publish ... via=huginn_x` fails, do not rerun publish immediately.

Recovery rule:

- First use the trusted browser session or another read-only X page inspection to determine whether a live X URL already exists for the just-submitted content.
- If no live X URL exists, `exit`, start a fresh shell session, and restart from step 1. Reuse the same approved `social_outcome`.
- If a live X URL is recoverable, repair the same outcome row instead of resubmitting:

```text
/memory publish <outcome-id> url=<publish_url> published_at=<rfc3339>
```

Then continue to step 10 with the same `outcome-id`.

## 10. Prove the same outcome row became published

```text
/memory list type=social_outcome field.publish_status=published field.publish_mode=huginn_x field.channel=x field.content_kind=post
/memory show <same outcome-id>
```

Required readback:

- the filtered list output visibly includes the same canonical `outcome-id`
- `/memory show <same outcome-id>` confirms:
  - `publish_status=published`
  - `publish_mode=huginn_x`
  - exact `publish_url`
  - non-empty `published_at`
  - non-empty `publish_screenshot_path`

Stop condition:
If `/memory publish ... via=huginn_x` appeared to succeed but this durable published-outcome readback does not prove those fields on the same `outcome-id`, stop and treat that as an Odin defect. Do not rerun publish. If no live X URL can be proven, exit and report the defect. If a live X URL is proven, repair the same outcome row through `/memory publish <outcome-id> url=<publish_url> published_at=<rfc3339>` and then repeat step 10.

## 11. Capture visible-page evidence for the published post

Take `publish_url` from the shown published `social_outcome` and reuse the same `outcome-id` in the label:

```text
/tool run browser_x_post_visible_evidence target_url=<publish_url> label=social-outcome-<outcome-id>
```

If the evidence tool visibly fails after native publish succeeded:

- stay in the same REPL
- rerun step 11 with the same `publish_url`

## 12. Prove the newest matching evidence row

```text
/memory list type=social_evidence field.evidence_kind=x_post_visible field.label=social-outcome-<outcome-id> order=desc limit=1
/memory show <evidence-id>
```

Use the `memory=<evidence-id>` returned by the list output as the canonical evidence ID for `/memory show`.

Required readback:

- `label=social-outcome-<outcome-id>`
- `target_url=<publish_url>`
- `final_url=<publish_url>`
- non-empty `screenshot_path`
- non-empty `snapshot_path`
- non-empty `snapshot_excerpt`

Supporting only, not completion gates:

- `post_text`
- author metadata
- engagement counts

Stop condition:
If tool output looks successful but this durable evidence readback does not produce the expected row or fields, stop and treat that as an Odin defect. Leave the already published `social_outcome` intact. Do not rerun publish.

## 13. End the run cleanly

After one successful post:

```text
/workflow clear
/skill clear
/scope global
exit
```

This runbook ends after one successful post.

If Marcus wants to publish another post, start a fresh session and restart from step 1.

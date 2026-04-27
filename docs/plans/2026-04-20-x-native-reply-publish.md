# X Native Reply Publish Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend the existing native X publish direction so `odin-os` can publish one approved X post and one approved X reply through `/memory publish <id> via=huginn_x`, with reply actions requiring an explicit `in_reply_to_url`.

**Architecture:** Reuse the existing `social_outcome` lifecycle and the planned single native X publish lane. Finish the post-capable native publish path, then extend the same publish command, tool lane, and driver contract to accept reply-mode metadata instead of adding a second engagement command family.

**Tech Stack:** Go, Bash, Node.js, jq, repo-local Huginn browser server, SQLite-backed memory state, real `odin` CLI verification.

---

### Task 1: Write the failing shell tests for reply-aware native publish validation

**Files:**
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/cli/commands/help.go` if usage text needs clarification

**Step 1: Write the failing tests**

Add shell tests that prove:

- `/memory publish 12 via=huginn_x` still parses successfully
- native X publish accepts `content_kind=reply` only when `in_reply_to_url` is present
- native X publish rejects `content_kind=reply` without `in_reply_to_url`
- native X publish rejects reply targets that are not valid X status URLs
- native X publish still rejects non-X channels

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli/repl -run 'TestShellMemoryPublish'
```

Expected: FAIL because reply-specific validation is not implemented yet.

**Step 3: Write minimal implementation**

Add the smallest publish-validation changes needed to support reply-mode native publish eligibility.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/cli/repl -run 'TestShellMemoryPublish'
```

Expected: PASS

### Task 2: Write the failing tests for the native X publish lane contract

**Files:**
- Create or modify: `internal/adapters/web/x_publish_driver.go`
- Create or modify: `internal/adapters/web/x_publish_driver_test.go`
- Modify: `internal/tools/invocation/service.go`
- Modify: `internal/tools/invocation/service_test.go`
- Modify: `internal/tools/catalog/builtin.go`
- Modify: `internal/tools/catalog/builtin_test.go`

**Step 1: Write the failing tests**

Add tests that expect the native X publish lane to support:

- one driver env var: `ODIN_HUGINN_X_PUBLISH_DRIVER`
- one tool key reused for native X publish
- `post_text` input
- `content_kind` input with `post` or `reply`
- `in_reply_to_url` input required in reply mode

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog
```

Expected: FAIL because the native X publish lane is incomplete or reply-blind.

**Step 3: Write minimal implementation**

Implement the native publish driver contract and tool wiring so one lane supports both post and reply mode.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog
```

Expected: PASS

### Task 3: Write the failing live driver script tests for post mode and reply mode

**Files:**
- Create or modify: `scripts/drivers/huginn-x-post-publish.sh`
- Modify: `tests/integration/live_driver_scripts_test.go`

**Step 1: Write the failing tests**

Add fixture-backed driver tests that prove:

- post mode opens compose, types the post, and returns a publish URL
- reply mode navigates to `in_reply_to_url`, finds the reply composer, types the reply, and returns a publish URL
- both modes return screenshot evidence
- reply mode fails when the target URL is missing

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./tests/integration -run 'TestHuginnXPostPublishDriverScript'
```

Expected: FAIL because the driver script does not yet implement both modes reliably.

**Step 3: Write minimal implementation**

Implement the native X publish script with:

- post mode via compose UI
- reply mode via explicit target post URL
- fail-closed behavior for missing compose/reply signals or unverifiable publish results

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./tests/integration -run 'TestHuginnXPostPublishDriverScript'
```

Expected: PASS

### Task 4: Write the failing CLI integration tests for true post-plus-reply publish

**Files:**
- Modify: `tests/integration/social_workflow_test.go`

**Step 1: Write the failing tests**

Add compiled-binary integration coverage that proves:

- an approved X post can be published through `/memory publish <id> via=huginn_x`
- an approved X reply with `in_reply_to_url` can be published through the same command
- both updated `social_outcome` records contain:
  - `publish_status=published`
  - `publish_mode=huginn_x`
  - `publish_url`
  - `published_at`
- the reply outcome preserves `in_reply_to_url`

Use fixture drivers through `ODIN_HUGINN_X_PUBLISH_DRIVER` so the tests stay deterministic.

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./tests/integration -run 'TestMarcusSocialNativeXPublishCLIIntegration|TestMarcusSocialNativeXReplyCLIIntegration'
```

Expected: FAIL because the real publish lane is not fully implemented for post-plus-reply mode.

**Step 3: Write minimal implementation**

Update `/memory publish ... via=huginn_x` so it calls the native publish lane, validates reply metadata, and persists the returned publish evidence back onto the same `social_outcome`.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./tests/integration -run 'TestMarcusSocialNativeXPublishCLIIntegration|TestMarcusSocialNativeXReplyCLIIntegration'
```

Expected: PASS

### Task 5: Verify the visible-evidence handoff for both published URLs

**Files:**
- Modify: `tests/integration/social_workflow_test.go`

**Step 1: Write the failing test**

Add or extend integration coverage proving that:

- the published post URL can feed `huginn_x_post_visible_evidence`
- the published reply URL can feed `huginn_x_post_visible_evidence`
- both produce compatible `social_evidence` records for the existing analytics path

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./tests/integration -run 'TestMarcusSocialXVisibleEvidenceCLIIntegration'
```

Expected: FAIL only if the native-publish output is incompatible with the current evidence handoff.

**Step 3: Write minimal implementation**

Adjust publish-field persistence or evidence expectations only as needed to keep the handoff clean.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./tests/integration -run 'TestMarcusSocialXVisibleEvidenceCLIIntegration'
```

Expected: PASS

### Task 6: Update the contracts and live-driver docs

**Files:**
- Modify: `docs/contracts/marcus-social-copilot.md`
- Modify: `docs/contracts/live-driver-tools.md`
- Modify: `memory/users/marcus-social-copilot.md` if the operator guidance needs reply-target memory rules

**Step 1: Keep verification green before doc edits**

Run:

```bash
go test ./internal/cli/repl ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog -run 'Test'
go test ./tests/integration -run 'TestHuginnXPostPublishDriverScript|TestMarcusSocialNativeXPublishCLIIntegration|TestMarcusSocialNativeXReplyCLIIntegration|TestMarcusSocialXVisibleEvidenceCLIIntegration'
```

Expected: PASS

**Step 2: Write minimal documentation**

Update the live docs to say:

- native X publishing is available through `/memory publish <id> via=huginn_x`
- the same command supports approved X replies when `in_reply_to_url` is present
- LinkedIn remains manual
- all live actions remain human-approved and X-only

**Step 3: Re-run focused verification**

Run:

```bash
go test ./tests/integration -run 'TestHuginnXPostPublishDriverScript|TestMarcusSocialNativeXPublishCLIIntegration|TestMarcusSocialNativeXReplyCLIIntegration|TestMarcusSocialXVisibleEvidenceCLIIntegration'
```

Expected: PASS

### Task 7: Run focused verification and rebuild Odin

**Files:**
- Modify: none unless verification exposes another bug

**Step 1: Run focused suites**

Run:

```bash
go test ./internal/cli/repl -run 'TestShellMemoryPublish'
go test ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog
go test ./tests/integration -run 'TestHuginnXPostPublishDriverScript|TestMarcusSocialNativeXPublishCLIIntegration|TestMarcusSocialNativeXReplyCLIIntegration|TestMarcusSocialXVisibleEvidenceCLIIntegration'
bash -n scripts/drivers/huginn-x-post-publish.sh scripts/drivers/huginn-x-post-evidence.sh scripts/browser/browser-access.sh
node --check scripts/browser/odin-huginn-server.js
go build -o ./bin/odin ./cmd/odin
```

Expected: PASS

### Task 8: Verify the real Odin CLI path for one post and one reply

**Files:**
- Modify: none unless live verification exposes another bug

**Step 1: Run the real CLI flow with fixture drivers first**

Run:

```text
/workflow use marcus-social-growth-workflow
/skill use marcus-x-drafting-assistant
Draft one X post about stabilized approach discipline.
/memory resolve <draft_id> result=approved
/memory publish <post_outcome_id> via=huginn_x
/memory show <post_outcome_id>
```

Then run:

```text
/memory remember social_outcome result=approved channel=x content_kind=reply in_reply_to_url=https://x.com/example/status/123 -- Short, useful reply text.
/memory publish <reply_outcome_id> via=huginn_x
/memory show <reply_outcome_id>
```

Expected:

- the approved post outcome is updated in place with native publish fields
- the approved reply outcome is updated in place with native publish fields
- the reply outcome still contains `in_reply_to_url`

### Task 9: Verify the true live E2E flow when Marcus is ready

**Files:**
- Modify: none unless live verification exposes another bug

**Step 1: Run the real operator flow**

After fixture verification is green and Marcus is ready for a live run:

1. approve one real X post in Odin
2. publish it through `/memory publish <id> via=huginn_x`
3. capture visible evidence for the resulting post URL
4. approve one real X reply with `in_reply_to_url`
5. publish it through `/memory publish <id> via=huginn_x`
6. capture visible evidence for the resulting reply URL
7. run the Marcus analytics advisor over the resulting evidence

Expected:

- one real post action is represented by `social_outcome` plus `social_evidence`
- one real reply action is represented by `social_outcome` plus `social_evidence`
- the existing analytics path can reason over both without any new reporting abstraction

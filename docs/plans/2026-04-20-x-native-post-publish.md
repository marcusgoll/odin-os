# X Native Post Publish Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add X-only native posting through the existing `social_outcome` lifecycle so approved Marcus X posts can be published via Huginn using `/memory publish <id> via=huginn_x`.

**Architecture:** Extend the current `/memory publish` command instead of adding a new social command family. Add one new Huginn X publish driver lane, wire it into the builtin tool catalog, and let the existing `social_outcome` record remain the single source of truth for publish status and evidence.

**Tech Stack:** Go, Bash, Node.js, jq, repo-local Huginn browser server, SQLite-backed memory state, real `odin` CLI verification.

---

### Task 1: Write the failing shell tests for native X publish parsing and validation

**Files:**
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/cli/commands/help.go`

**Step 1: Write the failing test**

Add shell tests that cover:

- `/memory publish 12 via=huginn_x` parses successfully
- native mode rejects `social_outcome` entries with `channel=linkedin`
- native mode rejects non-`post` content kinds
- help text includes the new `via=huginn_x` form

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli/repl -run 'TestShellMemoryPublish'
```

Expected: FAIL because the parser and publish handler do not support `via=huginn_x`.

**Step 3: Write minimal implementation**

Add the smallest parser and validation changes needed to recognize `via=huginn_x`.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/cli/repl -run 'TestShellMemoryPublish'
```

Expected: PASS

### Task 2: Write the failing tool, adapter, and invocation tests for X native publish

**Files:**
- Create: `internal/adapters/web/x_publish_driver.go`
- Create: `internal/adapters/web/x_publish_driver_test.go`
- Modify: `internal/tools/invocation/service.go`
- Modify: `internal/tools/invocation/service_test.go`
- Modify: `internal/tools/catalog/builtin.go`
- Modify: `internal/tools/catalog/builtin_test.go`

**Step 1: Write the failing test**

Add tests that expect:

- a new driver env var `ODIN_HUGINN_X_PUBLISH_DRIVER`
- a new tool key `huginn_x_post_publish`
- invocation service wiring for native X publish
- builtin tool definition and required schema inputs

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog
```

Expected: FAIL because the X publish driver lane does not exist yet.

**Step 3: Write minimal implementation**

Add the driver, invocation service method, and builtin tool wiring.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog
```

Expected: PASS

### Task 3: Write the failing live driver script test for Huginn X native publish

**Files:**
- Create: `scripts/drivers/huginn-x-post-publish.sh`
- Modify: `tests/integration/live_driver_scripts_test.go`

**Step 1: Write the failing test**

Add a fixture-backed driver script test that:

- stubs the browser helper library
- expects a trusted or headed browser start
- expects compose navigation
- expects type and click actions
- returns a completed driver response with `publish_url` and `screenshot_path`

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./tests/integration -run 'TestHuginnXPostPublishDriverScript'
```

Expected: FAIL because the script does not exist yet.

**Step 3: Write minimal implementation**

Implement the shell driver with the smallest reliable compose-and-submit flow.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./tests/integration -run 'TestHuginnXPostPublishDriverScript'
```

Expected: PASS

### Task 4: Write the failing CLI integration test for native publish through `/memory publish`

**Files:**
- Modify: `tests/integration/social_workflow_test.go`

**Step 1: Write the failing test**

Add a compiled-binary integration test that:

- drafts an X post
- resolves it to approved
- publishes it with `/memory publish <id> via=huginn_x`
- verifies the updated `social_outcome` contains `publish_status=published`, `publish_mode=huginn_x`, and `publish_url`

Use a fixture publish driver through `ODIN_HUGINN_X_PUBLISH_DRIVER` so the test is deterministic and side-effect free.

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./tests/integration -run 'TestMarcusSocialNativeXPublishCLIIntegration'
```

Expected: FAIL because `/memory publish ... via=huginn_x` is not implemented.

**Step 3: Write minimal implementation**

Update the shell publish flow to call the new driver lane in native mode and then persist the returned publish evidence onto the same `social_outcome`.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./tests/integration -run 'TestMarcusSocialNativeXPublishCLIIntegration'
```

Expected: PASS

### Task 5: Update the contract docs and live-driver docs

**Files:**
- Modify: `docs/contracts/marcus-social-copilot.md`
- Modify: `docs/contracts/live-driver-tools.md`

**Step 1: Keep verification green before doc edits**

Run:

```bash
go test ./internal/cli/repl ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog -run 'Test'
go test ./tests/integration -run 'TestHuginnXPostPublishDriverScript|TestMarcusSocialNativeXPublishCLIIntegration'
```

Expected: PASS

**Step 2: Write minimal documentation**

Update the live contract to say:

- X-only native posting is available through `/memory publish <id> via=huginn_x`
- LinkedIn remains manual
- approval is still mandatory

Update the live-driver contract with the new env var and driver script.

**Step 3: Re-run focused verification**

Run:

```bash
go test ./tests/integration -run 'TestHuginnXPostPublishDriverScript|TestMarcusSocialNativeXPublishCLIIntegration'
```

Expected: PASS

### Task 6: Run focused verification and build the binary

**Files:**
- Modify: none unless verification exposes another bug

**Step 1: Run focused test suites**

Run:

```bash
go test ./internal/cli/repl -run 'TestShellMemoryPublish'
go test ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog
go test ./tests/integration -run 'TestHuginnXPostPublishDriverScript|TestMarcusSocialNativeXPublishCLIIntegration|TestMarcusSocialXVisibleEvidenceCLIIntegration'
bash -n scripts/drivers/huginn-x-post-publish.sh scripts/drivers/huginn-x-post-evidence.sh scripts/browser/browser-access.sh
node --check scripts/browser/odin-huginn-server.js
go build -o ./bin/odin ./cmd/odin
```

Expected: PASS

### Task 7: Verify the real Odin CLI path

**Files:**
- Modify: none unless live verification exposes another bug

**Step 1: Run the real CLI flow with a fixture publish driver**

Run:

```bash
./bin/odin
```

Then in the shell:

```text
/workflow use marcus-social-growth-workflow
/skill use marcus-x-drafting-assistant
Draft one X post about stabilized approach discipline.
/memory resolve 1 result=approved
/memory publish 2 via=huginn_x
/memory show 2
```

Expected: the approved `social_outcome` is updated in place with:

- `publish_status=published`
- `publish_mode=huginn_x`
- `publish_url=<value>`
- `published_at=<timestamp>`

### Task 8: Verify the evidence handoff still works

**Files:**
- Modify: none unless verification exposes another bug

**Step 1: Feed the published URL into the existing X evidence path**

Run:

```text
/tool run huginn_x_post_visible_evidence target_url=<publish_url> label=marcus-native-post
```

Expected: a `social_evidence` memory record is created and remains compatible with the analytics prompt path.

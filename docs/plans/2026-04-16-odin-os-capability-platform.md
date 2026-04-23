# Odin OS Capability Platform Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a first-class capability platform for Odin OS so skills, agents, commands, workflows, and tools are loaded into a live registry, validated through explicit contracts, and exposed through one provider-neutral discovery and invocation surface.

**Architecture:** Add a persistent capability service in `internal/core/capabilities`, extend registry compilation into normalized versioned descriptors, make commands and workflows executable through shared engines, then expose those capabilities through a gateway consumed by CLI, HTTP, and future MCP/provider adapters. Keep SQLite, runtime events, project governance, and executor routing as the stable core.

**Tech Stack:** Go, existing Odin registry loader/parser/validator/compiler pipeline, SQLite-backed runtime state, HTTP handlers, CLI/REPL entrypoints, JSON Schema-style manifest metadata, structured runtime events.

---

### Task 1: Add the live capability snapshot service

**Files:**
- Create: `internal/core/capabilities/types.go`
- Create: `internal/core/capabilities/service.go`
- Create: `internal/core/capabilities/service_test.go`
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/app/bootstrap/bootstrap_test.go`

**Step 1: Write the failing service and bootstrap tests**

Add tests that require:

- `capabilities.Service` can be constructed from a compiled registry snapshot
- the service exposes active snapshot digest and diagnostics
- bootstrap retains the capability service on the returned app instead of discarding the snapshot

Use test names:

- `TestServiceExposesActiveSnapshot`
- `TestServiceRejectsNilSnapshot`
- `TestBootstrapRetainsCapabilityService`

**Step 2: Run the tests to verify failure**

Run: `go test ./internal/core/capabilities ./internal/app/bootstrap -count=1`

Expected: FAIL because `internal/core/capabilities` does not exist and bootstrap does not retain a live capability service.

**Step 3: Write the minimal implementation**

Create a service with explicit immutable snapshot ownership:

```go
type Snapshot struct {
    Digest       string
    Diagnostics  []registry.Diagnostic
    Capabilities map[string]Descriptor
}

type Service struct {
    active Snapshot
}

func NewService(snapshot Snapshot) (*Service, error)
func (s *Service) Active() Snapshot
```

Wire bootstrap to construct the service after registry compilation and store it on the app container.

**Step 4: Re-run the tests**

Run: `go test ./internal/core/capabilities ./internal/app/bootstrap -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/core/capabilities internal/app/bootstrap
git commit -m "feat: retain live capability snapshot"
```

### Task 2: Normalize the registry manifest contract

**Files:**
- Modify: `internal/registry/types.go`
- Modify: `internal/registry/parser/parse.go`
- Modify: `internal/registry/parser/parse_test.go`
- Modify: `internal/registry/validator/validate.go`
- Modify: `internal/registry/validator/validate_test.go`
- Modify: `internal/registry/compiler/compile.go`
- Modify: `docs/contracts/registry-format.md`
- Create: `internal/registry/testdata/normalized/skill-triage.md`
- Create: `internal/registry/testdata/normalized/command-project-status.md`
- Create: `internal/registry/testdata/normalized/workflow-project-status.md`

**Step 1: Write the failing parser and validator tests**

Add coverage for these required fields:

- `apiVersion`
- `kind`
- `name`
- `version`
- `availability`
- `permissions`
- `inputSchema`
- `outputSchema`
- `dependencies`
- `execution`
- `implementation`

Use test names:

- `TestParseNormalizedManifestFields`
- `TestValidateRequiresVersionedInvokableSchemas`
- `TestCompileProducesNormalizedDescriptors`

**Step 2: Run the registry tests to verify failure**

Run: `go test ./internal/registry/... -count=1`

Expected: FAIL because the parser, validator, and compiler do not understand the normalized manifest contract yet.

**Step 3: Write the minimal implementation**

Extend registry types so invokable capability kinds share explicit metadata:

```go
type Manifest struct {
    APIVersion   string
    Kind         string
    Name         string
    Version      string
    Availability Availability
    Permissions  []string
    InputSchema  SchemaRef
    OutputSchema SchemaRef
    Dependencies []DependencyRef
    Execution    ExecutionPolicy
    Implementation ImplementationRef
}
```

Require `inputSchema` and `outputSchema` for invokable kinds and reject missing version data.

**Step 4: Re-run the registry tests**

Run: `go test ./internal/registry/... -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/registry docs/contracts/registry-format.md
git commit -m "feat: normalize capability manifest schema"
```

### Task 3: Add atomic snapshot publication and reload

**Files:**
- Create: `internal/core/capabilities/reload.go`
- Create: `internal/core/capabilities/reload_test.go`
- Modify: `internal/registry/watcher/watcher.go`
- Modify: `internal/runtime/events/events.go`
- Modify: `internal/runtime/projections/observability_test.go`

**Step 1: Write the failing reload tests**

Add tests that prove:

- publishing a valid snapshot swaps the active snapshot atomically
- a rejected snapshot leaves the previous snapshot active
- snapshot publication emits `capability.snapshot_published`
- snapshot rejection emits `capability.snapshot_rejected`

Use test names:

- `TestPublishSwapsSnapshotAtomically`
- `TestPublishKeepsPreviousSnapshotOnFailure`
- `TestPublishEmitsCapabilitySnapshotEvents`

**Step 2: Run the reload tests to verify failure**

Run: `go test ./internal/core/capabilities ./internal/runtime/projections -count=1`

Expected: FAIL because reload semantics and events are not implemented.

**Step 3: Write the minimal implementation**

Add an explicit publish API:

```go
func (s *Service) Publish(next Snapshot) error
func (s *Service) Reload(ctx context.Context) (Snapshot, error)
```

Make watcher-triggered reload optional and fail-closed. The service should keep serving the last good snapshot when reload validation fails.

**Step 4: Re-run the reload tests**

Run: `go test ./internal/core/capabilities ./internal/runtime/projections -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/core/capabilities internal/registry/watcher internal/runtime/events internal/runtime/projections
git commit -m "feat: add atomic capability snapshot reload"
```

### Task 4: Add the provider-neutral capability gateway

**Files:**
- Create: `internal/core/capabilities/gateway.go`
- Create: `internal/core/capabilities/gateway_test.go`
- Modify: `internal/core/capabilities/types.go`
- Modify: `internal/runtime/runs/service.go`
- Modify: `internal/runtime/runs/service_test.go`

**Step 1: Write the failing gateway tests**

Add tests that require:

- `ListCapabilities` returns thin cards filtered by scope and kind
- `GetCapability` returns the expanded descriptor for an exact id and version
- `InvokeCapability` validates input before dispatch
- `GetRun` returns run status and artifacts through one envelope

Use test names:

- `TestGatewayListsCapabilities`
- `TestGatewayReturnsExpandedDescriptor`
- `TestGatewayRejectsInvalidInput`
- `TestGatewayReturnsRunEnvelope`

**Step 2: Run the gateway tests to verify failure**

Run: `go test ./internal/core/capabilities ./internal/runtime/runs -count=1`

Expected: FAIL because there is no gateway contract or invocation envelope.

**Step 3: Write the minimal implementation**

Define the canonical request/response shape:

```go
type InvokeRequest struct {
    RequestID      string
    CapabilityID   string
    CapabilityVersion string
    Scope          ScopeRef
    Caller         CallerRef
    Input          json.RawMessage
    Execution      ExecutionRequest
}

type InvokeResponse struct {
    RunID      string
    Status     string
    Output     json.RawMessage
    Artifacts  []Artifact
    Error      *RunError
}
```

Keep the gateway small:

- `ListCapabilities`
- `GetCapability`
- `InvokeCapability`
- `GetRun`
- `ResumeRun`
- `CancelRun`

**Step 4: Re-run the gateway tests**

Run: `go test ./internal/core/capabilities ./internal/runtime/runs -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/core/capabilities internal/runtime/runs
git commit -m "feat: add capability gateway contract"
```

### Task 5: Make commands executable through the registry

**Files:**
- Create: `internal/core/commands/service.go`
- Create: `internal/core/commands/service_test.go`
- Modify: `internal/cli/commands/commands.go`
- Modify: `internal/cli/commands/commands_test.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Create: `registry/commands/project.status.md`

**Step 1: Write the failing command-engine tests**

Add tests that require:

- a registry-backed command descriptor can be resolved by id
- CLI command execution delegates to the command service rather than hardcoded branch logic
- invalid command input is rejected before execution

Use test names:

- `TestCommandServiceResolvesRegistryCommand`
- `TestCLICommandDispatchUsesCommandService`
- `TestCommandServiceRejectsInvalidInput`

**Step 2: Run the command tests to verify failure**

Run: `go test ./internal/core/commands ./internal/cli/commands ./internal/cli/repl -count=1`

Expected: FAIL because there is no command engine and the CLI path is still hardcoded.

**Step 3: Write the minimal implementation**

Create a registry-backed command service:

```go
type Service struct {
    caps CapabilityGateway
    workflows WorkflowRunner
}

func (s *Service) Execute(ctx context.Context, req capabilities.InvokeRequest) (capabilities.InvokeResponse, error)
```

Start by routing one real command, `project.status`, through the new service while keeping minimal bootstrap fallbacks for commands required before the registry is ready.

**Step 4: Re-run the command tests**

Run: `go test ./internal/core/commands ./internal/cli/commands ./internal/cli/repl -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/core/commands internal/cli/commands internal/cli/repl registry/commands/project.status.md
git commit -m "feat: execute commands through capability registry"
```

### Task 6: Make workflows executable and reference-checked

**Files:**
- Create: `internal/core/workflows/service.go`
- Create: `internal/core/workflows/service_test.go`
- Modify: `internal/tools/broker/broker.go`
- Modify: `internal/tools/broker/broker_test.go`
- Create: `registry/workflows/project-status.md`

**Step 1: Write the failing workflow tests**

Add tests that require:

- workflows can reference commands, agents, skills, and tools by id plus version range
- missing dependencies are rejected at compile or execute time with a structured dependency error
- the broker expands workflow context from the normalized registry snapshot instead of bespoke registry handling

Use test names:

- `TestWorkflowServiceResolvesCapabilityDependencies`
- `TestWorkflowServiceRejectsMissingDependency`
- `TestBrokerUsesNormalizedCapabilitySnapshot`

**Step 2: Run the workflow tests to verify failure**

Run: `go test ./internal/core/workflows ./internal/tools/broker -count=1`

Expected: FAIL because workflows are not executable and the broker is not registry-snapshot driven.

**Step 3: Write the minimal implementation**

Support a small first workflow shape:

```go
type Step struct {
    Capability string
    VersionRange string
    With map[string]any
}
```

Only implement sequential workflow execution first. Keep DAG or branching behavior out of scope until the sequential path is stable.

**Step 4: Re-run the workflow tests**

Run: `go test ./internal/core/workflows ./internal/tools/broker -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/core/workflows internal/tools/broker registry/workflows/project-status.md
git commit -m "feat: execute workflows from capability registry"
```

### Task 7: Add centralized permissions and schema validation

**Files:**
- Create: `internal/core/capabilities/validation.go`
- Create: `internal/core/capabilities/validation_test.go`
- Modify: `internal/core/policy/service.go`
- Modify: `internal/core/policy/service_test.go`
- Modify: `internal/core/capabilities/gateway.go`

**Step 1: Write the failing validation and policy tests**

Add tests that require:

- invokable capabilities without `inputSchema` or `outputSchema` are rejected before execution
- denied permissions fail with a policy error, not a generic execution error
- scope restrictions such as `project` versus `global` are enforced consistently

Use test names:

- `TestValidateInvocationInputAgainstSchema`
- `TestGatewayReturnsPolicyErrorForDeniedPermission`
- `TestGatewayRejectsInvalidScopeForCapability`

**Step 2: Run the validation and policy tests to verify failure**

Run: `go test ./internal/core/capabilities ./internal/core/policy -count=1`

Expected: FAIL because schema and permission checks are not centralized in the gateway.

**Step 3: Write the minimal implementation**

Add a validation stage before dispatch:

```go
func ValidateInvocation(desc Descriptor, req InvokeRequest) error
func AuthorizeInvocation(ctx context.Context, desc Descriptor, scope ScopeRef, caller CallerRef) error
```

Use structured error codes such as `validation_failed`, `permission_denied`, and `invalid_scope`.

**Step 4: Re-run the validation and policy tests**

Run: `go test ./internal/core/capabilities ./internal/core/policy -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/core/capabilities internal/core/policy
git commit -m "feat: centralize capability validation and authorization"
```

### Task 8: Add timeout, retry, cancel, and resume hooks to capability runs

**Files:**
- Create: `internal/runtime/supervision/service.go`
- Create: `internal/runtime/supervision/service_test.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/checkpoints/service.go`
- Modify: `internal/runtime/recovery/startup.go`
- Modify: `internal/runtime/recovery/startup_test.go`

**Step 1: Write the failing supervision tests**

Add tests that require:

- per-invocation timeout is enforced
- transient failures can retry up to the declared limit
- cancelled runs enter an explicit `cancelled` terminal state
- startup recovery resumes from checkpoint metadata instead of reconstructing work from the task title alone

Use test names:

- `TestSupervisorEnforcesTimeout`
- `TestSupervisorRetriesTransientFailure`
- `TestSupervisorCancelsRun`
- `TestStartupRecoveryResumesCheckpointedRun`

**Step 2: Run the supervision tests to verify failure**

Run: `go test ./internal/runtime/supervision ./internal/runtime/jobs ./internal/runtime/recovery -count=1`

Expected: FAIL because there is no run supervisor and recovery does not resume from a typed invocation envelope.

**Step 3: Write the minimal implementation**

Add a small supervisor API:

```go
type Supervisor interface {
    Run(ctx context.Context, req capabilities.InvokeRequest, fn AttemptFunc) (capabilities.InvokeResponse, error)
}
```

Persist attempt state and checkpoint metadata using the run id so `ResumeRun` can recover the original invocation context.

**Step 4: Re-run the supervision tests**

Run: `go test ./internal/runtime/supervision ./internal/runtime/jobs ./internal/runtime/recovery -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/supervision internal/runtime/jobs internal/runtime/checkpoints internal/runtime/recovery
git commit -m "feat: supervise capability execution lifecycle"
```

### Task 9: Expose discovery and invocation through CLI and HTTP

**Files:**
- Modify: `internal/cli/commands/commands.go`
- Modify: `internal/cli/repl/session.go`
- Modify: `internal/cli/repl/shell.go`
- Create: `internal/api/http/capabilities.go`
- Create: `internal/api/http/capabilities_test.go`
- Modify: `internal/app/lifecycle/run.go`

**Step 1: Write the failing transport tests**

Add tests that require:

- CLI can list capabilities filtered by scope
- CLI can invoke a registry-backed command through the gateway
- HTTP exposes discovery and invocation endpoints
- transport responses preserve run id, status, and structured error codes

Use test names:

- `TestCLIListsCapabilities`
- `TestCLIInvokesCapabilityThroughGateway`
- `TestHTTPCapabilityEndpoints`

**Step 2: Run the transport tests to verify failure**

Run: `go test ./internal/cli/... ./internal/api/http -count=1`

Expected: FAIL because there are no capability transport endpoints yet.

**Step 3: Write the minimal implementation**

Add these HTTP routes first:

- `GET /capabilities`
- `GET /capabilities/{id}`
- `POST /capabilities/{id}:invoke`
- `GET /runs/{run_id}`

Make CLI/REPL discovery use the same gateway methods instead of parallel custom registry traversal.

**Step 4: Re-run the transport tests**

Run: `go test ./internal/cli/... ./internal/api/http -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli internal/api/http internal/app/lifecycle
git commit -m "feat: expose capability gateway through cli and http"
```

### Task 10: Add MCP and provider-edge adapter coverage

**Files:**
- Create: `internal/transports/mcp/server.go`
- Create: `internal/transports/mcp/server_test.go`
- Create: `internal/executors/codex/bridge.go`
- Create: `internal/executors/claude_code/bridge.go`
- Modify: `internal/executors/codex/adapter.go`
- Modify: `internal/executors/claude_code/adapter.go`
- Create: `tests/integration/capability_gateway_interop_test.go`
- Modify: `docs/contracts/executor-routing.md`

**Step 1: Write the failing interoperability tests**

Add tests that require:

- the MCP surface lists Odin capabilities as typed tools
- Codex and Claude provider bridges both translate to the same `InvokeRequest`
- provider-specific formatting differences do not change Odin capability ids, permission semantics, or result envelopes

Use test names:

- `TestMCPListsCapabilitiesAsTools`
- `TestCodexBridgeBuildsCanonicalInvokeRequest`
- `TestClaudeBridgeBuildsCanonicalInvokeRequest`
- `TestProviderBridgesPreserveCanonicalResultEnvelope`

**Step 2: Run the interoperability tests to verify failure**

Run: `go test ./internal/transports/mcp ./internal/executors/codex ./internal/executors/claude_code ./tests/integration -count=1`

Expected: FAIL because there is no MCP surface and provider bridges are not defined.

**Step 3: Write the minimal implementation**

Keep provider bridges thin:

```go
type Bridge interface {
    ToInvokeRequest(providerCall ProviderCall) (capabilities.InvokeRequest, error)
    FromInvokeResponse(resp capabilities.InvokeResponse) (ProviderResult, error)
}
```

Do not move provider-specific prompt shaping into manifests or the capability gateway.

**Step 4: Re-run the interoperability tests**

Run: `go test ./internal/transports/mcp ./internal/executors/codex ./internal/executors/claude_code ./tests/integration -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/transports/mcp internal/executors/codex internal/executors/claude_code tests/integration docs/contracts/executor-routing.md
git commit -m "feat: add provider-neutral capability interoperability"
```

### Task 11: Remove legacy hardcoded paths and finish docs

**Files:**
- Modify: `internal/tools/catalog/builtin.go`
- Modify: `internal/executors/router/catalog.go`
- Modify: `config/models.yaml`
- Modify: `config/telemetry.yaml`
- Create: `docs/contracts/capability-gateway.md`
- Create: `docs/operations/capability-reload.md`

**Step 1: Write the failing cleanup tests**

Add tests that require:

- built-in tool and executor catalogs use manifest-backed or gateway-backed registration where intended
- stale config keys fail validation
- capability reload documentation matches the implemented runtime commands and HTTP routes

Use test names:

- `TestBuiltinCatalogUsesCapabilityRegistryForDynamicEntries`
- `TestExecutorCatalogRejectsStaleConfig`
- `TestCapabilityReloadDocsMatchRuntimeSurface`

**Step 2: Run the cleanup tests to verify failure**

Run: `go test ./internal/tools/catalog ./internal/executors/router ./internal/app/config -count=1`

Expected: FAIL because legacy hardcoded registration and unused config surfaces still exist.

**Step 3: Write the minimal implementation**

Remove or explicitly mark bootstrap-only:

- hardcoded dynamic command registration
- hardcoded dynamic tool registration
- dead config surfaces that are documented but not enforced

Document:

- manifest contract
- gateway contract
- reload lifecycle
- operator expectations for rejected snapshots

**Step 4: Re-run the cleanup tests**

Run: `go test ./internal/tools/catalog ./internal/executors/router ./internal/app/config -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/tools/catalog internal/executors/router internal/app/config config docs/contracts/capability-gateway.md docs/operations/capability-reload.md
git commit -m "chore: remove legacy capability wiring drift"
```

### Task 12: Run the full verification suite

**Files:**
- No code changes required
- Verify: `docs/plans/2026-04-16-odin-os-capability-platform-design.md`
- Verify: `docs/plans/2026-04-16-odin-os-capability-platform.md`

**Step 1: Run targeted package tests**

Run:

```bash
go test ./internal/core/capabilities ./internal/core/commands ./internal/core/workflows ./internal/registry/... ./internal/runtime/... -count=1
```

Expected: PASS.

**Step 2: Run transport and adapter tests**

Run:

```bash
go test ./internal/cli/... ./internal/api/http ./internal/transports/mcp ./internal/executors/... -count=1
```

Expected: PASS.

**Step 3: Run integration coverage**

Run:

```bash
go test ./tests/integration -count=1
```

Expected: PASS.

**Step 4: Run the full suite**

Run:

```bash
go test ./... -count=1
```

Expected: PASS. If an unrelated existing integration failure remains, capture it explicitly in the final handoff instead of hand-waving it away.

**Step 5: Commit**

```bash
git status --short
git add docs/plans/2026-04-16-odin-os-capability-platform.md
git commit -m "docs: add capability platform implementation plan"
```

---
title: Executor Contract
status: active
date: 2026-04-09
phase: "06"
---

# Executor Contract

The executor layer provides one portable task contract across harness-driver-backed headless CLIs, direct APIs, and broker routes.

## Executor classes

- `plan_backed_cli`
- `api_executor`
- `broker_executor`

In the current alpha cutover, `plan_backed_cli` means a durable headless lane that delegates to an external harness driver such as Codex or Claude Code. Odin prepares the `TaskSpec`, selects the route, and records runtime state; the harness driver owns the interactive agent session.

## Portable task spec

The executor contract accepts a strongly typed `TaskSpec` rather than provider-specific payloads.

Required intent fields:

- task id
- task kind
- scope
- prompt
- metadata
- budget hints
- tool policy
- capability requirements

Harness-driver executors receive that task spec as structured input on stdin and must return structured status, output, and external id data on stdout.

## Required methods

- `Health`
- `Capabilities`
- `RunTask`
- `ResumeTask`
- `CancelTask`
- `EstimateCost`

## Capability requirements

Capability matching is explicit and portable.

Requirements may express:

- allowed executor classes
- resume support
- cancel support
- tool support
- cost estimate support
- headless plan support
- broker fallback support

## Harness-driver rules

- headless CLI executors are unavailable until their driver command environment variable is configured
- route selection may prefer headless lanes, but runtime execution must fail explicitly when no configured headless driver satisfies the route
- API and broker executors remain distinct classes; they are not substitutes for a required harness-driver lane

## Worker environment allowlist

Worker and harness-driver subprocesses must build their environment through
`internal/executors/drivers.AllowlistedEnvironment`. They must not inherit
`os.Environ()` directly.

To add a safe worker environment variable:

1. Add the exact non-secret key to the allowlist in `internal/executors/drivers`.
2. Add or update a regression test proving sensitive keys such as
   `GITHUB_TOKEN`, `OPENAI_API_KEY`, and `ODIN_ADMIN_TOKEN` are still excluded.
3. Document why the key is required for the worker lane.

Do not allow broad prefixes such as `ODIN_*`, and do not allow key names that
contain token, secret, password, API key, access key, private key, or credential
markers. Runtime IDs, workspace paths, sandbox mode, fixture responses, and
temporary paths are acceptable only when they are non-secret and lane-specific.

## Browser read-only foundation

`internal/executors/browser` defines the read-only browser task boundary for goal evidence collection. The foundation validates goal ID, objective, allowed domains, start URLs, page and duration limits, and read-only action names before it records `browser_readonly` goal evidence through SQLite.

The default adapter is `internal/adapters/huginnbrowser.StubAdapter`, which returns deterministic `stub_local` read-only evidence and explicitly records `no_live_browser_launched` in its action log. This is not a live Huginn launch path yet. It must not submit forms, log in, click mutation controls, post, purchase, message, delete, or change external account state. Until a reviewed live adapter is added, browser goal work remains a validated evidence-request surface and uses the existing `goal.evidence_recorded` audit event as its durable trail.

`ODIN_BROWSER_ADAPTER=live` selects `internal/adapters/huginnbrowser.LiveAdapter`. The live adapter has a bounded local process boundary configured by `ODIN_HUGINN_BROWSER_COMMAND` or an injected command in tests, and the selected command must also appear in `ODIN_HUGINN_BROWSER_ALLOWED_COMMANDS` or the injected allowlist before it can execute. It sends the read-only request as JSON on stdin, enforces a timeout, captures stdout/stderr, and records structured `failed`, `timeout`, `not_implemented`, or parsed JSON responses as goal evidence. Non-empty live JSON must include non-empty `status` and `adapter_kind` fields, `visited_urls` must be a list when present, and mutation-looking fields such as form submissions, posted messages, sessions, purchases, deletes, or external mutations are rejected as invalid response contracts. It must not be treated as a real Huginn integration until credentials, session policy, and mutation-denial tests are added in a separate reviewed slice.

### Huginn browser worker JSON contract

The repo-local MVP worker command is built as `bin/huginn-browser-worker`. It receives one JSON request on stdin. The stable request fields are:

```json
{
  "objective": "Collect public documentation",
  "mode": "fetch",
  "start_urls": ["https://example.com/docs"],
  "allowed_domains": ["example.com"],
  "max_pages": 2,
  "max_duration_seconds": 30,
  "evidence_required": true,
  "site_profiles": [
    {
      "domain": "example.com",
      "max_pages": 1,
      "min_delay_ms": 250,
      "max_duration_seconds": 10,
      "mode_allowed": "fetch"
    }
  ]
}
```

- `objective`: non-empty operator objective for read-only evidence collection.
- `mode`: optional worker mode; omitted or `fetch` uses bounded HTTP GET, and `browser` explicitly enables JavaScript-capable read-only Chromium mode.
- `start_urls`: non-empty list of absolute HTTP(S) URLs to inspect.
- `allowed_domains`: non-empty list of hostnames the worker may visit.
- `max_pages`: positive page limit.
- `max_duration_seconds`: positive runtime limit.
- `evidence_required`: whether the caller requires durable evidence for the task. In `browser` mode this requests a local screenshot artifact in addition to title/text evidence.
- `site_profiles`: optional per-domain constraints. Each profile may set `domain`, `max_pages`, `min_delay_ms`, `max_duration_seconds`, and `mode_allowed` (`fetch`, `browser`, or `both`). When a profile matches a requested URL, the worker applies the stricter page and duration limits, waits the configured per-page delay before collection, and denies modes the profile does not allow.

The worker writes exactly one JSON response to stdout. The stable response fields are:

```json
{
  "status": "completed",
  "adapter_kind": "huginn_live",
  "visited_urls": ["https://example.com/docs"],
  "page_results": [
    {
      "url": "https://example.com/docs",
      "status": "visited",
      "mode": "fetch",
      "title": "Example Docs",
      "summary": "Collected public documentation page."
    }
  ],
  "extracted_text_summary": "Collected public documentation pages for operator review.",
  "screenshots": ["artifact://huginn-browser/screenshots/example-docs.png"],
  "action_log": ["validated_read_only_request", "captured_read_only_evidence"],
  "error_code": "",
  "error_message": ""
}
```

- `status`: required non-empty status such as `completed`, `failed`, or `timeout`.
- `adapter_kind`: required non-empty adapter identity, currently `huginn_live`.
- `visited_urls`: optional list of visited URLs; when present it must be a JSON array.
- `page_results`: optional list with one diagnostic result per explicit URL. Each result includes `url`, `status` (`visited`, `skipped`, `failed`, or `limited`), `mode`, and optional `title`, `summary`, `error_code`, and `error_message`.
- `extracted_text_summary`: optional human-readable summary for goal evidence.
- `screenshots`: optional list of durable local screenshot artifact paths. Browser mode emits these only when `evidence_required` is true.
- `action_log`: optional ordered read-only action log.
- `error_code`: optional machine-readable failure code.
- `error_message`: optional operator-readable failure detail.

Fixture responses live under `internal/adapters/huginnbrowser/testdata/` and are validated by the adapter tests. Workers must not include mutation fields such as `submitted_forms`, `form_submissions`, `posted_messages`, `session_tokens`, `purchases`, `deleted_items`, or `external_mutations`.

The worker processes only the requested `start_urls`, in order, up to `max_pages`; it must not crawl discovered links. Each URL is checked against `allowed_domains` before any fetch or browser launch. If one requested page fails or is skipped but another page is collected, the worker returns `status: "partial"` with `error_code: "partial_failure"` and records page-level failure markers in `action_log`. `page_results` carries the exact per-URL outcome for successful visits, disallowed-domain skips, failed fetch/browser attempts, profile mode denials, and URLs limited by `max_pages`.

Site profiles never widen access. `allowed_domains` is enforced before site-profile policy is applied. Matching profiles record `site_profile_applied`; delay enforcement records `rate_limit_delay_applied`; profile mode denial records `site_profile_mode_denied` and does not launch the browser or fetch the page.

The `browser` worker mode is not default. It launches a fresh temporary Chromium profile for each allowed URL, waits for local page rendering through headless DOM dump, extracts read-only title/text evidence, optionally captures a screenshot under a local sanitized artifact path when `evidence_required` is true, and removes the temporary profile after the run. Browser mode must not click controls, submit forms, reuse cookies, attach login/session profiles, or mutate external state.

### Huginn browser artifact retention

Browser screenshots are local runtime artifacts, not external storage objects. The default artifact root is `ODIN_ROOT/artifacts/huginn-browser` when `ODIN_ROOT` is set, otherwise `.odin/huginn-browser`; tests may inject a narrower artifact root. Each screenshot has a sidecar metadata file at `<screenshot>.metadata.json` with:

- `created_at`: UTC timestamp when the worker recorded the artifact.
- `goal_id`: optional goal ID when the request came through an Odin goal-backed browser task.
- `source_url`: URL rendered for the screenshot.
- `evidence_type`: always `screenshot`.
- `screenshot_path`: absolute local screenshot path.

Cleanup is explicit and local-only through the Huginn browser worker cleanup helper. It requires an `older_than` duration, defaults to dry-run unless the caller explicitly applies deletion, and only considers files under the Huginn browser `screenshots` artifact directory with screenshot or screenshot-metadata filenames. It must not delete non-Huginn artifacts or files outside the configured artifact root.

## Important rule

Subscription-backed CLIs, APIs, and broker routes share one executor contract but remain distinct by class and capability metadata. They are not interchangeable by default.

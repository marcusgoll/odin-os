---
title: Harness-First CLI Contract
status: active
date: 2026-04-10
phase: "17"
---

# Harness-First CLI Contract

`odin` is a harness-first control-plane CLI.

The binary does not start an interactive session when invoked without a subcommand. No-arg invocation must print root usage so a Codex or Claude harness can decide which explicit command to run next.

`cmd/odin/main.go` is the binary entrypoint. It resolves the repo root, creates process-level context, and forwards command arguments into the lifecycle dispatcher in `internal/app/lifecycle/run.go`.

## Entry points

- `odin help` prints the root command surface.
- `odin repl` starts the compatibility REPL.
- `odin status --json` exposes runtime readiness and approval state for harness polling.
- `odin task run --project <key> --title <title>` is the legacy compatibility path for durable task execution relative to the newer follow-through vocabulary.
- `odin initiative`, `odin companion`, `odin profile`, `odin followup`, and `odin agenda` are real root command families in the current dispatcher.

## Current root command families

The current root command dispatcher lives in `internal/app/lifecycle/run.go`. The product-facing root families now include:

- `odin initiative`
- `odin companion`
- `odin profile`
- `odin followup`
- `odin agenda`

Today, companion lifecycle is limited to explicit create and list operations. Deeper companion execution and swarm read surfaces should be added by extending this same root command family rather than creating a second CLI.

## Command boundary rules

- durable operations must be addressable as explicit root commands
- machine-readable output must be available on operational commands through `--json`
- the REPL is optional compatibility surface, not the default operator entry point
- harnesses own the conversational loop and invoke `odin` commands as needed
- the root command surface should remain stable enough for a harness to select `initiative`, `companion`, `profile`, `followup`, or `agenda` directly
- companion swarm behavior must extend the existing `odin companion`, `odin status`, and `odin agenda` paths instead of adding a parallel swarm CLI

## Execution rules

- headless durable execution routes through configured harness drivers
- missing harness driver configuration must fail explicitly instead of silently falling back to placeholder executors
- persisted CLI session state may seed explicit commands, but commands remain valid when called without prior REPL use

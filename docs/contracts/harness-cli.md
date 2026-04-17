---
title: Harness-First CLI Contract
status: active
date: 2026-04-10
phase: "17"
---

# Harness-First CLI Contract

`odin` is a harness-first control-plane CLI.

The binary does not start an interactive session when invoked without a subcommand. No-arg invocation must print root usage so a Codex or Claude harness can decide which explicit command to run next.

## Entry points

- `odin help` prints the root command surface.
- `odin repl` starts the compatibility REPL.
- `odin status --json` exposes runtime readiness and approval state for harness polling.
- `odin task run --project <key> --title <title>` creates and executes one durable task from an explicit command boundary.

## Intended root command families

The current root command implementation lives in `internal/app/lifecycle/run.go`. That surface still exposes the existing command set, but the intended product-facing root families are:

- `odin initiative`
- `odin companion`
- `odin profile`
- `odin followup`
- `odin agenda`

These are the command families the docs should name for the follow-through model. They are not yet claimed as implemented CLI subcommands by this contract.

## Command boundary rules

- durable operations must be addressable as explicit root commands
- machine-readable output must be available on operational commands through `--json`
- the REPL is optional compatibility surface, not the default operator entry point
- harnesses own the conversational loop and invoke `odin` commands as needed
- the root command surface should remain stable enough for a harness to select `initiative`, `companion`, `profile`, `followup`, or `agenda` when those command families land

## Execution rules

- headless durable execution routes through configured harness drivers
- missing harness driver configuration must fail explicitly instead of silently falling back to placeholder executors
- persisted CLI session state may seed explicit commands, but commands remain valid when called without prior REPL use

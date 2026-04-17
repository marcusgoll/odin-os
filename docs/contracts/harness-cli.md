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

## Command boundary rules

- durable operations must be addressable as explicit root commands
- machine-readable output must be available on operational commands through `--json`
- the REPL is optional compatibility surface, not the default operator entry point
- harnesses own the conversational loop and invoke `odin` commands as needed

## Execution rules

- headless durable execution routes through configured harness drivers
- missing harness driver configuration must fail explicitly instead of silently falling back to placeholder executors
- persisted CLI session state may seed explicit commands, but commands remain valid when called without prior REPL use

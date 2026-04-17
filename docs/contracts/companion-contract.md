---
title: Companion Contract
status: active
date: 2026-04-16
---

# Companion Contract

Companions are durable runtime roles attached to a workspace. They represent persistent operational identities such as assistants, advisors, operators, and specialists.

## Canonical kinds

- `assistant`
- `advisor`
- `operator`
- `specialist`

## Contract rules

- A companion is durable state, not a transient prompt bundle.
- A companion may reference catalog capabilities, tool policies, memory policy, and planning policy.
- A companion is not itself provider-specific. Provider prompts, model wrappers, and executor-specific bundles belong elsewhere.
- Companion records should be stable across runs and updated through explicit persistence, not inferred from one-off conversations.

## Persistence shape

A companion is identified by workspace and key, and stores:

- title
- kind
- charter
- status
- initiative scope JSON
- tool policy JSON
- memory policy JSON
- planning policy JSON

## Usage guidance

- Use companions as the durable role layer when a workspace needs named assistants or advisors.
- Keep provider-specific prompt construction outside the companion contract.
- Treat companion policy fields as declarative inputs for downstream systems, not as execution logic themselves.

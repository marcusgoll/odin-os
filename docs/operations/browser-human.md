# Browser Human Operations

Use this runbook to check the Chromium browser lane before you rely on the `odin-os` browser-human tools.

## Preflight

Run:

```bash
bash scripts/ops/browser-preflight.sh
```

Expected success output:

```text
READY: Chromium browser preflight passed (wrapper=/usr/bin/google-chrome runtime=/opt/google/chrome/chrome libs=ok)
```

The preflight is Chromium-only in this phase. If you request Firefox or WebKit, it fails closed.

## What It Checks

The preflight validates three things:

- a Chromium-family browser wrapper is present on the host
- the real Chrome runtime binary can be resolved and executed
- the linked host libraries for the runtime binary are present

This mirrors the live repo-local Chrome runtime instead of a stale Playwright assumption.

## Common Failures

- `Chromium browser binary is missing`: install or repair the Chrome wrapper/runtime path
- `required runtime libraries are missing`: inspect the real runtime with `ldd /opt/google/chrome/chrome`
- `Unsupported engine 'firefox'. Chromium only in this phase.`: rerun with the default Chromium engine

## Test Harness Overrides

These overrides are for the shell test harness only:

- `BROWSER_PREFLIGHT_CHROME_BIN`: point the preflight at a specific browser wrapper
- `BROWSER_PREFLIGHT_LDD_OUTPUT`: inject synthetic `ldd` output to prove the missing-library failure path

Do not set the `ldd` override during normal operator runs.

## Operational Notes

- The host currently uses `/usr/bin/google-chrome`, which resolves to the real runtime at `/opt/google/chrome/chrome`
- If the preflight passes but a browser tool still fails, check the browser runtime logs under the repo-local Odin browser state directory before assuming the host is broken
- Keep this lane Chromium-only until the phase explicitly expands engine support

#!/usr/bin/env node
export async function detectCaptcha() {
  return { type: null, sitekey: null, pageUrl: null };
}

export async function injectToken() {
  return false;
}

export async function solveCaptcha() {
  return { detected: false, solved: false, method: 'none', error: null };
}

export function _setAutoPassWaitMs() {
  return undefined;
}

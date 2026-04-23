package integration_test

import (
	"path/filepath"
	"testing"
)

func TestRobinhoodTransferFlowScript(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "robinhood-transfer-flow.sh")
	assertDriverScriptShape(t, scriptPath)

	t.Run("prepare review ready", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "robinhood-prepare.png")
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "robinhood-transfer-flow.sh", `{"tool_key":"robinhood_transfer_flow","input":{"mode":"prepare","direction":"deposit","amount_usd":"25.00","source_account":"checking","destination_account":"brokerage","memo":"test"}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":        "Robinhood transfer review ready",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH": screenshotPath,
		})
		assertStructuredDriverOutput(t, stdout, "robinhood_transfer_flow", "completed")
		assertJSONArtifactString(t, stdout, "session_state", "review_ready")
		assertJSONArtifactString(t, stdout, "next_action", "request approval")
		assertJSONArtifactString(t, stdout, "screenshot_path", screenshotPath)
		assertFileContainsSubstring(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContainsSubstring(t, callsLog, "request:https://robinhood.com")
		assertFileContainsSubstring(t, callsLog, "start:")
		assertFileContainsSubstring(t, callsLog, "navigate:https://robinhood.com")
		assertFileContainsSubstring(t, callsLog, "snapshot:")
		assertFileContainsSubstring(t, callsLog, "screenshot:")
	})

	t.Run("submit resume verification failed with prior session state", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "robinhood-submit.png")
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "robinhood-transfer-flow.sh", `{"tool_key":"robinhood_transfer_flow","input":{"mode":"submit","direction":"deposit","amount_usd":"25.00","source_account":"checking","destination_account":"brokerage","resume_facts":{"expected_review_state":"review_ready"}}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":                   "Robinhood transfer review recovered",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH":            screenshotPath,
			"ODIN_ROBINHOOD_TRANSFER_FIXTURE_STATE":        "resume_verification_failed",
			"ODIN_ROBINHOOD_TRANSFER_PRIOR_SESSION_STATE":  "session_expired",
		})
		assertStructuredDriverOutput(t, stdout, "robinhood_transfer_flow", "completed")
		assertJSONArtifactString(t, stdout, "session_state", "resume_verification_failed")
		assertJSONArtifactString(t, stdout, "prior_session_state", "session_expired")
		assertJSONArtifactString(t, stdout, "next_action", "fresh prepare required")
		assertJSONArtifactString(t, stdout, "screenshot_path", screenshotPath)
		assertFileContainsSubstring(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContainsSubstring(t, callsLog, "request:https://robinhood.com")
		assertFileContainsSubstring(t, callsLog, "start:")
		assertFileContainsSubstring(t, callsLog, "navigate:https://robinhood.com")
		assertFileContainsSubstring(t, callsLog, "snapshot:")
		assertFileContainsSubstring(t, callsLog, "screenshot:")
	})
}

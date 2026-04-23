package integration_test

import (
	"path/filepath"
	"testing"
)

func TestRobinhoodTransferFlowScript(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "robinhood-transfer-flow.sh")
	assertDriverScriptShape(t, scriptPath)

	t.Run("prepare live review ready uses headed session", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "robinhood-prepare-live.png")
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "robinhood-transfer-flow.sh", `{"tool_key":"robinhood_transfer_flow","input":{"mode":"prepare","direction":"deposit","amount_usd":"25.00","source_account":"checking","destination_account":"brokerage","memo":"test"}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":                             "Review your transfer",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH":                      screenshotPath,
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_TIMEOUT_SECONDS":       "0",
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_POLL_INTERVAL_SECONDS": "0",
		})
		assertStructuredDriverOutput(t, stdout, "robinhood_transfer_flow", "completed")
		assertJSONArtifactString(t, stdout, "session_state", "review_ready")
		assertJSONArtifactString(t, stdout, "screenshot_path", screenshotPath)
		assertFileContainsSubstring(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContainsSubstring(t, callsLog, "request:https://robinhood.com")
		assertFileContainsSubstring(t, callsLog, "start:--url https://robinhood.com/transfer --headed")
		assertFileNotContains(t, callsLog, "--headless")
		assertFileContainsSubstring(t, callsLog, "navigate:https://robinhood.com")
		assertFileContainsSubstring(t, callsLog, "snapshot:")
		assertFileContainsSubstring(t, callsLog, "screenshot:")
	})

	t.Run("prepare live unknown snapshot fails instead of defaulting review ready", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "robinhood-prepare-timeout.png")
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "robinhood-transfer-flow.sh", `{"tool_key":"robinhood_transfer_flow","input":{"mode":"prepare","direction":"deposit","amount_usd":"25.00","source_account":"checking","destination_account":"brokerage","memo":"test"}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":                             "Robinhood transfer dashboard",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH":                      screenshotPath,
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_TIMEOUT_SECONDS":       "0",
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_POLL_INTERVAL_SECONDS": "0",
		})
		assertStructuredDriverOutput(t, stdout, "robinhood_transfer_flow", "failed")
		assertFileContainsSubstring(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContainsSubstring(t, callsLog, "start:--url https://robinhood.com/transfer --headed")
		assertFileNotContains(t, callsLog, "--headless")
	})

	t.Run("prepare live initial form does not count as review ready", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "robinhood-prepare-initial-form.png")
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "robinhood-transfer-flow.sh", `{"tool_key":"robinhood_transfer_flow","input":{"mode":"prepare","direction":"deposit","amount_usd":"1.00","source_account":"checking","destination_account":"brokerage","memo":"test"}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":                             "Transfer money\nAmount\nFrom\nWells Fargo at Work Checking · Checking 4428\nTo\nChoose account\nReview transfer",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH":                      screenshotPath,
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_TIMEOUT_SECONDS":       "0",
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_POLL_INTERVAL_SECONDS": "0",
		})
		assertStructuredDriverOutput(t, stdout, "robinhood_transfer_flow", "failed")
		assertFileContainsSubstring(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContainsSubstring(t, callsLog, "snapshot:")
		assertFileContainsSubstring(t, callsLog, "screenshot:")
	})

	t.Run("prepare live final review screen is review ready", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "robinhood-prepare-final-review.png")
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "robinhood-transfer-flow.sh", `{"tool_key":"robinhood_transfer_flow","input":{"mode":"prepare","direction":"deposit","amount_usd":"1.00","source_account":"checking","destination_account":"brokerage","memo":"test"}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":                             "Transfer money\nAmount\nFrom\nWells Fargo at Work Checking · Checking 4428\nTo\nIndividual · $5,936.38\nFrequency\nJust once\n$1.00 will be deducted from your bank account within the next several days. It may take up to 5 business days to transfer.\nTransfer $1.00\nCancel",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH":                      screenshotPath,
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_TIMEOUT_SECONDS":       "0",
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_POLL_INTERVAL_SECONDS": "0",
		})
		assertStructuredDriverOutput(t, stdout, "robinhood_transfer_flow", "completed")
		assertJSONArtifactString(t, stdout, "session_state", "review_ready")
		assertFileContainsSubstring(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContainsSubstring(t, callsLog, "snapshot:")
	})

	t.Run("prepare live external browser preserves current robinhood page", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "robinhood-prepare-external.png")
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "robinhood-transfer-flow.sh", `{"tool_key":"robinhood_transfer_flow","input":{"mode":"prepare","direction":"deposit","amount_usd":"25.00","source_account":"checking","destination_account":"brokerage","memo":"test"}}`, map[string]string{
			"ODIN_BROWSER_SERVER_URL":                                "http://remote-browser.test:7777",
			"ODIN_BROWSER_STUB_CURRENT_URL":                          "https://robinhood.com/transfers/review",
			"ODIN_BROWSER_STUB_SNAPSHOT":                             "Review your transfer",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH":                      screenshotPath,
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_TIMEOUT_SECONDS":       "0",
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_POLL_INTERVAL_SECONDS": "0",
		})
		assertStructuredDriverOutput(t, stdout, "robinhood_transfer_flow", "completed")
		assertJSONArtifactString(t, stdout, "session_state", "review_ready")
		assertFileContainsSubstring(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContainsSubstring(t, callsLog, "start:--url https://robinhood.com/transfer --headed")
		assertFileContainsSubstring(t, callsLog, "current_url:")
		assertFileNotContains(t, callsLog, "navigate:https://robinhood.com")
	})

	t.Run("prepare review ready", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "robinhood-prepare.png")
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "robinhood-transfer-flow.sh", `{"tool_key":"robinhood_transfer_flow","input":{"mode":"prepare","direction":"deposit","amount_usd":"25.00","source_account":"checking","destination_account":"brokerage","memo":"test"}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":            "Robinhood transfer review ready",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH":     screenshotPath,
			"ODIN_ROBINHOOD_TRANSFER_FIXTURE_STATE": "review_ready",
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

	t.Run("submit live unknown snapshot becomes resume verification failed", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "robinhood-submit-live.png")
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "robinhood-transfer-flow.sh", `{"tool_key":"robinhood_transfer_flow","input":{"mode":"submit","direction":"deposit","amount_usd":"25.00","source_account":"checking","destination_account":"brokerage","resume_facts":{"expected_review_state":"review_ready"}}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":                             "Robinhood transfer dashboard",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH":                      screenshotPath,
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_TIMEOUT_SECONDS":       "0",
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_POLL_INTERVAL_SECONDS": "0",
		})
		assertStructuredDriverOutput(t, stdout, "robinhood_transfer_flow", "completed")
		assertJSONArtifactString(t, stdout, "session_state", "resume_verification_failed")
		assertJSONArtifactString(t, stdout, "next_action", "fresh prepare required")
		assertJSONArtifactString(t, stdout, "screenshot_path", screenshotPath)
		assertFileContainsSubstring(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContainsSubstring(t, callsLog, "start:--url https://robinhood.com/transfer --headed")
		assertFileNotContains(t, callsLog, "--headless")
	})

	t.Run("submit live deposit initiated counts as submitted", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "robinhood-submit-initiated.png")
		snapshot := "Pending transfers\nDeposit to individual from Wells Fargo at Work Checking\nApr 23, 2026\n+$1.00\nDeposit initiated\n+$1.00 from Wells Fargo at Work Checking\nDeposit initiated\nApr 23 • Today\nCovers Margin\nApr 24 • Amount borrowed decreases.\nDeposit completed\nApr 24 • Available to trade and withdraw\nContinue"
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "robinhood-transfer-flow.sh", `{"tool_key":"robinhood_transfer_flow","input":{"mode":"submit","direction":"deposit","amount_usd":"1.00","source_account":"checking","destination_account":"brokerage","resume_facts":{"expected_review_state":"review_ready"}}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":                             snapshot,
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH":                      screenshotPath,
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_TIMEOUT_SECONDS":       "0",
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_POLL_INTERVAL_SECONDS": "0",
		})
		assertStructuredDriverOutput(t, stdout, "robinhood_transfer_flow", "completed")
		assertJSONArtifactString(t, stdout, "session_state", "submitted")
		assertJSONArtifactString(t, stdout, "evidence", snapshot)
		assertJSONArtifactAbsent(t, stdout, "prior_session_state")
		assertFileContainsSubstring(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContainsSubstring(t, callsLog, "snapshot:")
	})

	t.Run("submit live external browser preserves current robinhood page", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "robinhood-submit-external.png")
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "robinhood-transfer-flow.sh", `{"tool_key":"robinhood_transfer_flow","input":{"mode":"submit","direction":"deposit","amount_usd":"25.00","source_account":"checking","destination_account":"brokerage","resume_facts":{"expected_review_state":"review_ready"}}}`, map[string]string{
			"ODIN_BROWSER_SERVER_URL":                                "http://remote-browser.test:7777",
			"ODIN_BROWSER_STUB_CURRENT_URL":                          "https://robinhood.com/transfers/review",
			"ODIN_BROWSER_STUB_SNAPSHOT":                             "transfer submitted",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH":                      screenshotPath,
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_TIMEOUT_SECONDS":       "0",
			"ODIN_ROBINHOOD_TRANSFER_ATTENDED_POLL_INTERVAL_SECONDS": "0",
		})
		assertStructuredDriverOutput(t, stdout, "robinhood_transfer_flow", "completed")
		assertJSONArtifactString(t, stdout, "session_state", "submitted")
		assertFileContainsSubstring(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContainsSubstring(t, callsLog, "start:--url https://robinhood.com/transfer --headed")
		assertFileContainsSubstring(t, callsLog, "current_url:")
		assertFileNotContains(t, callsLog, "navigate:https://robinhood.com")
	})

	t.Run("submit resume verification failed with prior session state", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "robinhood-submit.png")
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "robinhood-transfer-flow.sh", `{"tool_key":"robinhood_transfer_flow","input":{"mode":"submit","direction":"deposit","amount_usd":"25.00","source_account":"checking","destination_account":"brokerage","resume_facts":{"expected_review_state":"review_ready"}}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":                  "Robinhood transfer review recovered",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH":           screenshotPath,
			"ODIN_ROBINHOOD_TRANSFER_FIXTURE_STATE":       "resume_verification_failed",
			"ODIN_ROBINHOOD_TRANSFER_PRIOR_SESSION_STATE": "session_expired",
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

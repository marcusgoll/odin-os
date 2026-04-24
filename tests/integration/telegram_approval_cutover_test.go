package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestTelegramApprovalCutover(t *testing.T) {
	ctx := context.Background()
	sourceRepoRoot := projectRoot(t)
	repoRoot := createCLIRepoRootWithPreferredExecutor(t, "codex_headless")
	runtimeRoot := t.TempDir()
	odinBinary := buildOdinBinary(t, sourceRepoRoot)

	telegramWorkflowPath := filepath.Join(sourceRepoRoot, "ops", "n8n", "workflows", "odin-os-telegram-bot.json")
	telegramWorkflow, err := os.ReadFile(telegramWorkflowPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", telegramWorkflowPath, err)
	}
	telegramWorkflowText := string(telegramWorkflow)
	for _, needle := range []string{"approval-resolve:", "approval_id", "approval_decision"} {
		if !strings.Contains(telegramWorkflowText, needle) {
			t.Fatalf("%s missing %q", telegramWorkflowPath, needle)
		}
	}
	if strings.Contains(telegramWorkflowText, "nonce-update") {
		t.Fatalf("%s contains nonce-update, want approval-resolve-only callback handling", telegramWorkflowPath)
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "project",
		GitRoot:       repoRoot,
		DefaultBranch: "main",
		GitHubRepo:    "acme/odin-core",
		ManifestPath:  filepath.Join(repoRoot, "config", "projects.yaml"),
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "telegram-approval-cutover",
		Title:       "Telegram approval cutover",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	callbackRequest := simulateTelegramApprovalCallback(t, telegramWorkflowPath, approval.ID, "approve", "12345")
	if callbackRequest.RouterMode != "approval_resolve" {
		t.Fatalf("router_mode = %q, want approval_resolve", callbackRequest.RouterMode)
	}
	if callbackRequest.ApprovalID != itoa(approval.ID) {
		t.Fatalf("approval_id = %q, want %d", callbackRequest.ApprovalID, approval.ID)
	}
	if callbackRequest.ApprovalDecision != "approve" {
		t.Fatalf("approval_decision = %q, want approve", callbackRequest.ApprovalDecision)
	}
	if callbackRequest.Payload.ApprovalID != itoa(approval.ID) {
		t.Fatalf("payload approval_id = %q, want %d", callbackRequest.Payload.ApprovalID, approval.ID)
	}
	if callbackRequest.Payload.Decision != "approve" {
		t.Fatalf("payload decision = %q, want approve", callbackRequest.Payload.Decision)
	}
	assertCallbackHasNoNonceState(t, callbackRequest)

	routerScriptPath := filepath.Join(sourceRepoRoot, "scripts", "ops", "odin-n8n-ssh-dispatch.sh")
	routerCmd := exec.Command("bash", routerScriptPath)
	routerCmd.Dir = repoRoot
	routerCmd.Env = append(
		append([]string{}, os.Environ()...),
		"ODIN_BIN="+odinBinary,
		"ODIN_ROOT="+runtimeRoot,
		"SSH_ORIGINAL_COMMAND=approval-resolve "+callbackRequest.ApprovalID+" "+callbackRequest.ApprovalDecision+" "+callbackRequest.ApprovalReason,
	)

	outputBytes, err := routerCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("approval-resolve dispatch error = %v\n%s", err, string(outputBytes))
	}

	var payload struct {
		ID              int64  `json:"id"`
		Status          string `json:"status"`
		DecisionBy      string `json:"decision_by"`
		Reason          string `json:"reason"`
		ResolverSupport string `json:"resolver_support"`
		Result          string `json:"result"`
		Summary         string `json:"summary"`
	}
	if err := json.Unmarshal(outputBytes, &payload); err != nil {
		t.Fatalf("unmarshal approval resolve output = %v\n%s", err, string(outputBytes))
	}
	if payload.ID != approval.ID {
		t.Fatalf("approval id = %d, want %d", payload.ID, approval.ID)
	}
	if payload.Status != "pending" {
		t.Fatalf("approval status = %q, want pending", payload.Status)
	}
	if payload.ResolverSupport != "unsupported" {
		t.Fatalf("resolver_support = %q, want unsupported", payload.ResolverSupport)
	}
	if payload.Result != "not_resolved" {
		t.Fatalf("result = %q, want not_resolved", payload.Result)
	}
	if payload.Summary != "approval has no registered resolver; inspect only" {
		t.Fatalf("summary = %q, want unsupported summary", payload.Summary)
	}
	if payload.DecisionBy != "" {
		t.Fatalf("decision_by = %q, want empty unsupported decision maker", payload.DecisionBy)
	}

	resolved, err := store.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if resolved.Status != "pending" {
		t.Fatalf("approval row status = %q, want pending", resolved.Status)
	}
	if resolved.DecisionBy != "" {
		t.Fatalf("approval row decision_by = %q, want empty unsupported decision maker", resolved.DecisionBy)
	}

	assertNoNonceState(t, runtimeRoot)
	assertFileContains(t, filepath.Join(sourceRepoRoot, "docs/operations/odin-os-rollback.md"), []string{
		"old ssh forced-command entry",
		"old telegram workflow activation state",
		"old workflow exports",
		"odin-os intake becomes degraded",
	})
	assertFileContains(t, filepath.Join(sourceRepoRoot, "docs/operations/n8n-rollback.md"), []string{
		"old ssh forced-command entry",
		"old telegram workflow activation state",
		"old workflow exports",
		"odin-os intake becomes degraded",
	})
	assertFileContains(t, filepath.Join(sourceRepoRoot, "docs/operations/n8n-cutover.md"), []string{
		"Odin OS Telegram Bot",
		"`odin-telegram-bot`",
		"TELEGRAM_WEBHOOK_SECRET",
		"secret token",
		"outstanding legacy approval buttons",
	})
}

type telegramApprovalCallbackRequest struct {
	RouterMode       string `json:"router_mode"`
	ApprovalID       string `json:"approval_id"`
	ApprovalDecision string `json:"approval_decision"`
	ApprovalReason   string `json:"approval_reason"`
	Payload          struct {
		ApprovalID string `json:"approval_id"`
		Decision   string `json:"decision"`
		Reason     string `json:"reason"`
	} `json:"payload"`
}

type telegramWorkflowExport struct {
	Nodes []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Parameters struct {
			JSCode string `json:"jsCode"`
		} `json:"parameters"`
	} `json:"nodes"`
	Connections map[string]struct {
		Main [][]struct {
			Node string `json:"node"`
		} `json:"main"`
	} `json:"connections"`
}

func simulateTelegramApprovalCallback(t *testing.T, workflowPath string, approvalID int64, decision string, chatID string) telegramApprovalCallbackRequest {
	t.Helper()

	workflowBytes, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", workflowPath, err)
	}

	var workflow telegramWorkflowExport
	if err := json.Unmarshal(workflowBytes, &workflow); err != nil {
		t.Fatalf("unmarshal workflow = %v", err)
	}

	var callbackCode string
	for _, node := range workflow.Nodes {
		if node.ID == "build-callback-request" {
			callbackCode = node.Parameters.JSCode
			break
		}
	}
	if callbackCode == "" {
		t.Fatalf("%s missing Build Callback Router Request node", workflowPath)
	}
	assertTelegramCallbackWiring(t, workflow)

	scriptPath := filepath.Join(t.TempDir(), "simulate-telegram-callback.js")
	script := `
const code = ` + strconv.Quote(callbackCode) + `;
const approvalId = process.argv[2];
const decision = process.argv[3];
const chatId = process.argv[4];
const callbackData = 'approval-resolve:' + approvalId + ':' + decision;
const $input = {
  first() {
    return {
      json: {
        body: {
          callback_query: {
            id: 'callback-1',
            data: callbackData,
            message: {
              chat: { id: chatId },
              message_id: 9
            }
          }
        }
      }
    };
  }
};
const $env = { TELEGRAM_CHAT_ID: chatId };
const run = new Function('$input', '$env', 'Buffer', code);
const result = run($input, $env, Buffer);
process.stdout.write(JSON.stringify(result[0].json));
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", scriptPath, err)
	}

	cmd := exec.Command("node", scriptPath, itoa(approvalID), decision, chatID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node callback simulation error = %v\n%s", err, string(output))
	}

	var request telegramApprovalCallbackRequest
	if err := json.Unmarshal(output, &request); err != nil {
		t.Fatalf("unmarshal callback request = %v\n%s", err, string(output))
	}
	return request
}

func assertNoNonceState(t *testing.T, root string) {
	t.Helper()

	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(entry.Name()), "nonce") {
			t.Fatalf("runtime root contains unexpected nonce state: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatalf("WalkDir(%s) error = %v", root, err)
	}
}

func assertCallbackHasNoNonceState(t *testing.T, request telegramApprovalCallbackRequest) {
	t.Helper()

	payloadBytes, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal callback request = %v", err)
	}
	if strings.Contains(strings.ToLower(string(payloadBytes)), "nonce") {
		t.Fatalf("callback request unexpectedly contains nonce state: %s", string(payloadBytes))
	}
}

func assertTelegramCallbackWiring(t *testing.T, workflow telegramWorkflowExport) {
	t.Helper()

	requireWorkflowEdge := func(fromID string, toID string) {
		connection, ok := workflow.Connections[fromID]
		if !ok {
			t.Fatalf("workflow missing connection source %q", fromID)
		}
		for _, branch := range connection.Main {
			for _, edge := range branch {
				if edge.Node == toID {
					return
				}
			}
		}
		t.Fatalf("workflow connection %q -> %q not found", fromID, toID)
	}

	requireWorkflowEdge("Build Callback Router Request", "Dispatch Callback to Odin OS")
	requireWorkflowEdge("Dispatch Callback to Odin OS", "Answer Callback Query")
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

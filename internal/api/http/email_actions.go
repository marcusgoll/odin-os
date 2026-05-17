package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	approvalsvc "odin-os/internal/runtime/approvals"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

const emailActionTokenVersion = 1

type emailActionTokenClaims struct {
	Version             int    `json:"v"`
	Recipient           string `json:"recipient"`
	QueueID             string `json:"queue_id"`
	SourceType          string `json:"source_type"`
	ObjectID            int64  `json:"object_id"`
	Action              string `json:"action"`
	Reason              string `json:"reason"`
	PolicySnapshotHash  string `json:"policy_snapshot_hash,omitempty"`
	RuntimeSnapshotHash string `json:"runtime_snapshot_hash,omitempty"`
	IssuedAt            int64  `json:"iat"`
	ExpiresAt           int64  `json:"exp"`
}

type emailActionPreview struct {
	To      string                 `json:"to"`
	Subject string                 `json:"subject"`
	Text    string                 `json:"text"`
	HTML    string                 `json:"html"`
	Items   []emailActionEmailItem `json:"items"`
}

type emailActionEmailItem struct {
	QueueID        string                 `json:"queue_id"`
	SourceType     string                 `json:"source_type"`
	ObjectID       int64                  `json:"object_id"`
	Title          string                 `json:"title"`
	Status         string                 `json:"status"`
	Reason         string                 `json:"reason,omitempty"`
	AllowedActions []string               `json:"allowed_actions"`
	Links          []emailActionEmailLink `json:"links"`
}

type emailActionEmailLink struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	URL    string `json:"url"`
}

type emailActionResult struct {
	QueueID    string `json:"queue_id"`
	SourceType string `json:"source_type"`
	ObjectID   int64  `json:"object_id"`
	Action     string `json:"action"`
	Status     string `json:"status"`
	Result     string `json:"result"`
	Summary    string `json:"summary,omitempty"`
}

func handleEmailActionPreview(writer http.ResponseWriter, request *http.Request, deps Dependencies, now func() time.Time) {
	if statusCode, ok := authorizeAdmin(request, deps.AdminToken); !ok {
		writeAdminAuthorizationError(writer, statusCode)
		return
	}
	preview, err := buildEmailActionPreview(request.Context(), deps, request, now)
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "email_action_preview_unavailable", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, preview)
}

func handleEmailActionSend(writer http.ResponseWriter, request *http.Request, deps Dependencies, now func() time.Time) {
	if statusCode, ok := authorizeAdmin(request, deps.AdminToken); !ok {
		writeAdminAuthorizationError(writer, statusCode)
		return
	}
	preview, err := buildEmailActionPreview(request.Context(), deps, request, now)
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "email_action_preview_unavailable", err.Error())
		return
	}
	if len(preview.Items) == 0 {
		writeJSON(writer, http.StatusOK, map[string]any{"sent": false, "reason": "no pending email action items", "preview": preview})
		return
	}
	if err := sendEmailActionMessage(request.Context(), deps, preview); err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "email_action_send_failed", err.Error())
		return
	}
	writeJSON(writer, http.StatusAccepted, map[string]any{"sent": true, "to": preview.To, "subject": preview.Subject, "item_count": len(preview.Items)})
}

func handleEmailActionOpen(writer http.ResponseWriter, request *http.Request, deps Dependencies, now func() time.Time) {
	claims, err := verifyEmailActionToken(request.PathValue("token"), deps.EmailActionSecret, now())
	if err != nil {
		writeAPIError(writer, http.StatusForbidden, "email_action_token_invalid", err.Error())
		return
	}
	if claims.Action != "open-review" {
		writeAPIError(writer, http.StatusBadRequest, "email_action_open_invalid", "token is not an open-review action")
		return
	}
	http.Redirect(writer, request, "/pwa?queue_id="+url.QueryEscape(claims.QueueID), http.StatusFound)
}

func handleEmailActionApply(writer http.ResponseWriter, request *http.Request, deps Dependencies, now func() time.Time) {
	claims, err := verifyEmailActionToken(request.PathValue("token"), deps.EmailActionSecret, now())
	if err != nil {
		writeAPIError(writer, http.StatusForbidden, "email_action_token_invalid", err.Error())
		return
	}
	result, err := applyEmailAction(request.Context(), deps, claims)
	if err != nil {
		writeAPIError(writer, http.StatusConflict, "email_action_rejected", err.Error())
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(writer, "<!doctype html><title>Odin action applied</title><h1>Odin action applied</h1><p>%s</p><p><a href=\"/pwa?queue_id=%s\">Open Odin review</a></p>",
		html.EscapeString(result.Summary), url.QueryEscape(result.QueueID))
}

func buildEmailActionPreview(ctx context.Context, deps Dependencies, request *http.Request, now func() time.Time) (emailActionPreview, error) {
	if strings.TrimSpace(deps.EmailActionSecret) == "" {
		return emailActionPreview{}, fmt.Errorf("email action signing secret is required")
	}
	recipient := strings.TrimSpace(request.URL.Query().Get("to"))
	if recipient == "" {
		recipient = strings.TrimSpace(deps.EmailActionRecipient)
	}
	if recipient == "" {
		recipient = "marcusgoll@gmail.com"
	}
	baseURL := strings.TrimRight(strings.TrimSpace(deps.EmailActionBaseURL), "/")
	if baseURL == "" {
		scheme := "http"
		if request.TLS != nil {
			scheme = "https"
		}
		baseURL = scheme + "://" + request.Host
	}

	items, err := emailActionItems(ctx, deps, recipient, baseURL, now())
	if err != nil {
		return emailActionPreview{}, err
	}
	preview := emailActionPreview{
		To:      recipient,
		Subject: fmt.Sprintf("Odin needs %d approval/review decision(s)", len(items)),
		Items:   items,
	}
	preview.Text = renderEmailActionText(preview)
	preview.HTML = renderEmailActionHTML(preview)
	return preview, nil
}

func sendEmailActionMessage(ctx context.Context, deps Dependencies, preview emailActionPreview) error {
	path := strings.TrimSpace(deps.EmailActionSendmail)
	if path == "" {
		return fmt.Errorf("email action sendmail path is not configured")
	}
	if strings.ContainsAny(path, "\r\n") {
		return fmt.Errorf("email action sendmail path is invalid")
	}
	message, err := renderEmailActionMIME(deps, preview)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, path, "-t", "-i")
	command.Stdin = strings.NewReader(message)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sendmail failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func renderEmailActionMIME(deps Dependencies, preview emailActionPreview) (string, error) {
	from := strings.TrimSpace(deps.EmailActionFrom)
	if from == "" {
		from = "odin-os@localhost"
	}
	for _, value := range []string{from, preview.To, preview.Subject} {
		if strings.ContainsAny(value, "\r\n") {
			return "", fmt.Errorf("email header contains newline")
		}
	}
	boundary := "odin-email-actions"
	var builder strings.Builder
	builder.WriteString("From: ")
	builder.WriteString(from)
	builder.WriteString("\r\nTo: ")
	builder.WriteString(preview.To)
	builder.WriteString("\r\nSubject: ")
	builder.WriteString(preview.Subject)
	builder.WriteString("\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"")
	builder.WriteString(boundary)
	builder.WriteString("\"\r\n\r\n--")
	builder.WriteString(boundary)
	builder.WriteString("\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")
	builder.WriteString(preview.Text)
	builder.WriteString("\r\n--")
	builder.WriteString(boundary)
	builder.WriteString("\r\nContent-Type: text/html; charset=utf-8\r\n\r\n")
	builder.WriteString(preview.HTML)
	builder.WriteString("\r\n--")
	builder.WriteString(boundary)
	builder.WriteString("--\r\n")
	return builder.String(), nil
}

func emailActionItems(ctx context.Context, deps Dependencies, recipient string, baseURL string, now time.Time) ([]emailActionEmailItem, error) {
	approvals, err := mobileApprovals(ctx, deps)
	if err != nil {
		return nil, err
	}
	reviewItems, err := mobileReviewQueue(ctx, deps)
	if err != nil {
		return nil, err
	}

	items := make([]emailActionEmailItem, 0, len(approvals)+len(reviewItems))
	for _, approval := range approvals {
		if approval.Status != "pending" {
			continue
		}
		item := emailActionEmailItem{
			QueueID:        fmt.Sprintf("approval:%d", approval.ID),
			SourceType:     "approval",
			ObjectID:       approval.ID,
			Title:          approval.Title,
			Status:         approval.Status,
			Reason:         approval.RequiredReason,
			AllowedActions: []string{"approve", "deny", "clarify", "open-review"},
		}
		for _, action := range item.AllowedActions {
			link, err := emailActionLink(baseURL, deps.EmailActionSecret, emailActionTokenClaims{
				Recipient:           recipient,
				QueueID:             item.QueueID,
				SourceType:          item.SourceType,
				ObjectID:            item.ObjectID,
				Action:              action,
				Reason:              "email action from " + recipient,
				PolicySnapshotHash:  approval.PolicySnapshotHash,
				RuntimeSnapshotHash: approval.RuntimeSnapshotHash,
			}, now)
			if err != nil {
				return nil, err
			}
			item.Links = append(item.Links, emailActionEmailLink{Action: action, Label: emailActionLabel(action), URL: link})
		}
		items = append(items, item)
	}

	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		seen[item.QueueID] = struct{}{}
	}
	for _, review := range reviewItems {
		if _, ok := seen[review.QueueID]; ok {
			continue
		}
		actions := emailSupportedReviewActions(review)
		if len(actions) == 0 {
			actions = []string{"open-review"}
		}
		item := emailActionEmailItem{
			QueueID:        review.QueueID,
			SourceType:     review.SourceType,
			ObjectID:       review.ObjectID,
			Title:          review.Title,
			Status:         review.Status,
			Reason:         review.Reason,
			AllowedActions: actions,
		}
		for _, action := range actions {
			link, err := emailActionLink(baseURL, deps.EmailActionSecret, emailActionTokenClaims{
				Recipient:  recipient,
				QueueID:    item.QueueID,
				SourceType: item.SourceType,
				ObjectID:   item.ObjectID,
				Action:     action,
				Reason:     "email action from " + recipient,
			}, now)
			if err != nil {
				return nil, err
			}
			item.Links = append(item.Links, emailActionEmailLink{Action: action, Label: emailActionLabel(action), URL: link})
		}
		items = append(items, item)
	}
	return items, nil
}

func emailSupportedReviewActions(item mobileReviewItem) []string {
	if item.SourceType != "intake_review" || item.Status != "review_required" {
		return []string{"open-review"}
	}
	actions := []string{}
	for _, action := range item.AllowedActions {
		switch action {
		case "reject", "clarify", "archive":
			actions = append(actions, action)
		}
	}
	actions = append(actions, "open-review")
	return actions
}

func emailActionLink(baseURL string, secret string, claims emailActionTokenClaims, now time.Time) (string, error) {
	token, err := signEmailActionToken(secret, claims, now, now.Add(24*time.Hour))
	if err != nil {
		return "", err
	}
	if claims.Action == "open-review" {
		return baseURL + "/email-actions/open/" + url.PathEscape(token), nil
	}
	return baseURL + "/email-actions/" + url.PathEscape(token), nil
}

func signEmailActionToken(secret string, claims emailActionTokenClaims, issuedAt time.Time, expiresAt time.Time) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", fmt.Errorf("email action signing secret is required")
	}
	claims.Version = emailActionTokenVersion
	claims.Recipient = strings.TrimSpace(claims.Recipient)
	claims.QueueID = strings.TrimSpace(claims.QueueID)
	claims.SourceType = strings.TrimSpace(claims.SourceType)
	claims.Action = strings.ToLower(strings.TrimSpace(claims.Action))
	claims.Reason = strings.TrimSpace(claims.Reason)
	claims.IssuedAt = issuedAt.UTC().Unix()
	claims.ExpiresAt = expiresAt.UTC().Unix()
	if claims.Recipient == "" || claims.QueueID == "" || claims.SourceType == "" || claims.ObjectID <= 0 || claims.Action == "" {
		return "", fmt.Errorf("email action token is incomplete")
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := emailActionSignature(secret, encodedPayload)
	return encodedPayload + "." + signature, nil
}

func verifyEmailActionToken(token string, secret string, now time.Time) (emailActionTokenClaims, error) {
	if strings.TrimSpace(secret) == "" {
		return emailActionTokenClaims{}, fmt.Errorf("email action signing secret is required")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return emailActionTokenClaims{}, fmt.Errorf("malformed email action token")
	}
	want := emailActionSignature(secret, parts[0])
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return emailActionTokenClaims{}, fmt.Errorf("email action signature mismatch")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return emailActionTokenClaims{}, fmt.Errorf("email action payload is invalid")
	}
	var claims emailActionTokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return emailActionTokenClaims{}, fmt.Errorf("email action payload is invalid")
	}
	if claims.Version != emailActionTokenVersion {
		return emailActionTokenClaims{}, fmt.Errorf("unsupported email action token version")
	}
	if now.UTC().Unix() > claims.ExpiresAt {
		return emailActionTokenClaims{}, fmt.Errorf("email action token expired")
	}
	if strings.TrimSpace(claims.Recipient) == "" || strings.TrimSpace(claims.QueueID) == "" || claims.ObjectID <= 0 {
		return emailActionTokenClaims{}, fmt.Errorf("email action token is incomplete")
	}
	return claims, nil
}

func emailActionSignature(secret string, encodedPayload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encodedPayload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func applyEmailAction(ctx context.Context, deps Dependencies, claims emailActionTokenClaims) (emailActionResult, error) {
	switch claims.SourceType {
	case "approval":
		return applyEmailApprovalAction(ctx, deps, claims)
	case "intake_review":
		return applyEmailIntakeReviewAction(ctx, deps, claims)
	default:
		return emailActionResult{}, fmt.Errorf("email action %q is inspect-only for source %q", claims.Action, claims.SourceType)
	}
}

func applyEmailApprovalAction(ctx context.Context, deps Dependencies, claims emailActionTokenClaims) (emailActionResult, error) {
	action := strings.ToLower(strings.TrimSpace(claims.Action))
	if action != "approve" && action != "deny" && action != "clarify" {
		return emailActionResult{}, fmt.Errorf("approval email action must be approve, deny, or clarify")
	}
	service := approvalsvc.Service{Store: deps.Store}
	detail, err := service.Detail(ctx, claims.ObjectID)
	if err != nil {
		return emailActionResult{}, err
	}
	if detail.Approval.Status != "pending" {
		return emailActionResult{}, fmt.Errorf("approval %d is %s, want pending", claims.ObjectID, detail.Approval.Status)
	}
	if claims.PolicySnapshotHash != "" && claims.PolicySnapshotHash != detail.Approval.PolicySnapshotHash {
		return emailActionResult{}, fmt.Errorf("%w: policy snapshot changed since email was generated", approvalsvc.ErrStaleApproval)
	}
	if claims.RuntimeSnapshotHash != "" && claims.RuntimeSnapshotHash != detail.Approval.RuntimeSnapshotHash {
		return emailActionResult{}, fmt.Errorf("%w: runtime snapshot changed since email was generated", approvalsvc.ErrStaleApproval)
	}
	result, err := service.Resolve(ctx, approvalsvc.ResolveParams{
		ApprovalID: claims.ObjectID,
		Action:     action,
		DecisionBy: "email:" + claims.Recipient,
		Reason:     firstNonEmpty(claims.Reason, "email action"),
	})
	if err != nil {
		return emailActionResult{}, err
	}
	receipt, err := approvalsvc.FormatReceipt(result)
	if err != nil {
		return emailActionResult{}, err
	}
	return emailActionResult{
		QueueID:    claims.QueueID,
		SourceType: claims.SourceType,
		ObjectID:   claims.ObjectID,
		Action:     action,
		Status:     result.Approval.Status,
		Result:     strings.TrimPrefix(receipt.Line, fmt.Sprintf("approval=%d status=resolved result=", result.Approval.ID)),
		Summary:    strings.TrimPrefix(receipt.Summary, "summary="),
	}, nil
}

func applyEmailIntakeReviewAction(ctx context.Context, deps Dependencies, claims emailActionTokenClaims) (emailActionResult, error) {
	action := strings.ToLower(strings.TrimSpace(claims.Action))
	existing, err := deps.Store.GetIntakeItem(ctx, claims.ObjectID)
	if err != nil {
		return emailActionResult{}, err
	}
	if existing.Status != "review_required" {
		return emailActionResult{}, fmt.Errorf("intake review %d is %s, want review_required", claims.ObjectID, existing.Status)
	}
	routingNotes := strings.TrimSpace(existing.RoutingNotes)
	if routingNotes == "" {
		routingNotes = "{}"
	}
	status := ""
	decision := ""
	eventType := runtimeevents.EventIntakeReviewRejected
	summary := ""
	switch action {
	case "reject":
		status = "rejected"
		decision = "rejected"
		eventType = runtimeevents.EventIntakeReviewRejected
		summary = "Intake review rejected from Odin email action: " + claims.Reason
	case "clarify":
		status = "needs_clarification"
		decision = "clarification_requested"
		eventType = runtimeevents.EventIntakeReviewClarificationRequested
		summary = "Operator requested clarification from Odin email action: " + claims.Reason
	case "archive":
		status = "archived"
		decision = "archived"
		eventType = runtimeevents.EventIntakeReviewArchived
		summary = "Intake archived from Odin email action: " + claims.Reason
	default:
		return emailActionResult{}, fmt.Errorf("intake review email action must be reject, clarify, or archive")
	}
	updated, err := deps.Store.ReviewIntakeItem(ctx, sqlite.ReviewIntakeItemParams{
		ID:             claims.ObjectID,
		Status:         status,
		Summary:        summary,
		RoutingNotes:   routingNotes,
		EventType:      eventType,
		Decision:       decision,
		PolicyDecision: "email_review_decision",
		PolicyReason:   claims.Reason,
	})
	if err != nil {
		return emailActionResult{}, err
	}
	return emailActionResult{
		QueueID:    claims.QueueID,
		SourceType: claims.SourceType,
		ObjectID:   claims.ObjectID,
		Action:     action,
		Status:     updated.Status,
		Result:     decision,
		Summary:    "email action applied to " + claims.QueueID,
	}, nil
}

func renderEmailActionText(preview emailActionPreview) string {
	var builder strings.Builder
	builder.WriteString(preview.Subject)
	builder.WriteString("\n\n")
	for _, item := range preview.Items {
		builder.WriteString(item.QueueID)
		builder.WriteString(" ")
		builder.WriteString(firstNonEmpty(item.Title, item.SourceType))
		builder.WriteString(" status=")
		builder.WriteString(item.Status)
		builder.WriteString("\n")
		for _, link := range item.Links {
			builder.WriteString("- ")
			builder.WriteString(link.Label)
			builder.WriteString(": ")
			builder.WriteString(link.URL)
			builder.WriteString("\n")
		}
		builder.WriteString("Fallback: odin review show ")
		builder.WriteString(item.QueueID)
		builder.WriteString("\n\n")
	}
	return builder.String()
}

func renderEmailActionHTML(preview emailActionPreview) string {
	var builder strings.Builder
	builder.WriteString("<!doctype html><html><body>")
	builder.WriteString("<h1>")
	builder.WriteString(html.EscapeString(preview.Subject))
	builder.WriteString("</h1>")
	for _, item := range preview.Items {
		builder.WriteString("<section><h2>")
		builder.WriteString(html.EscapeString(firstNonEmpty(item.Title, item.QueueID)))
		builder.WriteString("</h2><p>")
		builder.WriteString(html.EscapeString(item.QueueID + " status=" + item.Status))
		if item.Reason != "" {
			builder.WriteString(" reason=")
			builder.WriteString(html.EscapeString(item.Reason))
		}
		builder.WriteString("</p><p>")
		for _, link := range item.Links {
			builder.WriteString("<a href=\"")
			builder.WriteString(html.EscapeString(link.URL))
			builder.WriteString("\">")
			builder.WriteString(html.EscapeString(link.Label))
			builder.WriteString("</a> ")
		}
		builder.WriteString("</p><p>Fallback: <code>odin review show ")
		builder.WriteString(html.EscapeString(item.QueueID))
		builder.WriteString("</code></p></section>")
	}
	builder.WriteString("</body></html>")
	return builder.String()
}

func emailActionLabel(action string) string {
	switch action {
	case "approve":
		return "Approve"
	case "deny":
		return "Deny"
	case "reject":
		return "Reject"
	case "clarify":
		return "Clarify"
	case "archive":
		return "Archive"
	case "open-review":
		return "Open review"
	default:
		return strings.Title(strings.ReplaceAll(action, "-", " "))
	}
}

func extractFirstEmailActionURL(preview emailActionPreview, action string) (string, error) {
	for _, item := range preview.Items {
		for _, link := range item.Links {
			if link.Action == action {
				return link.URL, nil
			}
		}
	}
	return "", errors.New("email action link not found")
}

func emailActionIDFromQueueID(queueID string) int64 {
	_, rawID, ok := strings.Cut(queueID, ":")
	if !ok {
		return 0
	}
	id, _ := strconv.ParseInt(rawID, 10, 64)
	return id
}

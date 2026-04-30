package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const tradeboardUsage = "/tradeboard status|trips|credit|scan|sync-status|pickup <pairing_id> <...> | post <pairing_id> <type=...> <...>"

const tradeboardSyncSuccess = "success"

type tradeboardClient struct {
	BaseURL    string
	Token      string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type tradeboardTripsResponse struct {
	Trips     []map[string]any `json:"trips"`
	Timestamp string           `json:"timestamp"`
}

type tradeboardStatusResponse struct {
	LastScan   string `json:"last_scan"`
	TripsFound int    `json:"trips_found"`
}

type flicaSyncStatusResponse struct {
	LastSync         string `json:"last_sync"`
	LastSyncStatus   string `json:"last_sync_status"`
	FlicaSyncRunning bool   `json:"flica_sync_running"`
	ActiveFlicaRunID string `json:"active_flica_run_id"`
}

func newTradeboardClient() tradeboardClient {
	baseURL := strings.TrimSpace(os.Getenv("ODIN_TRADEBOARD_API_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("PBS_API_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8083"
	}

	token := strings.TrimSpace(os.Getenv("ODIN_TRADEBOARD_API_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("FLIGHT_API_TOKEN"))
	}
	timeout := tradeboardHTTPTimeout()

	return tradeboardClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Token:      token,
		Timeout:    timeout,
		HTTPClient: &http.Client{Timeout: timeout},
	}
}

func tradeboardHTTPTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("ODIN_TRADEBOARD_API_TIMEOUT_SECONDS"))
	if raw == "" {
		return 180 * time.Second
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 180 * time.Second
	}
	if parsed < 5 {
		parsed = 5
	}
	if parsed > 600 {
		parsed = 600
	}
	return time.Duration(parsed) * time.Second
}

func (client tradeboardClient) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var payload io.Reader
	if body != nil {
		bytesPayload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(bytesPayload)
	}

	request, err := http.NewRequestWithContext(ctx, method, client.BaseURL+path, payload)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if client.Token != "" {
		request.Header.Set("Authorization", "Bearer "+client.Token)
	}

	return client.HTTPClient.Do(request)
}

func (client tradeboardClient) readError(resp *http.Response) string {
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	bodyText := strings.TrimSpace(string(body))
	if bodyText == "" {
		return fmt.Sprintf("status=%d", resp.StatusCode)
	}
	return fmt.Sprintf("status=%d body=%s", resp.StatusCode, bodyText)
}

func (client tradeboardClient) requireSuccessfulFlicaSync(ctx context.Context, output io.Writer) error {
	resp, err := client.request(ctx, http.MethodGet, "/ops/flica/status", nil)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "unable to verify Flica sync status: %v\n", err)
		if writeErr != nil {
			return writeErr
		}
		return fmt.Errorf("flica sync check failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, writeErr := fmt.Fprintf(output, "flica sync check failed: %s\n", client.readError(resp))
		if writeErr != nil {
			return writeErr
		}
		return fmt.Errorf("flica sync check failed")
	}

	var payload flicaSyncStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		_ = resp.Body.Close()
		_, writeErr := fmt.Fprintf(output, "flica sync check parse error: %v\n", err)
		if writeErr != nil {
			return writeErr
		}
		return fmt.Errorf("flica sync check failed")
	}
	_ = resp.Body.Close()

	status := strings.ToLower(strings.TrimSpace(payload.LastSyncStatus))
	if payload.FlicaSyncRunning {
		_, writeErr := fmt.Fprintf(
			output,
			"flica sync in progress (run_id=%s); wait before posting/picking\n",
			payload.ActiveFlicaRunID,
		)
		if writeErr != nil {
			return writeErr
		}
		return fmt.Errorf("flica sync in progress")
	}

	if status != tradeboardSyncSuccess {
		_, writeErr := fmt.Fprintf(
			output,
			"flica sync preflight failed: last_sync_status=%s last_sync=%s\n",
			payload.LastSyncStatus,
			payload.LastSync,
		)
		if writeErr != nil {
			return writeErr
		}
		return fmt.Errorf("flica sync preflight failed")
	}

	return nil
}

func parseTradeboardArgs(args []string) map[string]string {
	flags := make(map[string]string)
	for _, arg := range args {
		if strings.EqualFold(arg, "--confirm") || strings.EqualFold(arg, "confirm") {
			flags["confirm"] = "true"
			continue
		}

		key, value, hasValue := strings.Cut(arg, "=")
		if !hasValue {
			continue
		}

		key = strings.TrimLeft(strings.TrimSpace(strings.ToLower(key)), "-")
		flags[key] = value
	}
	return flags
}

func parseTradeboardIntList(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var values []int
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		value, err := strconv.Atoi(trimmed)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func renderTradeboardTrip(trip map[string]any) string {
	pairingID := ""
	if value, ok := trip["pairing_id"]; ok {
		pairingID, _ = value.(string)
	}
	pickupType := "fcfs"
	if value, ok := trip["pickup_type"]; ok {
		if asString, ok := value.(string); ok && asString != "" {
			pickupType = asString
		}
	}
	creditTime := "0"
	if value, ok := trip["credit_time"]; ok {
		switch typed := value.(type) {
		case float64:
			creditTime = fmt.Sprintf("%.1f", typed)
		case int:
			creditTime = fmt.Sprintf("%d", typed)
		case string:
			creditTime = typed
		}
	}
	origin := ""
	if value, ok := trip["origin"]; ok {
		origin, _ = value.(string)
	}
	destination := ""
	if value, ok := trip["destination"]; ok {
		destination, _ = value.(string)
	}
	bcid := ""
	if value, ok := trip["bcid"]; ok {
		bcid, _ = value.(string)
	}
	if origin != "" && destination != "" {
		startDate := ""
		if value, ok := trip["start_date"]; ok {
			startDate, _ = value.(string)
		}
		endDate := ""
		if value, ok := trip["end_date"]; ok {
			endDate, _ = value.(string)
		}
		report := ""
		if value, ok := trip["report_time"]; ok {
			report, _ = value.(string)
		}
		end := ""
		if value, ok := trip["release_time"]; ok {
			end, _ = value.(string)
		}
		if report != "" {
			return fmt.Sprintf(
				"%s %s %s→%s (%s-%s) report=%s end=%s credit=%sh bcid=%s",
				pairingID, pickupType, origin, destination, startDate, endDate, report, end, creditTime, bcid,
			)
		}
		return fmt.Sprintf("%s %s %s→%s credit=%sh bcid=%s", pairingID, pickupType, origin, destination, creditTime, bcid)
	}
	return fmt.Sprintf("%s %s credit=%sh bcid=%s", pairingID, pickupType, creditTime, bcid)
}

func (shell *Shell) handleTradeboard(ctx context.Context, args []string, output io.Writer) error {
	client := newTradeboardClient()
	if len(args) == 0 || strings.EqualFold(args[0], "help") {
		_, err := fmt.Fprintf(output, "%s\n", tradeboardUsage)
		return err
	}

	subcommand := strings.ToLower(args[0])

	switch subcommand {
	case "status":
		resp, err := client.request(ctx, http.MethodGet, "/tradeboard/status", nil)
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "unable to fetch tradeboard status: %v\n", err)
			return writeErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, writeErr := fmt.Fprintf(output, "tradeboard status failed: %s\n", client.readError(resp))
			return writeErr
		}

		var payload tradeboardStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			_, writeErr := fmt.Fprintf(output, "tradeboard status parse error: %v\n", err)
			return writeErr
		}
		_ = resp.Body.Close()

		_, err = fmt.Fprintf(
			output,
			"last_scan=%s trips_found=%d\n",
			payload.LastScan,
			payload.TripsFound,
		)
		return err

	case "trips":
		limit := 20
		flags := parseTradeboardArgs(args[1:])
		if raw, ok := flags["limit"]; ok && raw != "" {
			if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
				limit = parsed
			}
		}

		resp, err := client.request(ctx, http.MethodGet, "/tradeboard/trips", nil)
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "unable to fetch tradeboard trips: %v\n", err)
			return writeErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, writeErr := fmt.Fprintf(output, "tradeboard trips failed: %s\n", client.readError(resp))
			return writeErr
		}

		var payload tradeboardTripsResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			_, writeErr := fmt.Fprintf(output, "tradeboard trips parse error: %v\n", err)
			return writeErr
		}
		_ = resp.Body.Close()

		var lines []string
		for idx, trip := range payload.Trips {
			if idx >= limit {
				break
			}
			lines = append(lines, renderTradeboardTrip(trip))
		}
		_, err = fmt.Fprintf(output, "timestamp=%s count=%d\n", payload.Timestamp, len(lines))
		if err != nil {
			return err
		}
		for _, line := range lines {
			if _, writeErr := fmt.Fprintf(output, "%s\n", line); writeErr != nil {
				return writeErr
			}
		}
		return nil

	case "credit":
		resp, err := client.request(ctx, http.MethodGet, "/tradeboard/credit", nil)
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "unable to fetch tradeboard credit: %v\n", err)
			return writeErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, writeErr := fmt.Fprintf(output, "tradeboard credit failed: %s\n", client.readError(resp))
			return writeErr
		}

		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			_, writeErr := fmt.Fprintf(output, "tradeboard credit parse error: %v\n", err)
			return writeErr
		}
		_ = resp.Body.Close()

		currentCredit := payload["current_credit"]
		creditFloor := payload["credit_floor"]
		projectedCredit := payload["projected_credit"]
		pendingPickups := payload["pending_pickups"]
		pendingDrops := payload["pending_drops"]
		_, err = fmt.Fprintf(
			output,
			"current=%v floor=%v projected=%v pending_pickups=%v pending_drops=%v\n",
			currentCredit,
			creditFloor,
			projectedCredit,
			pendingPickups,
			pendingDrops,
		)
		return err

	case "scan", "sync":
		flags := parseTradeboardArgs(args[1:])
		headless := true
		if raw, ok := flags["headless"]; ok {
			parsed, parseErr := strconv.ParseBool(raw)
			if parseErr == nil {
				headless = parsed
			}
		}
		resp, err := client.request(ctx, http.MethodPost, fmt.Sprintf("/tradeboard/scan?headless=%t", headless), nil)
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "unable to trigger tradeboard scan: %v\n", err)
			return writeErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, writeErr := fmt.Fprintf(output, "tradeboard scan failed: %s\n", client.readError(resp))
			return writeErr
		}

		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			_ = resp.Body.Close()
			_, writeErr := fmt.Fprintf(output, "tradeboard scan parse error: %v\n", err)
			return writeErr
		}
		_ = resp.Body.Close()

		_, err = fmt.Fprintln(output, "scan_started")
		if note, ok := payload["message"]; ok {
			_, err = fmt.Fprintf(output, "note=%v\n", note)
		}
		return err

	case "sync-status":
		resp, err := client.request(ctx, http.MethodGet, "/ops/flica/status", nil)
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "unable to fetch flica sync status: %v\n", err)
			return writeErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, writeErr := fmt.Fprintf(output, "flica sync status failed: %s\n", client.readError(resp))
			return writeErr
		}

		var payload flicaSyncStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			_ = resp.Body.Close()
			_, writeErr := fmt.Fprintf(output, "flica sync status parse error: %v\n", err)
			return writeErr
		}
		_ = resp.Body.Close()

		_, err = fmt.Fprintf(
			output,
			"last_sync=%s status=%s running=%t",
			payload.LastSync,
			payload.LastSyncStatus,
			payload.FlicaSyncRunning,
		)
		if payload.ActiveFlicaRunID != "" {
			_, err = fmt.Fprintf(output, " run_id=%s", payload.ActiveFlicaRunID)
		}
		if err == nil {
			_, err = fmt.Fprintln(output)
		}
		return err

	case "pickup":
		if len(args) < 2 {
			_, err := fmt.Fprintln(output, "usage: /tradeboard pickup <pairing_id> [type=fcfs|seniority] [bcid=<NNN.NNN>] [confirm]")
			return err
		}
		pairingID := strings.TrimSpace(args[1])
		flags := parseTradeboardArgs(args[2:])
		pickupType := "fcfs"
		if value, ok := flags["type"]; ok {
			pickupType = strings.ToLower(strings.TrimSpace(value))
		}
		if pickupType != "fcfs" && pickupType != "seniority" {
			_, err := fmt.Fprintln(output, "usage: /tradeboard pickup <pairing_id> [type=fcfs|seniority]")
			return err
		}
		bcid := strings.TrimSpace(flags["bcid"])
		if bcid == "" {
			_, err := fmt.Fprintln(output, "pickup requires bcid=<NNN.NNN> (from /tradeboard trips)")
			return err
		}
		if _, ok := flags["confirm"]; !ok {
			_, err := fmt.Fprintln(output, "pickup requires confirm")
			return err
		}
		if err := client.requireSuccessfulFlicaSync(ctx, output); err != nil {
			return err
		}

		payload := map[string]any{
			"pairing_id":  pairingID,
			"pickup_type": pickupType,
			"bcid":        bcid,
		}
		resp, err := client.request(ctx, http.MethodPost, "/tradeboard/pickup", payload)
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "unable to request pickup: %v\n", err)
			return writeErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, writeErr := fmt.Fprintf(output, "tradeboard pickup failed: %s\n", client.readError(resp))
			return writeErr
		}
		_, err = fmt.Fprintf(output, "pickup requested: pairing=%s type=%s bcid=%s\n", pairingID, pickupType, bcid)
		return err

	case "post":
		if len(args) < 2 {
			_, err := fmt.Fprintln(
				output,
				"usage: /tradeboard post <pairing_id> [type=<Drop|Swap|Add>] [bcid=<NNN.NNN>] [comment=...] [split_legs=0,1] [split_keep_legs=0,1] [confirm]",
			)
			return err
		}
		pairingID := strings.TrimSpace(args[1])
		flags := parseTradeboardArgs(args[2:])
		tradeType := "Drop"
		if value, ok := flags["type"]; ok {
			tradeType = value
		}
		bcid := strings.TrimSpace(flags["bcid"])
		if bcid == "" {
			_, err := fmt.Fprintln(output, "post requires bcid=<NNN.NNN> (from /tradeboard trips)")
			return err
		}
		comments := strings.TrimSpace(flags["comment"])
		if _, ok := flags["confirm"]; !ok {
			_, err := fmt.Fprintln(output, "post requires confirm")
			return err
		}
		if err := client.requireSuccessfulFlicaSync(ctx, output); err != nil {
			return err
		}

		payload := map[string]any{
			"pairing_id": pairingID,
			"trade_type": tradeType,
			"bcid":       bcid,
			"comments":   comments,
		}
		if raw, ok := flags["split_legs"]; ok {
			splitLegs, parseErr := parseTradeboardIntList(raw)
			if parseErr != nil {
				_, writeErr := fmt.Fprintf(output, "split_legs must be comma-separated integers\n")
				if writeErr != nil {
					return writeErr
				}
				return parseErr
			}
			payload["split_legs"] = splitLegs
		}
		if raw, ok := flags["split_keep_legs"]; ok {
			splitKeepLegs, parseErr := parseTradeboardIntList(raw)
			if parseErr != nil {
				_, writeErr := fmt.Fprintf(output, "split_keep_legs must be comma-separated integers\n")
				if writeErr != nil {
					return writeErr
				}
				return parseErr
			}
			payload["split_keep_legs"] = splitKeepLegs
		}
		resp, err := client.request(ctx, http.MethodPost, "/tradeboard/post", payload)
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "unable to request post: %v\n", err)
			return writeErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, writeErr := fmt.Fprintf(output, "tradeboard post failed: %s\n", client.readError(resp))
			return writeErr
		}
		_, err = fmt.Fprintf(output, "post requested: pairing=%s type=%s bcid=%s\n", pairingID, tradeType, bcid)
		return err

	default:
		_, err := fmt.Fprintf(output, "%s\n", tradeboardUsage)
		return err
	}
}

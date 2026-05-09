package catalog

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	caldriver "odin-os/internal/adapters/calendar"
	webdriver "odin-os/internal/adapters/web"
	"odin-os/internal/tools/invocation"
)

func BuiltinDefinitions() map[string]ToolDefinition {
	definitions := []ToolDefinition{
		{
			Key:        "project_status",
			Title:      "Project Status",
			Summary:    "Summarizes managed project status for planning.",
			Scopes:     []string{"global", "project", "odin-core"},
			Tags:       []string{"project", "status"},
			CostHint:   CostHintLow,
			BudgetCost: 1,
			SourceRef:  "builtin://project_status",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project_key": map[string]any{"type": "string"},
				},
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				projectKey := input["project_key"]
				if projectKey == "" {
					projectKey = "current"
				}
				return StructuredResult{
					CapabilityKey:   "project_status",
					Summary:         fmt.Sprintf("Project status prepared for %s.", projectKey),
					KeyFacts:        map[string]string{"project_key": projectKey},
					FollowOnOptions: []string{"expand skill", "inspect tasks"},
					RawRef:          "builtin://project_status/result",
					RawOutput:       fmt.Sprintf("project=%s status=ready", projectKey),
				}, nil
			},
		},
		{
			Key:        "google_calendar_off_dates",
			Title:      "Google Calendar Off Dates",
			Summary:    "Reads live off-dates for the requested PBS bid period from Google Calendar.",
			Scopes:     []string{"project", "odin-core"},
			Tags:       []string{"calendar", "pbs", "live"},
			CostHint:   CostHintMedium,
			BudgetCost: 2,
			SourceRef:  "builtin://google_calendar_off_dates",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"bid_period":  map[string]any{"type": "string"},
					"calendar_id": map[string]any{"type": "string"},
					"timezone":    map[string]any{"type": "string"},
				},
				"required": []string{"bid_period"},
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				bidPeriod, err := requiredString(input, "bid_period")
				if err != nil {
					return StructuredResult{}, err
				}

				result, err := invocation.Service{}.GoogleCalendarOffDates(context.Background(), caldriver.Request{
					ToolKey: "google_calendar_off_dates",
					Input: caldriver.Input{
						BidPeriod:  bidPeriod,
						CalendarID: defaultString(input["calendar_id"], "primary"),
						Timezone:   defaultString(input["timezone"], "America/Chicago"),
					},
				})
				if err != nil {
					return StructuredResult{}, err
				}

				offDates := stringSlice(result.Artifacts, "off_dates")
				return StructuredResult{
					CapabilityKey: "google_calendar_off_dates",
					Summary:       result.Summary,
					Artifacts: []string{
						"bid_period=" + stringValue(result.Artifacts, "bid_period"),
						"calendar_id=" + stringValue(result.Artifacts, "calendar_id"),
						"timezone=" + stringValue(result.Artifacts, "timezone"),
						"off_dates=" + strings.Join(offDates, ","),
					},
					KeyFacts: map[string]string{
						"bid_period":      stringValue(result.Artifacts, "bid_period"),
						"calendar_id":     stringValue(result.Artifacts, "calendar_id"),
						"timezone":        stringValue(result.Artifacts, "timezone"),
						"off_dates_count": strconv.Itoa(len(offDates)),
					},
					FollowOnOptions: []string{"invoke browser_pbs_session"},
					RawRef:          "builtin://google_calendar_off_dates/result",
					RawOutput:       result.RawOutput,
				}, nil
			},
		},
		{
			Key:          "browser_pbs_session",
			CanonicalKey: "browser_pbs_session",
			Aliases:      []string{"huginn_pbs_session"},
			Title:        "Browser PBS Session",
			Summary:      "Validates the live trusted browser session for the PBS May bid workflow.",
			Scopes:       []string{"project", "odin-core"},
			Tags:         []string{"browser", "pbs", "live"},
			CostHint:     CostHintMedium,
			BudgetCost:   2,
			SourceRef:    "builtin://browser_pbs_session",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"bid_period":   map[string]any{"type": "string"},
					"workflow_key": map[string]any{"type": "string"},
					"timezone":     map[string]any{"type": "string"},
				},
				"required": []string{"bid_period", "workflow_key"},
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				bidPeriod, err := requiredString(input, "bid_period")
				if err != nil {
					return StructuredResult{}, err
				}
				workflowKey, err := requiredString(input, "workflow_key")
				if err != nil {
					return StructuredResult{}, err
				}

				result, err := invocation.Service{}.HuginnPBSSession(context.Background(), webdriver.Request{
					ToolKey: "browser_pbs_session",
					Input: webdriver.Input{
						BidPeriod:   bidPeriod,
						WorkflowKey: workflowKey,
						Timezone:    defaultString(input["timezone"], "America/Chicago"),
					},
				})
				if err != nil {
					return StructuredResult{}, err
				}

				evidence := stringSlice(result.Artifacts, "evidence")
				return StructuredResult{
					CapabilityKey: "browser_pbs_session",
					Summary:       result.Summary,
					Artifacts: []string{
						"bid_period=" + stringValue(result.Artifacts, "bid_period"),
						"workflow_key=" + stringValue(result.Artifacts, "workflow_key"),
						"session_state=" + stringValue(result.Artifacts, "session_state"),
						"session_id=" + stringValue(result.Artifacts, "session_id"),
						"evidence=" + strings.Join(evidence, ","),
					},
					KeyFacts: map[string]string{
						"bid_period":     stringValue(result.Artifacts, "bid_period"),
						"workflow_key":   stringValue(result.Artifacts, "workflow_key"),
						"session_state":  stringValue(result.Artifacts, "session_state"),
						"session_id":     stringValue(result.Artifacts, "session_id"),
						"evidence_count": strconv.Itoa(len(evidence)),
					},
					FollowOnOptions: []string{"invoke google_calendar_off_dates"},
					RawRef:          "builtin://browser_pbs_session/result",
					RawOutput:       result.RawOutput,
				}, nil
			},
		},
		{
			Key:          "browser_visual_audit",
			CanonicalKey: "browser_visual_audit",
			Aliases:      []string{"huginn_visual_audit"},
			Title:        "Browser Visual Audit",
			Summary:      "Captures a live browser snapshot and screenshot for a visual review target.",
			Scopes:       []string{"global", "project", "odin-core"},
			Tags:         []string{"browser", "visual", "live"},
			CostHint:     CostHintMedium,
			BudgetCost:   2,
			SourceRef:    "builtin://browser_visual_audit",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target_url":         map[string]any{"type": "string"},
					"label":              map[string]any{"type": "string"},
					"screenshot_path":    map[string]any{"type": "string"},
					"wait_ms":            map[string]any{"type": "string"},
					"allow_private_host": map[string]any{"type": "string"},
					"headless":           map[string]any{"type": "string"},
				},
				"required": []string{"target_url"},
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				targetURL, err := requiredString(input, "target_url")
				if err != nil {
					return StructuredResult{}, err
				}

				result, err := invocation.Service{}.HuginnVisualAudit(context.Background(), webdriver.VisualRequest{
					ToolKey: "browser_visual_audit",
					Input: webdriver.VisualInput{
						TargetURL:        targetURL,
						Label:            defaultString(input["label"], "visual-audit"),
						ScreenshotPath:   input["screenshot_path"],
						WaitMS:           defaultString(input["wait_ms"], "2000"),
						AllowPrivateHost: defaultString(input["allow_private_host"], "false"),
						Headless:         defaultString(input["headless"], "true"),
					},
				})
				if err != nil {
					return StructuredResult{}, err
				}

				return StructuredResult{
					CapabilityKey: "browser_visual_audit",
					Summary:       result.Summary,
					Artifacts: []string{
						"target_url=" + stringValue(result.Artifacts, "target_url"),
						"final_url=" + stringValue(result.Artifacts, "final_url"),
						"title=" + stringValue(result.Artifacts, "title"),
						"label=" + stringValue(result.Artifacts, "label"),
						"screenshot_path=" + stringValue(result.Artifacts, "screenshot_path"),
						"snapshot_excerpt=" + stringValue(result.Artifacts, "snapshot_excerpt"),
					},
					KeyFacts: map[string]string{
						"target_url":      stringValue(result.Artifacts, "target_url"),
						"final_url":       stringValue(result.Artifacts, "final_url"),
						"title":           stringValue(result.Artifacts, "title"),
						"label":           stringValue(result.Artifacts, "label"),
						"screenshot_path": stringValue(result.Artifacts, "screenshot_path"),
					},
					FollowOnOptions: []string{"run task with captured evidence"},
					RawRef:          "builtin://browser_visual_audit/result",
					RawOutput:       result.RawOutput,
				}, nil
			},
		},
		{
			Key:          "browser_x_post_visible_evidence",
			CanonicalKey: "browser_x_post_visible_evidence",
			Aliases:      []string{"huginn_x_post_visible_evidence"},
			Title:        "Browser X Post Visible Evidence",
			Summary:      "Captures read-only visible-page evidence for a specific X post URL through Browser Control.",
			Scopes:       []string{"global", "project", "odin-core"},
			Tags:         []string{"browser", "x", "social", "live"},
			CostHint:     CostHintMedium,
			BudgetCost:   2,
			SourceRef:    "builtin://browser_x_post_visible_evidence",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target_url":      map[string]any{"type": "string"},
					"label":           map[string]any{"type": "string"},
					"screenshot_path": map[string]any{"type": "string"},
					"wait_ms":         map[string]any{"type": "string"},
					"headless":        map[string]any{"type": "string"},
				},
				"required": []string{"target_url"},
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				targetURL, err := requiredString(input, "target_url")
				if err != nil {
					return StructuredResult{}, err
				}

				result, err := invocation.Service{}.HuginnXPostVisibleEvidence(context.Background(), webdriver.XPostRequest{
					ToolKey: "browser_x_post_visible_evidence",
					Input: webdriver.XPostInput{
						TargetURL:      targetURL,
						Label:          defaultString(input["label"], "x-post-evidence"),
						ScreenshotPath: input["screenshot_path"],
						WaitMS:         defaultString(input["wait_ms"], "2000"),
						Headless:       defaultString(input["headless"], "false"),
					},
				})
				if err != nil {
					return StructuredResult{}, err
				}

				return StructuredResult{
					CapabilityKey: "browser_x_post_visible_evidence",
					Summary:       result.Summary,
					Artifacts: []string{
						"target_url=" + stringValue(result.Artifacts, "target_url"),
						"final_url=" + stringValue(result.Artifacts, "final_url"),
						"label=" + stringValue(result.Artifacts, "label"),
						"screenshot_path=" + stringValue(result.Artifacts, "screenshot_path"),
						"snapshot_path=" + stringValue(result.Artifacts, "snapshot_path"),
						"author_handle=" + stringValue(result.Artifacts, "author_handle"),
						"reply_count=" + stringValue(result.Artifacts, "reply_count"),
						"repost_count=" + stringValue(result.Artifacts, "repost_count"),
						"like_count=" + stringValue(result.Artifacts, "like_count"),
						"bookmark_count=" + stringValue(result.Artifacts, "bookmark_count"),
						"view_count=" + stringValue(result.Artifacts, "view_count"),
					},
					KeyFacts: map[string]string{
						"target_url":      stringValue(result.Artifacts, "target_url"),
						"final_url":       stringValue(result.Artifacts, "final_url"),
						"author_handle":   stringValue(result.Artifacts, "author_handle"),
						"reply_count":     stringValue(result.Artifacts, "reply_count"),
						"repost_count":    stringValue(result.Artifacts, "repost_count"),
						"like_count":      stringValue(result.Artifacts, "like_count"),
						"bookmark_count":  stringValue(result.Artifacts, "bookmark_count"),
						"view_count":      stringValue(result.Artifacts, "view_count"),
						"screenshot_path": stringValue(result.Artifacts, "screenshot_path"),
						"snapshot_path":   stringValue(result.Artifacts, "snapshot_path"),
					},
					FollowOnOptions: []string{"review recent social evidence"},
					MemoryRecords:   []MemoryRecord{socialEvidenceMemoryRecord(result.Summary, result.Artifacts)},
					RawRef:          "builtin://browser_x_post_visible_evidence/result",
					RawOutput:       result.RawOutput,
				}, nil
			},
		},
		{
			Key:              "browser_x_post_publish",
			CanonicalKey:     "browser_x_post_publish",
			Aliases:          []string{"huginn_x_post_publish"},
			Title:            "Browser X Post Publish",
			Summary:          "Publishes a single X post or reply through Browser Control using the operator's live session.",
			Scopes:           []string{"global", "project", "odin-core"},
			Tags:             []string{"browser", "x", "social", "live"},
			CostHint:         CostHintMedium,
			BudgetCost:       3,
			SourceRef:        "builtin://browser_x_post_publish",
			RequiresApproval: true,
			ApprovalReason:   "public social publishing requires an approved social_outcome",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"post_text":       map[string]any{"type": "string"},
					"content_kind":    map[string]any{"type": "string"},
					"in_reply_to_url": map[string]any{"type": "string"},
					"label":           map[string]any{"type": "string"},
					"screenshot_path": map[string]any{"type": "string"},
					"wait_ms":         map[string]any{"type": "string"},
					"headless":        map[string]any{"type": "string"},
				},
				"required": []string{"post_text"},
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				postText, err := requiredString(input, "post_text")
				if err != nil {
					return StructuredResult{}, err
				}

				result, err := invocation.Service{}.HuginnXPostPublish(context.Background(), webdriver.XPublishRequest{
					ToolKey: "browser_x_post_publish",
					Input: webdriver.XPublishInput{
						PostText:       postText,
						ContentKind:    defaultString(input["content_kind"], "post"),
						InReplyToURL:   input["in_reply_to_url"],
						Label:          defaultString(input["label"], "x-post-publish"),
						ScreenshotPath: input["screenshot_path"],
						WaitMS:         defaultString(input["wait_ms"], "4000"),
						Headless:       defaultString(input["headless"], "false"),
					},
				})
				if err != nil {
					return StructuredResult{}, err
				}

				return StructuredResult{
					CapabilityKey: "browser_x_post_publish",
					Summary:       result.Summary,
					Artifacts: []string{
						"publish_url=" + stringValue(result.Artifacts, "publish_url"),
						"final_url=" + stringValue(result.Artifacts, "final_url"),
						"in_reply_to_url=" + stringValue(result.Artifacts, "in_reply_to_url"),
						"screenshot_path=" + stringValue(result.Artifacts, "screenshot_path"),
						"published_at=" + stringValue(result.Artifacts, "published_at"),
					},
					KeyFacts: map[string]string{
						"publish_url":     stringValue(result.Artifacts, "publish_url"),
						"final_url":       stringValue(result.Artifacts, "final_url"),
						"in_reply_to_url": stringValue(result.Artifacts, "in_reply_to_url"),
						"screenshot_path": stringValue(result.Artifacts, "screenshot_path"),
						"published_at":    stringValue(result.Artifacts, "published_at"),
					},
					FollowOnOptions: []string{"invoke browser_x_post_visible_evidence"},
					RawRef:          "builtin://browser_x_post_publish/result",
					RawOutput:       result.RawOutput,
				}, nil
			},
		},
		{
			Key:          "browser_x_weekly_evidence_bundle",
			CanonicalKey: "browser_x_weekly_evidence_bundle",
			Aliases:      []string{"huginn_x_weekly_evidence_bundle"},
			Title:        "Browser X Weekly Evidence Bundle",
			Summary:      "Captures read-only visible-page evidence for several explicit X post URLs in one weekly bundle.",
			Scopes:       []string{"global", "project", "odin-core"},
			Tags:         []string{"browser", "x", "social", "weekly"},
			CostHint:     CostHintMedium,
			BudgetCost:   3,
			SourceRef:    "builtin://browser_x_weekly_evidence_bundle",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target_urls": map[string]any{"type": "string"},
					"label":       map[string]any{"type": "string"},
					"wait_ms":     map[string]any{"type": "string"},
					"headless":    map[string]any{"type": "string"},
				},
				"required": []string{"target_urls"},
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				targetURLsRaw, err := requiredString(input, "target_urls")
				if err != nil {
					return StructuredResult{}, err
				}
				targetURLs, err := parseExplicitXPostURLs(targetURLsRaw)
				if err != nil {
					return StructuredResult{}, err
				}

				bundleLabel := defaultString(input["label"], "x-weekly-evidence-bundle")
				waitMS := defaultString(input["wait_ms"], "2000")
				headless := defaultString(input["headless"], "false")

				memoryRecords := make([]MemoryRecord, 0, len(targetURLs))
				artifacts := make([]string, 0, len(targetURLs)+1)
				artifacts = append(artifacts, "bundle_label="+bundleLabel)
				failures := 0

				for index, targetURL := range targetURLs {
					result, err := invocation.Service{}.HuginnXPostVisibleEvidence(context.Background(), webdriver.XPostRequest{
						ToolKey: "browser_x_post_visible_evidence",
						Input: webdriver.XPostInput{
							TargetURL: targetURL,
							Label:     bundleLabel,
							WaitMS:    waitMS,
							Headless:  headless,
						},
					})
					if err != nil {
						failures++
						artifacts = append(artifacts, fmt.Sprintf("post_%d=failed target_url=%s", index+1, targetURL))
						continue
					}

					record := socialEvidenceMemoryRecord(result.Summary, result.Artifacts)
					record.Fields["bundle_label"] = bundleLabel
					record.Fields["bundle_position"] = strconv.Itoa(index + 1)
					memoryRecords = append(memoryRecords, record)
					artifacts = append(artifacts, fmt.Sprintf("post_%d=recorded target_url=%s", index+1, targetURL))
				}

				return StructuredResult{
					CapabilityKey: "browser_x_weekly_evidence_bundle",
					Summary:       fmt.Sprintf("Captured visible X evidence for %d of %d requested posts.", len(memoryRecords), len(targetURLs)),
					Artifacts:     artifacts,
					KeyFacts: map[string]string{
						"attempted_urls":    strconv.Itoa(len(targetURLs)),
						"recorded_evidence": strconv.Itoa(len(memoryRecords)),
						"failed_urls":       strconv.Itoa(failures),
					},
					FollowOnOptions: []string{"review recent social evidence"},
					MemoryRecords:   memoryRecords,
					RawRef:          "builtin://browser_x_weekly_evidence_bundle/result",
					RawOutput:       strings.Join(artifacts, "\n"),
				}, nil
			},
		},
		{
			Key:        "task_list",
			Title:      "Task List",
			Summary:    "Lists task projections for the requested scope.",
			Scopes:     []string{"global", "project", "odin-core", "new-project"},
			Tags:       []string{"runtime", "tasks"},
			CostHint:   CostHintLow,
			BudgetCost: 1,
			SourceRef:  "builtin://task_list",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope": map[string]any{"type": "string"},
				},
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				scope := input["scope"]
				if scope == "" {
					scope = "global"
				}
				return StructuredResult{
					CapabilityKey:   "task_list",
					Summary:         fmt.Sprintf("Task list prepared for %s scope.", scope),
					KeyFacts:        map[string]string{"scope": scope},
					FollowOnOptions: []string{"expand sub-agent", "invoke event_log"},
					RawRef:          "builtin://task_list/result",
					RawOutput:       fmt.Sprintf("scope=%s tasks=0", scope),
				}, nil
			},
		},
		{
			Key:        "event_log",
			Title:      "Event Log",
			Summary:    "Retrieves recent audit event summaries.",
			Scopes:     []string{"global", "project", "odin-core", "new-project"},
			Tags:       []string{"runtime", "events"},
			CostHint:   CostHintMedium,
			BudgetCost: 2,
			SourceRef:  "builtin://event_log",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer"},
				},
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				limit := input["limit"]
				if limit == "" {
					limit = "10"
				}
				return StructuredResult{
					CapabilityKey:   "event_log",
					Summary:         fmt.Sprintf("Event log prepared with limit %s.", limit),
					KeyFacts:        map[string]string{"limit": limit},
					FollowOnOptions: []string{"invoke task_list"},
					RawRef:          "builtin://event_log/result",
					RawOutput:       fmt.Sprintf("limit=%s events=0", limit),
				}, nil
			},
		},
	}

	index := make(map[string]ToolDefinition, len(definitions)*2)
	for _, definition := range definitions {
		indexToolDefinition(index, definition)
	}
	return index
}

func indexToolDefinition(index map[string]ToolDefinition, definition ToolDefinition) {
	if definition.CanonicalKey == "" {
		definition.CanonicalKey = definition.Key
	}
	definition = cloneToolDefinition(definition)
	setIndexedToolDefinition(index, definition.Key, definition)

	for _, alias := range definition.Aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" || alias == definition.Key {
			continue
		}

		aliasDefinition := cloneToolDefinition(definition)
		aliasDefinition.Key = alias
		aliasDefinition.Hidden = true
		aliasDefinition.Aliases = nil
		setIndexedToolDefinition(index, alias, aliasDefinition)
	}
}

func setIndexedToolDefinition(index map[string]ToolDefinition, key string, definition ToolDefinition) {
	if _, exists := index[key]; exists {
		panic(fmt.Sprintf("duplicate tool definition key %q", key))
	}
	index[key] = definition
}

func cloneToolDefinition(definition ToolDefinition) ToolDefinition {
	cloned := definition
	cloned.Aliases = append([]string(nil), definition.Aliases...)
	cloned.Scopes = append([]string(nil), definition.Scopes...)
	cloned.Tags = append([]string(nil), definition.Tags...)
	cloned.Schema = cloneAnyMap(definition.Schema)
	return cloned
}

func cloneAnyMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneAnyValue(value)
	}
	return cloned
}

func cloneAnySlice(values []any) []any {
	if values == nil {
		return nil
	}
	cloned := make([]any, len(values))
	for index, value := range values {
		cloned[index] = cloneAnyValue(value)
	}
	return cloned
}

func cloneStringSlice(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		return cloneAnySlice(typed)
	case []string:
		return cloneStringSlice(typed)
	default:
		return typed
	}
}

func stringValue(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok {
		return ""
	}
	if value, ok := raw.(string); ok {
		return value
	}
	return fmt.Sprint(raw)
}

func stringSlice(values map[string]any, key string) []string {
	raw, ok := values[key]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return []string{fmt.Sprint(value)}
	}
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func requiredString(input map[string]string, key string) (string, error) {
	value := strings.TrimSpace(input[key])
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func socialEvidenceMemoryRecord(summary string, artifacts map[string]any) MemoryRecord {
	fields := memoryFieldsFromArtifacts(artifacts, []string{
		"target_url",
		"final_url",
		"label",
		"screenshot_path",
		"snapshot_path",
		"author_display_name",
		"author_handle",
		"reply_count",
		"repost_count",
		"like_count",
		"bookmark_count",
		"view_count",
		"post_text",
		"snapshot_excerpt",
	})
	fields["channel"] = "x"
	fields["evidence_kind"] = "x_post_visible"

	return MemoryRecord{
		MemoryType: "social_evidence",
		Summary:    strings.TrimSpace(summary),
		Fields:     fields,
	}
}

func memoryFieldsFromArtifacts(artifacts map[string]any, keys []string) map[string]string {
	fields := make(map[string]string, len(keys))
	for _, key := range keys {
		if value := strings.TrimSpace(stringValue(artifacts, key)); value != "" {
			fields[key] = value
		}
	}
	return fields
}

func parseExplicitXPostURLs(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	urls := make([]string, 0, len(parts))
	for _, part := range parts {
		candidate := strings.TrimSpace(part)
		if candidate == "" {
			continue
		}
		if !isAllowedXPostURL(candidate) {
			return nil, fmt.Errorf("target_urls must contain only allowed X post URLs")
		}
		key := normalizeURLKey(candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		urls = append(urls, candidate)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("target_urls is required")
	}
	return urls, nil
}

func isAllowedXPostURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	switch host {
	case "x.com", "www.x.com", "twitter.com", "www.twitter.com":
		return parsed.Scheme == "http" || parsed.Scheme == "https"
	default:
		return false
	}
}

func normalizeURLKey(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	return parsed.String()
}

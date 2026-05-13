package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"odin-os/internal/adapters/web"
	"odin-os/internal/app/bootstrap"
	commands "odin-os/internal/cli/commands"
	scope "odin-os/internal/cli/scope"
	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/projects"
	"odin-os/internal/skills"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tools/invocation"
)

const (
	designUsage               = "usage: odin design invoke <skill-key> [--input <json>] [--json] | odin design request create --brief <path> [--json] | odin design artifacts [--json] | odin design artifact show <id> [--json] | odin design artifact review <accept|reject|archive> <id> [--json] | odin design status [--json] | odin design skills list [--json] | odin design systems list [--json]"
	designArtifactType        = "design_output"
	designRequestArtifactType = "design_request"
	designArtifactQueue       = "needs_review"
	designRequestQueue        = "review_required"
)

type designArtifactListView struct {
	Items []skills.ReviewArtifact `json:"items"`
}

type designStatusView struct {
	Total               int            `json:"total"`
	Requests            int            `json:"requests"`
	Outputs             int            `json:"outputs"`
	ByType              map[string]int `json:"by_type"`
	ByStatus            map[string]int `json:"by_status"`
	Pending             int            `json:"pending"`
	Accepted            int            `json:"accepted"`
	Rejected            int            `json:"rejected"`
	Archived            int            `json:"archived"`
	Reviewable          int            `json:"reviewable"`
	ExecutionType       string         `json:"execution_type"`
	ODDataDir           string         `json:"od_data_dir"`
	OpenDesignAvailable bool           `json:"open_design_available"`
	OpenDesignDriver    string         `json:"open_design_driver"`
	OpenDesignError     string         `json:"open_design_error"`
}

type designSkillListView struct {
	Items []designSkillSummary `json:"items"`
}

type designSkillSummary struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Version   string `json:"version"`
	Summary   string `json:"summary"`
	Status    string `json:"status"`
	SourceKey string `json:"source_key,omitempty"`
}

type designSystemsView struct {
	Systems []string `json:"systems"`
}

func runDesign(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) == 0 || strings.EqualFold(remaining[0], "help") || strings.EqualFold(remaining[0], "--help") {
		_, err := fmt.Fprintln(stdout, designUsage)
		return err
	}

	switch strings.ToLower(strings.TrimSpace(remaining[0])) {
	case "invoke":
		if len(remaining) < 2 {
			return fmt.Errorf("usage: odin design invoke <skill-key> [--input <json>] [--json]")
		}
		return runDesignInvoke(ctx, app, strings.TrimSpace(remaining[1]), remaining[2:], jsonOutput, stdout)
	case "request":
		if len(remaining) < 3 || !strings.EqualFold(remaining[1], "create") {
			return fmt.Errorf("usage: odin design request create --brief <path> [--json]")
		}
		return runDesignRequestCreate(ctx, app, remaining[2:], jsonOutput, stdout)
	case "status":
		if len(remaining) != 1 {
			return fmt.Errorf("usage: odin design status [--json]")
		}
		return runDesignStatus(ctx, app, jsonOutput, stdout)
	case "skills":
		if len(remaining) != 2 || !strings.EqualFold(remaining[1], "list") {
			return fmt.Errorf("usage: odin design skills list [--json]")
		}
		return runDesignSkillsList(ctx, app, jsonOutput, stdout)
	case "systems":
		if len(remaining) != 2 || !strings.EqualFold(remaining[1], "list") {
			return fmt.Errorf("usage: odin design systems list [--json]")
		}
		return runDesignSystemsList(ctx, app, jsonOutput, stdout)
	case "artifacts":
		return runDesignArtifacts(ctx, app, jsonOutput, stdout)
	case "artifact":
		if len(remaining) == 4 && strings.TrimSpace(remaining[1]) == "review" {
			return runDesignArtifactReview(ctx, app, strings.TrimSpace(remaining[2]), strings.TrimSpace(remaining[3]), jsonOutput, stdout)
		}
		if len(remaining) != 3 || strings.TrimSpace(remaining[1]) != "show" {
			return fmt.Errorf("usage: odin design artifact show <id> [--json]")
		}
		return runDesignArtifactShow(ctx, app, remaining[2], jsonOutput, stdout)
	default:
		return fmt.Errorf("usage: %s", designUsage)
	}
}

func runDesignStatus(ctx context.Context, app bootstrap.App, jsonOutput bool, stdout io.Writer) error {
	if app.Store == nil {
		return fmt.Errorf("design status requires runtime store")
	}

	artifacts, err := app.Store.ListSkillArtifacts(ctx, sqlite.ListSkillArtifactsParams{})
	if err != nil {
		return err
	}
	view := designStatusView{
		ByType:              make(map[string]int),
		ByStatus:            make(map[string]int),
		ExecutionType:       "human_gated",
		ODDataDir:           strings.TrimSpace(os.Getenv("OD_DATA_DIR")),
		OpenDesignDriver:    "",
		OpenDesignAvailable: false,
		OpenDesignError:     "",
	}
	view.OpenDesignDriver, view.OpenDesignAvailable, view.OpenDesignError = openDesignDriverStatus()
	if view.OpenDesignAvailable && strings.TrimSpace(view.OpenDesignError) == "" {
		view.OpenDesignError = "ok"
	}

	for _, artifact := range artifacts {
		if !isDesignArtifactType(artifact.ArtifactType) {
			continue
		}
		view.Total++
		view.ByType[artifact.ArtifactType]++
		view.ByStatus[artifact.Status]++
		switch strings.TrimSpace(strings.ToLower(artifact.Status)) {
		case "accepted":
			view.Accepted++
		case "rejected":
			view.Rejected++
		case "archived":
			view.Archived++
		}
		if isDesignRequestArtifactType(artifact.ArtifactType) {
			view.Requests++
			if isDesignRequestQueueStatus(artifact.Status) {
				view.Pending++
				view.Reviewable++
			}
			continue
		}
		view.Outputs++
		if isReviewQueueDesignArtifactStatus(artifact.Status) && !isDesignRequestQueueStatus(artifact.Status) {
			view.Pending++
			view.Reviewable++
		}
	}

	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}

	if view.Total == 0 {
		_, err := fmt.Fprintln(stdout, "design status: no design artifacts")
		return err
	}

	openDesignStatus := "unavailable"
	if view.OpenDesignAvailable {
		openDesignStatus = "available"
		if view.OpenDesignError == "" {
			view.OpenDesignError = "ok"
		}
	}
	_, err = fmt.Fprintf(stdout, "design_status total=%d requests=%d outputs=%d pending=%d reviewable=%d accepted=%d rejected=%d archived=%d execution_type=%s open_design=%s open_design_driver=%s open_design_error=%s od_data_dir=%s\n",
		view.Total, view.Requests, view.Outputs, view.Pending, view.Reviewable, view.Accepted, view.Rejected, view.Archived, view.ExecutionType, openDesignStatus, view.OpenDesignDriver, view.OpenDesignError, view.ODDataDir)
	return err
}

func runDesignSkillsList(ctx context.Context, app bootstrap.App, jsonOutput bool, stdout io.Writer) error {
	designSkills, err := openDesignSkills(ctx, app)
	if err != nil {
		return err
	}

	if jsonOutput {
		return commands.WriteJSON(stdout, designSkillListView{Items: designSkills})
	}

	if len(designSkills) == 0 {
		_, err := fmt.Fprintln(stdout, "no design skills")
		return err
	}
	for _, skill := range designSkills {
		if _, err := fmt.Fprintf(stdout, "design_skill key=%s version=%s title=%s\n", skill.Key, skill.Version, skill.Title); err != nil {
			return err
		}
	}
	return nil
}

func runDesignSystemsList(ctx context.Context, app bootstrap.App, jsonOutput bool, stdout io.Writer) error {
	systemSet, err := openDesignSystems(ctx, app)
	if err != nil {
		return err
	}
	systems := systemSet

	if jsonOutput {
		return commands.WriteJSON(stdout, designSystemsView{Systems: systems})
	}
	if len(systems) == 0 {
		_, err := fmt.Fprintln(stdout, "no design systems")
		return err
	}
	for _, system := range systems {
		if _, err := fmt.Fprintln(stdout, system); err != nil {
			return err
		}
	}
	return nil
}

func runDesignArtifacts(ctx context.Context, app bootstrap.App, jsonOutput bool, stdout io.Writer) error {
	if app.Store == nil {
		return fmt.Errorf("design artifacts require runtime store")
	}
	artifacts, err := app.Store.ListSkillArtifacts(ctx, sqlite.ListSkillArtifactsParams{})
	if err != nil {
		return err
	}

	views := make([]skills.ReviewArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if !isDesignArtifactType(artifact.ArtifactType) {
			continue
		}
		views = append(views, renderSkillReviewArtifact(artifact))
	}

	if jsonOutput {
		return commands.WriteJSON(stdout, designArtifactListView{Items: views})
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(stdout, "no design artifacts")
		return err
	}
	for _, artifact := range views {
		if _, err := fmt.Fprintf(stdout, "design_artifact id=%d skill=%s status=%s summary=%s\n", artifact.ID, artifact.SkillKey, artifact.Status, artifact.Summary); err != nil {
			return err
		}
	}
	return nil
}

func runDesignArtifactShow(ctx context.Context, app bootstrap.App, artifactRef string, jsonOutput bool, stdout io.Writer) error {
	if app.Store == nil {
		return fmt.Errorf("design artifact show requires runtime store")
	}
	artifactID, err := strconv.ParseInt(strings.TrimSpace(artifactRef), 10, 64)
	if err != nil || artifactID <= 0 {
		return fmt.Errorf("design artifact id must be a positive integer")
	}
	artifact, err := app.Store.GetSkillArtifact(ctx, artifactID)
	if err != nil {
		return err
	}
	if !isDesignArtifactType(artifact.ArtifactType) {
		return fmt.Errorf("artifact %d is not a design artifact", artifactID)
	}
	view := renderSkillReviewArtifact(artifact)
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "design_artifact=%d skill=%s status=%s type=%s summary=%s\n", view.ID, view.SkillKey, view.Status, view.ArtifactType, view.Summary)
	return err
}

func runDesignRequestCreate(ctx context.Context, app bootstrap.App, args []string, jsonOutput bool, stdout io.Writer) error {
	if app.Store == nil {
		return fmt.Errorf("design request create requires runtime store")
	}

	briefPath, err := consumeFlagValue(args, "--brief")
	if err != nil {
		return err
	}
	if strings.TrimSpace(briefPath) == "" {
		return fmt.Errorf("usage: odin design request create --brief <path> [--json]")
	}

	briefPath = filepath.Clean(briefPath)
	briefPayload, err := os.ReadFile(briefPath)
	if err != nil {
		return err
	}
	trimmedBrief := strings.TrimSpace(string(briefPayload))
	if trimmedBrief == "" {
		return fmt.Errorf("design brief file is empty: %s", briefPath)
	}

	payloadData := map[string]any{
		"brief":      trimmedBrief,
		"brief_path": briefPath,
	}
	summary := ""
	if err := json.Unmarshal(briefPayload, &payloadData); err == nil {
		summary = designStringField(payloadData, "summary")
		if summary == "" {
			summary = designStringField(payloadData, "title")
		}
		if designStringField(payloadData, "skill_key") == "" {
			payloadData["request_brief_text"] = trimmedBrief
		}
	} else {
		payloadData = map[string]any{
			"brief":      trimmedBrief,
			"brief_path": briefPath,
		}
	}

	if summary == "" {
		summary = firstNonBlank(strings.SplitN(trimmedBrief, "\n", 2)[0], "design request")
	}

	invocationContext, err := resolveDesignInvocationContext(ctx, app)
	if err != nil {
		return err
	}

	artifactPermissions, err := artifactPermissionsFromPayload(payloadData)
	if err != nil {
		return err
	}

	skillKey := designStringField(payloadData, "skill_key")
	if skillKey == "" {
		skillKey = "design-request"
	}

	artifact, err := app.Store.CreateSkillArtifact(ctx, sqlite.CreateSkillArtifactParams{
		SkillKey:         skillKey,
		Scope:            invocationContext.ResolvedScopeKind,
		ProjectID:        projectIDFromInvocation(invocationContext),
		Status:           designRequestQueue,
		ArtifactType:     designRequestArtifactType,
		Summary:          summary,
		OutputJSON:       artifactOutputJSON(payloadData),
		RawOutput:        trimmedBrief,
		ExecutionProfile: "design_request",
		PermissionsJSON:  artifactPermissions,
	})
	if err != nil {
		return err
	}

	if err := app.Store.RecordDesignRequestCreatedEvent(ctx, sqlite.RecordDesignRequestCreatedEventParams{
		RequestArtifactID: artifact.ID,
		SkillKey:          artifact.SkillKey,
		Scope:             artifact.Scope,
		ProjectID:         artifact.ProjectID,
		Status:            artifact.Status,
		ArtifactType:      artifact.ArtifactType,
		Summary:           artifact.Summary,
		ExecutionProfile:  artifact.ExecutionProfile,
	}); err != nil {
		return err
	}

	view := renderSkillReviewArtifact(artifact)
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "design_request=%d skill=%s status=%s type=%s summary=%q\n", view.ID, view.SkillKey, view.Status, view.ArtifactType, view.Summary)
	return err
}

func runDesignInvoke(ctx context.Context, app bootstrap.App, skillKey string, args []string, jsonOutput bool, stdout io.Writer) error {
	if app.Store == nil {
		return fmt.Errorf("design invoke requires runtime store")
	}
	skillKey = strings.TrimSpace(skillKey)
	if skillKey == "" {
		return fmt.Errorf("design invoke requires a skill key")
	}

	invocationContext, err := resolveDesignInvocationContext(ctx, app)
	if err != nil {
		return err
	}

	service := skills.Service{
		RepoRoot:             app.RepoRoot,
		TransitionAuthorizer: projects.Service{Store: app.Store},
	}
	if err := assertDesignSkillExists(ctx, service, skillKey); err != nil {
		return err
	}

	inputValue, err := optionalFlagValue(args, "--input")
	if err != nil {
		return err
	}
	input, err := commands.DecodeSkillInput(inputValue)
	if err != nil {
		return err
	}

	requestPayload := map[string]any{
		"skill_key": skillKey,
	}
	if input != nil {
		requestPayload["input"] = input
	}

	reviewSummary := firstNonBlank(
		designStringField(input, "summary"),
		designStringField(input, "title"),
		skillKey,
	)
	artifactPermissions, err := artifactPermissionsFromPayload(requestPayload)
	if err != nil {
		return err
	}

	artifact, err := app.Store.CreateSkillArtifact(ctx, sqlite.CreateSkillArtifactParams{
		SkillKey:         skillKey,
		Scope:            invocationContext.ResolvedScopeKind,
		ProjectID:        projectIDFromInvocation(invocationContext),
		Status:           designRequestQueue,
		ArtifactType:     designRequestArtifactType,
		Summary:          reviewSummary,
		OutputJSON:       artifactOutputJSON(requestPayload),
		RawOutput:        strings.TrimSpace(inputValue),
		ExecutionProfile: "design_request",
		PermissionsJSON:  artifactPermissions,
	})
	if err != nil {
		return err
	}

	if err := app.Store.RecordDesignRequestCreatedEvent(ctx, sqlite.RecordDesignRequestCreatedEventParams{
		RequestArtifactID: artifact.ID,
		SkillKey:          artifact.SkillKey,
		Scope:             artifact.Scope,
		ProjectID:         artifact.ProjectID,
		Status:            artifact.Status,
		ArtifactType:      artifact.ArtifactType,
		Summary:           artifact.Summary,
		ExecutionProfile:  artifact.ExecutionProfile,
	}); err != nil {
		return err
	}

	view := renderSkillReviewArtifact(artifact)
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "design_request=%d skill=%s status=%s type=%s summary=%q\n", view.ID, view.SkillKey, view.Status, view.ArtifactType, view.Summary)
	return err
}

func resolveDesignInvocationContext(ctx context.Context, app bootstrap.App) (skills.InvocationContext, error) {
	state, err := loadCLIState(app)
	if err != nil {
		return skills.InvocationContext{}, err
	}
	stateKind := strings.TrimSpace(string(state.Scope.Kind))
	if stateKind == "" {
		stateKind = "repo"
	}
	invocationContext := skills.InvocationContext{
		ResolvedScopeKind: stateKind,
	}
	if state.Scope.Kind == scope.ScopeProject || state.Scope.Kind == scope.ScopeOdinCore {
		manifest, ok := app.Registry.Lookup(state.Scope.ProjectKey)
		if !ok {
			return skills.InvocationContext{}, fmt.Errorf("unknown project: %s", state.Scope.ProjectKey)
		}
		project, err := projects.Service{Store: app.Store}.RegisterManagedProject(ctx, manifest)
		if err != nil {
			return skills.InvocationContext{}, err
		}
		invocationContext.Project = &skills.InvocationProject{
			ID:            project.ID,
			Key:           project.Key,
			SystemProject: manifest.SystemProject,
		}
		invocationContext.Manifest = manifest
	}
	return invocationContext, nil
}

func projectIDFromInvocation(invocationContext skills.InvocationContext) *int64 {
	if invocationContext.Project == nil {
		return nil
	}
	return &invocationContext.Project.ID
}

func artifactSummary(response skills.InvokeResponse) string {
	summary := strings.TrimSpace(response.Summary)
	if summary != "" {
		return summary
	}
	if response.SkillKey != "" {
		return response.SkillKey
	}
	return "design artifact"
}

func artifactOutputJSON(values map[string]any) string {
	if len(values) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func artifactPermissionsJSON(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func artifactPermissionsFromPayload(payload map[string]any) (string, error) {
	rawPermissions, err := designPermissionsFromPayload(payload)
	if err != nil {
		return "", err
	}

	permissionValues, err := artifactPermissionsFromAnySlice(rawPermissions)
	if err != nil {
		return "", err
	}
	return artifactPermissionsJSON(permissionValues), nil
}

func artifactPermissionsFromAnySlice(rawPermissions []any) ([]string, error) {
	permissionEntries := make([]string, 0, len(rawPermissions))
	for _, value := range rawPermissions {
		permission, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("permission must be a string")
		}
		permission = strings.TrimSpace(permission)
		if permission != "" {
			normalized, err := designPermission(permission)
			if err != nil {
				return nil, err
			}
			permissionEntries = append(permissionEntries, normalized)
		}
	}
	if len(permissionEntries) == 0 {
		return []string{}, nil
	}
	uniqPermissionEntries := make([]string, 0, len(permissionEntries))
	seen := map[string]struct{}{}
	for _, permission := range permissionEntries {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			continue
		}
		if _, exists := seen[permission]; exists {
			continue
		}
		seen[permission] = struct{}{}
		uniqPermissionEntries = append(uniqPermissionEntries, permission)
	}
	if len(uniqPermissionEntries) == 0 {
		return []string{}, nil
	}
	permissionBytes, err := json.Marshal(uniqPermissionEntries)
	if err != nil {
		return nil, err
	}
	if string(permissionBytes) == "null" {
		return []string{}, nil
	}
	var normalized []string
	if err := json.Unmarshal(permissionBytes, &normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func designPermissionsFromPayload(payload map[string]any) ([]any, error) {
	permissions := make([]any, 0, 4)
	if payload == nil {
		return permissions, nil
	}

	if payloadPermissions, ok := payload["permissions"]; ok {
		raw, err := normalizePermissionsFromJSON(payloadPermissions)
		if err != nil {
			return nil, err
		}
		permissions = append(permissions, raw...)
	}

	if input, ok := payload["input"].(map[string]any); ok {
		if payloadPermissions, ok := input["permissions"]; ok {
			raw, err := normalizePermissionsFromJSON(payloadPermissions)
			if err != nil {
				return nil, err
			}
			permissions = append(permissions, raw...)
		}
	}

	return permissions, nil
}

func normalizePermissionsFromJSON(value any) ([]any, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("permissions must be an array")
	}
	return raw, nil
}

func designPermission(raw string) (string, error) {
	parsed, err := skills.ParsePermission(raw)
	if err != nil {
		return "", err
	}
	switch parsed.Kind {
	case skills.PermissionKindRepoMutateIsolated, skills.PermissionKindRepoMutateFull, skills.PermissionKindRepoMutateGovernance, skills.PermissionKindRepoMutateDestructive:
		return "", fmt.Errorf("permission %q is not allowed for design", raw)
	}
	return parsed.Raw, nil
}

func openDesignSkills(ctx context.Context, app bootstrap.App) ([]designSkillSummary, error) {
	artifacts, err := openDesignArtifacts(ctx, app, "list_skills")
	if err != nil {
		return nil, err
	}
	skillArtifacts, ok := artifacts["skills"]
	if !ok {
		return nil, fmt.Errorf("open design response missing skills")
	}
	skills := make([]designSkillSummary, 0, 8)
	for _, item := range toOpenDesignList(skillArtifacts) {
		skillSummary, ok := parseOpenDesignSkill(item)
		if !ok {
			continue
		}
		skills = append(skills, skillSummary)
	}
	sort.Slice(skills, func(i, j int) bool {
		return strings.ToLower(skills[i].Key) < strings.ToLower(skills[j].Key)
	})
	return skills, nil
}

func openDesignSystems(ctx context.Context, app bootstrap.App) ([]string, error) {
	artifacts, err := openDesignArtifacts(ctx, app, "list_systems")
	if err != nil {
		return nil, err
	}
	systemArtifacts, ok := artifacts["systems"]
	if !ok {
		return nil, fmt.Errorf("open design response missing systems")
	}
	systems, err := parseOpenDesignSystems(systemArtifacts)
	if err != nil {
		return nil, err
	}
	sort.Strings(systems)
	return systems, nil
}

func openDesignArtifacts(ctx context.Context, app bootstrap.App, action string) (map[string]any, error) {
	response, err := invocation.Service{RuntimeRoot: app.RuntimeRoot}.OpenDesign(ctx, web.OpenDesignRequest{
		ToolKey: "",
		Input: web.OpenDesignInput{
			ArtifactID: "",
			Action:     action,
		},
	})
	if err != nil {
		return nil, err
	}
	return response.Artifacts, nil
}

func parseOpenDesignSystems(raw any) ([]string, error) {
	switch typed := raw.(type) {
	case []any:
		systemValues := make([]string, 0, len(typed))
		for _, value := range typed {
			system, ok := value.(string)
			if !ok {
				continue
			}
			system = strings.TrimSpace(system)
			if system != "" {
				systemValues = append(systemValues, system)
			}
		}
		return dedupeStrings(systemValues), nil
	case map[string]any:
		systemValues := make([]string, 0, len(typed))
		for system, value := range typed {
			switch typedValue := value.(type) {
			case string:
				typedValue = strings.TrimSpace(typedValue)
				if typedValue != "" {
					systemValues = append(systemValues, typedValue)
				}
			default:
				system = strings.TrimSpace(system)
				if system != "" {
					systemValues = append(systemValues, system)
				}
			}
		}
		return dedupeStrings(systemValues), nil
	default:
		return nil, fmt.Errorf("systems must be an array")
	}
}

func toOpenDesignList(raw any) []any {
	switch typed := raw.(type) {
	case []any:
		return typed
	case map[string]any:
		items := make([]any, 0, len(typed))
		for key, value := range typed {
			if valueMap, ok := value.(map[string]any); ok {
				items = append(items, valueMap)
				continue
			}
			item := map[string]any{"key": key, "summary": fmt.Sprintf("%v", value)}
			items = append(items, item)
		}
		return items
	default:
		return nil
	}
}

func parseOpenDesignSkill(raw any) (designSkillSummary, bool) {
	typed, ok := raw.(map[string]any)
	if !ok {
		return designSkillSummary{}, false
	}
	key := strings.TrimSpace(designStringField(typed, "key"))
	if key == "" {
		return designSkillSummary{}, false
	}
	return designSkillSummary{
		Key:       key,
		Title:     designStringField(typed, "title"),
		Version:   designStringField(typed, "version"),
		Summary:   designStringField(typed, "summary"),
		Status:    designStringField(typed, "status"),
		SourceKey: designStringField(typed, "source_key"),
	}, true
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func assertDesignSkillExists(ctx context.Context, service skills.Service, skillKey string) error {
	allSkills, err := service.List(ctx)
	if err != nil {
		return err
	}

	for _, skill := range allSkills {
		if strings.EqualFold(strings.TrimSpace(skill.Key), strings.TrimSpace(skillKey)) {
			if !isDesignSkill(skill) {
				return fmt.Errorf("skill %q is not a design skill", skillKey)
			}
			return nil
		}
	}

	return fmt.Errorf("design skill %q not found", skillKey)
}

func artifactRecordPath(scopeState clistate.State) string {
	return scopeState.Scope.ProjectKey + "-" + string(scopeState.Scope.Kind)
}

func isDesignArtifactType(artifactType string) bool {
	return isDesignOutputArtifactType(artifactType) || isDesignRequestArtifactType(artifactType)
}

func isDesignOutputArtifactType(artifactType string) bool {
	return strings.EqualFold(strings.TrimSpace(artifactType), designArtifactType)
}

func isDesignRequestArtifactType(artifactType string) bool {
	return strings.EqualFold(strings.TrimSpace(artifactType), designRequestArtifactType)
}

func isDesignRequestQueueStatus(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), designRequestQueue)
}

func openDesignDriverStatus() (string, bool, string) {
	path := strings.TrimSpace(os.Getenv("ODIN_HUGINN_OPEN_DESIGN_DRIVER"))
	if path == "" {
		return "", false, "ODIN_HUGINN_OPEN_DESIGN_DRIVER is not set"
	}
	cleanPath := filepath.Clean(path)
	info, err := os.Stat(cleanPath)
	if err != nil {
		return cleanPath, false, err.Error()
	}
	if info.IsDir() {
		return cleanPath, false, "driver path is a directory"
	}
	if info.Mode()&0o111 == 0 {
		return cleanPath, false, "driver path is not executable"
	}
	return cleanPath, true, ""
}

func isDesignSystemCandidate(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "design")
}

func isDesignSkill(skill skills.Skill) bool {
	for _, tag := range skill.Tags {
		if isDesignSystemCandidate(tag) {
			return true
		}
	}
	for _, value := range skill.AppliesTo {
		if strings.Contains(strings.ToLower(value), "design") {
			return true
		}
	}
	if strings.Contains(strings.ToLower(skill.Key), "design") {
		return true
	}
	return false
}

func designStringField(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

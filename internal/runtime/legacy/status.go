package legacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const DefaultRoot = "/var/odin"
const DefaultScriptsRoot = "/home/orchestrator/odin-orchestrator/scripts/odin"

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\a]*(?:\a|\x1b\\)`)

type Status string

const (
	StatusHealthy  Status = "healthy"
	StatusDegraded Status = "degraded"
	StatusMissing  Status = "missing"
)

type Runner interface {
	CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error)
}

type CommandRunner struct{}

func (CommandRunner) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type Service struct {
	Root        string
	ScriptsRoot string
	Runner      Runner
	Now         func() time.Time
}

type CapabilityReport struct {
	GeneratedAt  time.Time         `json:"generated_at"`
	Root         string            `json:"root"`
	ScriptsRoot  string            `json:"scripts_root"`
	Summary      CapabilitySummary `json:"summary"`
	Capabilities []Capability      `json:"capabilities"`
	Warnings     []string          `json:"warnings,omitempty"`
}

type CapabilitySummary struct {
	Services          int `json:"services"`
	TaskRoutes        int `json:"task_routes"`
	RoleDefaults      int `json:"role_defaults"`
	Backends          int `json:"backends"`
	TaskTypes         int `json:"task_types"`
	ActiveTaskTypes   int `json:"active_task_types"`
	Schedules         int `json:"schedules"`
	EnabledSchedules  int `json:"enabled_schedules"`
	Tools             int `json:"tools"`
	CapabilityRecords int `json:"capability_records"`
}

type Capability struct {
	Key            string `json:"key"`
	Source         string `json:"source"`
	Owner          string `json:"owner"`
	Classification string `json:"classification"`
	Proof          string `json:"proof"`
	Detail         string `json:"detail,omitempty"`
}

type ScheduleCandidate struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Role        string         `json:"role,omitempty"`
	Cron        string         `json:"cron,omitempty"`
	Source      string         `json:"source,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
	Enabled     bool           `json:"enabled"`
	LegacyIndex int            `json:"legacy_index"`
}

type Report struct {
	Status      Status         `json:"status"`
	GeneratedAt time.Time      `json:"generated_at"`
	Root        string         `json:"root"`
	RootExists  bool           `json:"root_exists"`
	Services    []UnitStatus   `json:"services,omitempty"`
	Engine      EngineSummary  `json:"engine"`
	State       StateSummary   `json:"state"`
	Routing     RoutingSummary `json:"routing"`
	Checkpoints []Checkpoint   `json:"checkpoints,omitempty"`
	Tmux        []TmuxSession  `json:"tmux,omitempty"`
	Warnings    []string       `json:"warnings,omitempty"`
}

type UnitStatus struct {
	Scope       string `json:"scope"`
	Name        string `json:"name"`
	LoadState   string `json:"load_state"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
	Description string `json:"description,omitempty"`
}

type EngineSummary struct {
	Available    bool           `json:"available"`
	Path         string         `json:"path"`
	TaskCounts   map[string]int `json:"task_counts,omitempty"`
	RunCounts    map[string]int `json:"run_counts,omitempty"`
	ActiveLeases []LeaseSummary `json:"active_leases,omitempty"`
}

type LeaseSummary struct {
	ID            string `json:"id"`
	TaskID        string `json:"task_id"`
	WorkerID      string `json:"worker_id"`
	Status        string `json:"status"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	LastHeartbeat string `json:"last_heartbeat,omitempty"`
}

type StateSummary struct {
	Available            bool   `json:"available"`
	Path                 string `json:"path"`
	TasksThisSession     int    `json:"tasks_this_session,omitempty"`
	DispatchedTasksCount int    `json:"dispatched_tasks_count,omitempty"`
	ActiveRunsCount      int    `json:"active_runs_count,omitempty"`
}

type RoutingSummary struct {
	Available        bool   `json:"available"`
	Path             string `json:"path"`
	SystemDefault    string `json:"system_default,omitempty"`
	BackendCount     int    `json:"backend_count,omitempty"`
	TaskRoutingCount int    `json:"task_routing_count,omitempty"`
	RoleDefaultCount int    `json:"role_default_count,omitempty"`
}

type Checkpoint struct {
	WorkerID   string      `json:"worker_id"`
	TaskID     string      `json:"task_id"`
	LeaseID    string      `json:"lease_id,omitempty"`
	Backend    string      `json:"backend,omitempty"`
	TaskType   string      `json:"task_type,omitempty"`
	RepoRoot   string      `json:"repo_root,omitempty"`
	LogPath    string      `json:"log_path,omitempty"`
	StartAt    string      `json:"start_at,omitempty"`
	TTLSeconds int         `json:"ttl_seconds,omitempty"`
	Log        LogEvidence `json:"log"`
}

type LogEvidence struct {
	Path      string `json:"path,omitempty"`
	Exists    bool   `json:"exists"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	LastLine  string `json:"last_line,omitempty"`
}

type TmuxSession struct {
	Name     string `json:"name"`
	Attached bool   `json:"attached"`
}

func DefaultService() Service {
	return Service{
		Root:        envOrDefault("ODIN_LEGACY_ROOT", DefaultRoot),
		ScriptsRoot: envOrDefault("ODIN_LEGACY_SCRIPTS_ROOT", DefaultScriptsRoot),
		Runner:      CommandRunner{},
	}
}

func (service Service) WithDefaults() Service {
	if strings.TrimSpace(service.Root) == "" {
		service.Root = envOrDefault("ODIN_LEGACY_ROOT", DefaultRoot)
	}
	if strings.TrimSpace(service.ScriptsRoot) == "" {
		service.ScriptsRoot = envOrDefault("ODIN_LEGACY_SCRIPTS_ROOT", DefaultScriptsRoot)
	}
	if service.Runner == nil {
		service.Runner = CommandRunner{}
	}
	return service
}

func (service Service) Capabilities(ctx context.Context) (CapabilityReport, error) {
	service = service.WithDefaults()
	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	report := CapabilityReport{
		GeneratedAt: now,
		Root:        service.Root,
		ScriptsRoot: service.ScriptsRoot,
	}

	statusReport := Report{}
	service.collectSystemd(ctx, &statusReport)
	report.Summary.Services = len(statusReport.Services)
	for _, unit := range statusReport.Services {
		if capability, ok := capabilityFromService(unit.Name); ok {
			addCapability(&report, capability)
		}
	}
	report.Warnings = append(report.Warnings, statusReport.Warnings...)

	service.collectRoutingCapabilities(&report)
	service.collectTaskTypeCapabilities(&report)
	service.collectScheduleCapabilities(&report)
	service.collectToolCapabilities(&report)

	sort.Slice(report.Capabilities, func(i, j int) bool {
		if report.Capabilities[i].Source == report.Capabilities[j].Source {
			return report.Capabilities[i].Key < report.Capabilities[j].Key
		}
		return report.Capabilities[i].Source < report.Capabilities[j].Source
	})
	report.Summary.CapabilityRecords = len(report.Capabilities)
	return report, nil
}

func (service Service) Report(ctx context.Context) (Report, error) {
	service = service.WithDefaults()
	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	report := Report{
		Status:      StatusHealthy,
		GeneratedAt: now,
		Root:        service.Root,
		Engine: EngineSummary{
			Path: filepath.Join(service.Root, "engine.db"),
		},
		State: StateSummary{
			Path: filepath.Join(service.Root, "state.json"),
		},
		Routing: RoutingSummary{
			Path: filepath.Join(service.Root, "routing.json"),
		},
	}

	if info, err := os.Stat(service.Root); err == nil && info.IsDir() {
		report.RootExists = true
	} else if errors.Is(err, os.ErrNotExist) {
		report.Status = StatusMissing
	} else if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("legacy root stat failed: %v", err))
	}

	service.collectSystemd(ctx, &report)
	service.collectTmux(ctx, &report)
	service.collectEngine(ctx, &report)
	service.collectState(&report)
	service.collectRouting(&report)
	service.collectCheckpoints(&report)
	report.Status = report.derivedStatus()
	return report, nil
}

func (report Report) derivedStatus() Status {
	if !report.RootExists && len(report.Services) == 0 && !report.Engine.Available {
		return StatusMissing
	}
	if len(report.Warnings) > 0 {
		return StatusDegraded
	}
	for _, unit := range report.Services {
		if unit.ActiveState == "failed" || unit.SubState == "failed" || unit.LoadState == "not-found" {
			return StatusDegraded
		}
	}
	if report.Engine.RunCounts["running"] > 0 {
		return StatusDegraded
	}
	return StatusHealthy
}

func (service Service) collectSystemd(ctx context.Context, report *Report) {
	rootOutput, rootErr := service.Runner.CombinedOutput(ctx, "systemctl", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*")
	if rootErr != nil {
		report.Warnings = append(report.Warnings, commandWarning("systemctl root list-units", rootOutput, rootErr))
	} else {
		report.Services = append(report.Services, parseSystemdUnits("root", string(rootOutput))...)
	}

	userOutput, userErr := service.Runner.CombinedOutput(ctx, "systemctl", "--user", "list-units", "--all", "--type=service", "--type=timer", "--no-legend", "--plain", "odin*")
	if userErr != nil {
		text := strings.TrimSpace(string(userOutput))
		if text != "" && !strings.Contains(text, "No medium found") && !strings.Contains(text, "No such file or directory") {
			report.Warnings = append(report.Warnings, commandWarning("systemctl user list-units", userOutput, userErr))
		}
	} else {
		report.Services = append(report.Services, parseSystemdUnits("user", string(userOutput))...)
	}

	sort.Slice(report.Services, func(i, j int) bool {
		if report.Services[i].Scope == report.Services[j].Scope {
			return report.Services[i].Name < report.Services[j].Name
		}
		return report.Services[i].Scope < report.Services[j].Scope
	})
}

func parseSystemdUnits(scope string, output string) []UnitStatus {
	lines := strings.Split(output, "\n")
	units := make([]UnitStatus, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "0 loaded units listed") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		unit := UnitStatus{
			Scope:       scope,
			Name:        fields[0],
			LoadState:   fields[1],
			ActiveState: fields[2],
			SubState:    fields[3],
		}
		if len(fields) > 4 {
			unit.Description = strings.Join(fields[4:], " ")
		}
		units = append(units, unit)
	}
	return units
}

func (service Service) collectTmux(ctx context.Context, report *Report) {
	output, err := service.Runner.CombinedOutput(ctx, "tmux", "list-sessions", "-F", "#{session_name}|#{session_attached}")
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" || strings.Contains(text, "no server running") {
			return
		}
		report.Warnings = append(report.Warnings, commandWarning("tmux list-sessions", output, err))
		return
	}

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "odin-") {
			continue
		}
		report.Tmux = append(report.Tmux, TmuxSession{
			Name:     parts[0],
			Attached: parts[1] == "1",
		})
	}
	sort.Slice(report.Tmux, func(i, j int) bool {
		return report.Tmux[i].Name < report.Tmux[j].Name
	})
}

func (service Service) collectEngine(ctx context.Context, report *Report) {
	dbPath := report.Engine.Path
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return
	} else if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("legacy engine db stat failed: %v", err))
		return
	}

	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(dbPath))
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("legacy engine db open failed: %v", err))
		return
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("legacy engine db ping failed: %v", err))
		return
	}
	report.Engine.Available = true

	if ok, err := tableExists(ctx, db, "tasks"); err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("legacy tasks table check failed: %v", err))
	} else if ok {
		counts, err := groupedCounts(ctx, db, "tasks", "status")
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("legacy task counts failed: %v", err))
		} else {
			report.Engine.TaskCounts = counts
		}
	}

	if ok, err := tableExists(ctx, db, "runs"); err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("legacy runs table check failed: %v", err))
	} else if ok {
		counts, err := groupedCounts(ctx, db, "runs", "state")
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("legacy run counts failed: %v", err))
		} else {
			report.Engine.RunCounts = counts
		}
	}

	if ok, err := tableExists(ctx, db, "leases"); err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("legacy leases table check failed: %v", err))
	} else if ok {
		leases, err := activeLeases(ctx, db)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("legacy active leases failed: %v", err))
		} else {
			report.Engine.ActiveLeases = leases
		}
	}
}

func sqliteReadOnlyDSN(path string) string {
	return (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String()
}

func tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count)
	return count > 0, err
}

func groupedCounts(ctx context.Context, db *sql.DB, table string, column string) (map[string]int, error) {
	if !simpleIdentifier(table) || !simpleIdentifier(column) {
		return nil, fmt.Errorf("unsafe identifier %q.%q", table, column)
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT %s, COUNT(*) FROM %s GROUP BY %s`, column, table, column))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		counts[key] = count
	}
	return counts, rows.Err()
}

func activeLeases(ctx context.Context, db *sql.DB) ([]LeaseSummary, error) {
	columns, err := tableColumns(ctx, db, "leases")
	if err != nil {
		return nil, err
	}
	for _, required := range []string{"id", "task_id", "status"} {
		if !columns[required] {
			return nil, nil
		}
	}
	workerColumn := ""
	switch {
	case columns["worker_id"]:
		workerColumn = "worker_id"
	case columns["agent_id"]:
		workerColumn = "agent_id"
	default:
		return nil, nil
	}

	where := `status = 'active'`
	selectExpires := "''"
	if columns["expires_at"] {
		selectExpires = "expires_at"
	}
	selectHeartbeat := "''"
	if columns["last_heartbeat"] {
		selectHeartbeat = "last_heartbeat"
	}

	query := fmt.Sprintf(`
		SELECT id, task_id, %s, status, %s, %s
		FROM leases
		WHERE %s
		ORDER BY %s DESC
		LIMIT 10
	`, workerColumn, selectExpires, selectHeartbeat, where, selectHeartbeat)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var leases []LeaseSummary
	for rows.Next() {
		var lease LeaseSummary
		if err := rows.Scan(&lease.ID, &lease.TaskID, &lease.WorkerID, &lease.Status, &lease.ExpiresAt, &lease.LastHeartbeat); err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}
	return leases, rows.Err()
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	if !simpleIdentifier(table) {
		return nil, fmt.Errorf("unsafe identifier %q", table)
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func simpleIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func (service Service) collectState(report *Report) {
	content, ok, warning := readOptionalJSON(report.State.Path)
	if warning != "" {
		report.Warnings = append(report.Warnings, warning)
	}
	if !ok {
		return
	}
	report.State.Available = true
	report.State.TasksThisSession = intFromAny(content["tasks_this_session"])
	report.State.DispatchedTasksCount = intFromAny(content["dispatched_tasks_count"])
	report.State.ActiveRunsCount = countAny(content["active_runs"])
}

func (service Service) collectRouting(report *Report) {
	content, ok, warning := readOptionalJSON(report.Routing.Path)
	if warning != "" {
		report.Warnings = append(report.Warnings, warning)
	}
	if !ok {
		return
	}
	report.Routing.Available = true
	report.Routing.SystemDefault, _ = content["system_default"].(string)
	report.Routing.BackendCount = mapLength(content["backends"])
	report.Routing.TaskRoutingCount = mapLength(content["task_routing"])
	report.Routing.RoleDefaultCount = mapLength(content["role_defaults"])
}

func (service Service) collectRoutingCapabilities(report *CapabilityReport) {
	path := filepath.Join(service.Root, "routing.json")
	content, ok, warning := readOptionalJSON(path)
	if warning != "" {
		report.Warnings = append(report.Warnings, warning)
	}
	if !ok {
		return
	}

	taskRouting := mapFromAny(content["task_routing"])
	report.Summary.TaskRoutes = len(taskRouting)
	for _, key := range sortedMapKeys(taskRouting) {
		addCapability(report, Capability{
			Key:            "task_route:" + key,
			Source:         "routing:task_routing",
			Owner:          "workflow",
			Classification: "migrate",
			Proof:          "real_odin_work_item_run",
			Detail:         stringFromAny(taskRouting[key]),
		})
	}

	roleDefaults := mapFromAny(content["role_defaults"])
	report.Summary.RoleDefaults = len(roleDefaults)
	for _, key := range sortedMapKeys(roleDefaults) {
		addCapability(report, Capability{
			Key:            "role:" + key,
			Source:         "routing:role_defaults",
			Owner:          "worker",
			Classification: "migrate",
			Proof:          "real_odin_worker_dispatch",
		})
	}

	backends := mapFromAny(content["backends"])
	report.Summary.Backends = len(backends)
	for _, key := range sortedMapKeys(backends) {
		addCapability(report, Capability{
			Key:            "backend:" + key,
			Source:         "routing:backends",
			Owner:          "provider_adapter",
			Classification: "migrate",
			Proof:          "real_odin_provider_route",
		})
	}
}

func (service Service) collectTaskTypeCapabilities(report *CapabilityReport) {
	path := filepath.Join(service.ScriptsRoot, "config", "task-type-registry.json")
	content, ok, warning := readOptionalJSONAny(path)
	if warning != "" {
		report.Warnings = append(report.Warnings, warning)
	}
	if !ok {
		return
	}
	items, ok := content.([]any)
	if !ok {
		report.Warnings = append(report.Warnings, "task-type-registry.json parse failed: expected array")
		return
	}
	report.Summary.TaskTypes = len(items)
	for _, item := range items {
		entry := mapFromAny(item)
		key := strings.TrimSpace(stringFromAny(entry["type"]))
		if key == "" {
			continue
		}
		status := strings.TrimSpace(stringFromAny(entry["status"]))
		if status == "active" || status == "" {
			report.Summary.ActiveTaskTypes++
		}
		addCapability(report, Capability{
			Key:            "task_type:" + key,
			Source:         "config:task-type-registry.json",
			Owner:          "workflow",
			Classification: classificationForTaskTypeStatus(status),
			Proof:          "real_odin_task_type_run",
			Detail:         taskTypeDetail(entry),
		})
	}
}

func classificationForTaskTypeStatus(status string) string {
	switch status {
	case "", "active":
		return "migrate"
	default:
		return "inventory_before_migration"
	}
}

func taskTypeDetail(entry map[string]any) string {
	parts := make([]string, 0, 3)
	if status := strings.TrimSpace(stringFromAny(entry["status"])); status != "" {
		parts = append(parts, "status="+status)
	}
	if role := strings.TrimSpace(stringFromAny(entry["role"])); role != "" {
		parts = append(parts, "role="+role)
	}
	if gemma := strings.TrimSpace(stringFromAny(entry["gemma4_eligible"])); gemma != "" {
		parts = append(parts, "gemma4_eligible="+gemma)
	}
	return strings.Join(parts, ",")
}

func (service Service) collectScheduleCapabilities(report *CapabilityReport) {
	candidates, warnings, err := service.ScheduleCandidates(context.Background())
	report.Warnings = append(report.Warnings, warnings...)
	if err != nil {
		report.Warnings = append(report.Warnings, err.Error())
		return
	}
	report.Summary.Schedules = len(candidates)
	for _, candidate := range candidates {
		if !candidate.Enabled {
			continue
		}
		report.Summary.EnabledSchedules++
		addCapability(report, Capability{
			Key:            "schedule:" + candidate.ID,
			Source:         "config:schedules.json",
			Owner:          "automation_trigger",
			Classification: "migrate",
			Proof:          "real_odin_scheduled_work_item",
			Detail:         candidate.Type,
		})
	}
}

func (service Service) ScheduleCandidates(_ context.Context) ([]ScheduleCandidate, []string, error) {
	service = service.WithDefaults()
	path := filepath.Join(service.ScriptsRoot, "config", "schedules.json")
	content, ok, warning := readOptionalJSONAny(path)
	var warnings []string
	if warning != "" {
		warnings = append(warnings, warning)
	}
	if !ok {
		return nil, warnings, nil
	}
	items, ok := content.([]any)
	if !ok {
		return nil, warnings, fmt.Errorf("schedules.json parse failed: expected array")
	}

	candidates := make([]ScheduleCandidate, 0, len(items))
	for index, item := range items {
		entry := mapFromAny(item)
		if len(entry) == 0 {
			continue
		}
		id := strings.TrimSpace(stringFromAny(entry["id"]))
		scheduleType := strings.TrimSpace(stringFromAny(entry["type"]))
		if id == "" {
			id = scheduleType
		}
		if id == "" {
			warnings = append(warnings, fmt.Sprintf("schedules.json item %d skipped: missing id and type", index))
			continue
		}
		payload := map[string]any{}
		if rawPayload, ok := entry["payload"]; ok {
			payload = mapFromAny(rawPayload)
		}
		candidates = append(candidates, ScheduleCandidate{
			ID:          id,
			Type:        scheduleType,
			Role:        strings.TrimSpace(stringFromAny(entry["role"])),
			Cron:        strings.TrimSpace(stringFromAny(entry["cron"])),
			Source:      strings.TrimSpace(stringFromAny(entry["source"])),
			Payload:     payload,
			Enabled:     !isExplicitlyDisabled(entry["enabled"]),
			LegacyIndex: index,
		})
	}
	return candidates, warnings, nil
}

func (service Service) collectToolCapabilities(report *CapabilityReport) {
	path := filepath.Join(service.ScriptsRoot, "config", "tool-registry.json")
	content, ok, warning := readOptionalJSONAny(path)
	if warning != "" {
		report.Warnings = append(report.Warnings, warning)
	}
	if !ok {
		return
	}
	root := mapFromAny(content)
	items, ok := root["tools"].([]any)
	if !ok {
		report.Warnings = append(report.Warnings, "tool-registry.json parse failed: expected tools array")
		return
	}
	report.Summary.Tools = len(items)
	for _, item := range items {
		entry := mapFromAny(item)
		key := strings.TrimSpace(stringFromAny(entry["tool_id"]))
		if key == "" {
			continue
		}
		addCapability(report, Capability{
			Key:            "tool:" + key,
			Source:         "config:tool-registry.json",
			Owner:          "tool",
			Classification: "inventory_before_migration",
			Proof:          "real_odin_tool_or_adapter",
		})
	}
}

func capabilityFromService(name string) (Capability, bool) {
	switch {
	case name == "odin-slack-gateway.service":
		return Capability{Key: "slack_intake", Source: "service:" + name, Owner: "provider_adapter", Classification: "keep_until_migrated", Proof: "real_odin_intake"}, true
	case name == "odin-dropbox.service":
		return Capability{Key: "dropbox_intake", Source: "service:" + name, Owner: "provider_adapter", Classification: "keep_until_migrated", Proof: "real_odin_intake"}, true
	case name == "odin-content-fetcher.service":
		return Capability{Key: "content_fetcher", Source: "service:" + name, Owner: "provider_adapter", Classification: "keep_until_migrated", Proof: "real_odin_intake"}, true
	case name == "odin-ohs.service":
		return Capability{Key: "webhook_intake", Source: "service:" + name, Owner: "provider_adapter", Classification: "keep_until_migrated", Proof: "real_odin_http_intake"}, true
	case name == "odin-keepalive.service" || name == "odin-keepalive.timer":
		return Capability{Key: "keepalive_watchdog", Source: "service:" + name, Owner: "observability", Classification: "bridge_then_migrate", Proof: "real_odin_doctor_status"}, true
	case name == "odin-engine.service":
		return Capability{Key: "legacy_engine", Source: "service:" + name, Owner: "work_queue", Classification: "migrate", Proof: "real_odin_work_item_run"}, true
	case strings.HasPrefix(name, "odin-worker-shim@") && strings.HasSuffix(name, ".service"):
		worker := strings.TrimSuffix(strings.TrimPrefix(name, "odin-worker-shim@"), ".service")
		return Capability{Key: "worker_shim:" + worker, Source: "service:" + name, Owner: "worker", Classification: "migrate", Proof: "real_odin_worker_dispatch"}, true
	default:
		return Capability{}, false
	}
}

func addCapability(report *CapabilityReport, capability Capability) {
	for _, existing := range report.Capabilities {
		if existing.Key == capability.Key && existing.Source == capability.Source {
			return
		}
	}
	report.Capabilities = append(report.Capabilities, capability)
}

func (service Service) collectCheckpoints(report *Report) {
	pattern := filepath.Join(service.Root, "agents", "*", "checkpoint.json")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("legacy checkpoint glob failed: %v", err))
		return
	}
	sort.Strings(paths)

	for _, path := range paths {
		checkpoint, warning := readCheckpoint(path, service.Root)
		if warning != "" {
			report.Warnings = append(report.Warnings, warning)
			continue
		}
		report.Checkpoints = append(report.Checkpoints, checkpoint)
	}

	sort.Slice(report.Checkpoints, func(i, j int) bool {
		return report.Checkpoints[i].WorkerID < report.Checkpoints[j].WorkerID
	})
}

func readCheckpoint(path string, root string) (Checkpoint, string) {
	var checkpoint Checkpoint
	bytes, err := os.ReadFile(path)
	if err != nil {
		return checkpoint, fmt.Sprintf("checkpoint read failed for %s: %v", path, err)
	}
	if err := json.Unmarshal(bytes, &checkpoint); err != nil {
		return checkpoint, fmt.Sprintf("checkpoint parse failed for %s: %v", path, err)
	}
	if strings.TrimSpace(checkpoint.WorkerID) == "" {
		checkpoint.WorkerID = filepath.Base(filepath.Dir(path))
	}
	if strings.TrimSpace(checkpoint.LogPath) != "" {
		checkpoint.Log = readLogEvidence(resolveLegacyPath(root, checkpoint.LogPath))
	}
	return checkpoint, ""
}

func resolveLegacyPath(root string, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func readLogEvidence(path string) LogEvidence {
	evidence := LogEvidence{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		return evidence
	}
	evidence.Exists = true
	evidence.SizeBytes = info.Size()
	bytes, err := os.ReadFile(path)
	if err != nil {
		return evidence
	}
	evidence.LastLine = lastNonEmptyLine(string(bytes))
	return evidence
}

func lastNonEmptyLine(content string) string {
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := normalizeLogEvidenceLine(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func normalizeLogEvidenceLine(line string) string {
	line = ansiEscapePattern.ReplaceAllString(line, "")
	line = strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' {
			return -1
		}
		return r
	}, line)
	line = strings.TrimSpace(line)
	if looksLikeShellPrompt(line) {
		return ""
	}
	return line
}

func looksLikeShellPrompt(line string) bool {
	return strings.HasSuffix(line, "$") &&
		strings.Contains(line, "@") &&
		strings.Contains(line, ":") &&
		strings.Contains(line, "odin-orchestrator")
}

func readOptionalJSON(path string) (map[string]any, bool, string) {
	bytes, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, ""
	}
	if err != nil {
		return nil, false, fmt.Sprintf("%s read failed: %v", filepath.Base(path), err)
	}
	var content map[string]any
	if err := json.Unmarshal(bytes, &content); err != nil {
		return nil, false, fmt.Sprintf("%s parse failed: %v", filepath.Base(path), err)
	}
	return content, true, ""
}

func readOptionalJSONAny(path string) (any, bool, string) {
	bytes, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, ""
	}
	if err != nil {
		return nil, false, fmt.Sprintf("%s read failed: %v", filepath.Base(path), err)
	}
	var content any
	if err := json.Unmarshal(bytes, &content); err != nil {
		return nil, false, fmt.Sprintf("%s parse failed: %v", filepath.Base(path), err)
	}
	return content, true, ""
}

func mapFromAny(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func isExplicitlyDisabled(value any) bool {
	typed, ok := value.(bool)
	return ok && !typed
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}

func countAny(value any) int {
	switch typed := value.(type) {
	case []any:
		return len(typed)
	case map[string]any:
		return len(typed)
	default:
		return 0
	}
}

func mapLength(value any) int {
	if typed, ok := value.(map[string]any); ok {
		return len(typed)
	}
	return 0
}

func commandWarning(label string, output []byte, err error) string {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return fmt.Sprintf("%s failed: %v", label, err)
	}
	return fmt.Sprintf("%s failed: %v: %s", label, err, text)
}

func envOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func RenderText(report Report) string {
	var builder strings.Builder
	fmt.Fprintf(
		&builder,
		"legacy_odin status=%s root=%s root_exists=%t services=%d failed_services=%d active_leases=%d checkpoints=%d tmux_sessions=%d warnings=%d\n",
		report.Status,
		report.Root,
		report.RootExists,
		len(report.Services),
		failedServiceCount(report.Services),
		len(report.Engine.ActiveLeases),
		len(report.Checkpoints),
		len(report.Tmux),
		len(report.Warnings),
	)

	if report.Engine.Available {
		fmt.Fprintf(&builder, "legacy_engine path=%s task_counts=%s run_counts=%s\n", report.Engine.Path, formatCounts(report.Engine.TaskCounts), formatCounts(report.Engine.RunCounts))
		for _, lease := range report.Engine.ActiveLeases {
			fmt.Fprintf(&builder, "legacy_lease id=%s task=%s worker=%s status=%s last_heartbeat=%s expires_at=%s\n", lease.ID, lease.TaskID, lease.WorkerID, lease.Status, lease.LastHeartbeat, lease.ExpiresAt)
		}
	} else {
		fmt.Fprintf(&builder, "legacy_engine path=%s available=false\n", report.Engine.Path)
	}

	if report.State.Available {
		fmt.Fprintf(&builder, "legacy_state path=%s tasks_this_session=%d dispatched_tasks=%d active_runs=%d\n", report.State.Path, report.State.TasksThisSession, report.State.DispatchedTasksCount, report.State.ActiveRunsCount)
	}

	if report.Routing.Available {
		fmt.Fprintf(&builder, "legacy_routing path=%s system_default=%s backends=%d task_routes=%d role_defaults=%d\n", report.Routing.Path, report.Routing.SystemDefault, report.Routing.BackendCount, report.Routing.TaskRoutingCount, report.Routing.RoleDefaultCount)
	}

	for _, unit := range report.Services {
		fmt.Fprintf(&builder, "legacy_service scope=%s service=%s load=%s active=%s sub=%s\n", unit.Scope, unit.Name, unit.LoadState, unit.ActiveState, unit.SubState)
	}

	for _, session := range report.Tmux {
		fmt.Fprintf(&builder, "legacy_tmux session=%s attached=%t\n", session.Name, session.Attached)
	}

	for _, checkpoint := range report.Checkpoints {
		fmt.Fprintf(
			&builder,
			"legacy_checkpoint worker=%s task=%s backend=%s task_type=%s lease=%s start_at=%s ttl_seconds=%d repo=%s log=%s\n",
			checkpoint.WorkerID,
			checkpoint.TaskID,
			checkpoint.Backend,
			checkpoint.TaskType,
			checkpoint.LeaseID,
			checkpoint.StartAt,
			checkpoint.TTLSeconds,
			checkpoint.RepoRoot,
			checkpoint.LogPath,
		)
		if checkpoint.Log.Path != "" {
			fmt.Fprintf(&builder, "legacy_checkpoint_log worker=%s exists=%t last_line=%q path=%s size_bytes=%d\n", checkpoint.WorkerID, checkpoint.Log.Exists, checkpoint.Log.LastLine, checkpoint.Log.Path, checkpoint.Log.SizeBytes)
		}
	}

	for _, warning := range report.Warnings {
		fmt.Fprintf(&builder, "legacy_warning=%q\n", warning)
	}

	return builder.String()
}

func RenderCapabilityText(report CapabilityReport) string {
	var builder strings.Builder
	fmt.Fprintf(
		&builder,
		"legacy_capability_registry root=%s scripts_root=%s capabilities=%d services=%d task_routes=%d role_defaults=%d backends=%d task_types=%d active_task_types=%d schedules=%d enabled_schedules=%d tools=%d warnings=%d\n",
		report.Root,
		report.ScriptsRoot,
		report.Summary.CapabilityRecords,
		report.Summary.Services,
		report.Summary.TaskRoutes,
		report.Summary.RoleDefaults,
		report.Summary.Backends,
		report.Summary.TaskTypes,
		report.Summary.ActiveTaskTypes,
		report.Summary.Schedules,
		report.Summary.EnabledSchedules,
		report.Summary.Tools,
		len(report.Warnings),
	)
	for _, capability := range report.Capabilities {
		fmt.Fprintf(
			&builder,
			"legacy_capability key=%s source=%s owner=%s classification=%s proof=%s",
			capability.Key,
			capability.Source,
			capability.Owner,
			capability.Classification,
			capability.Proof,
		)
		if strings.TrimSpace(capability.Detail) != "" {
			fmt.Fprintf(&builder, " detail=%q", capability.Detail)
		}
		builder.WriteByte('\n')
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(&builder, "legacy_capability_warning=%q\n", warning)
	}
	return builder.String()
}

func failedServiceCount(units []UnitStatus) int {
	var count int
	for _, unit := range units {
		if unit.ActiveState == "failed" || unit.SubState == "failed" || unit.LoadState == "not-found" {
			count++
		}
	}
	return count
}

func formatCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", key, counts[key]))
	}
	return strings.Join(parts, ",")
}

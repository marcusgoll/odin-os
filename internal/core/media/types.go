package media

type AutomationClass string

const (
	AutomationClassAutoAllowed      AutomationClass = "auto_allowed"
	AutomationClassNotifyOnly       AutomationClass = "notify_only"
	AutomationClassApprovalRequired AutomationClass = "approval_required"
	AutomationClassForbidden        AutomationClass = "forbidden"
)

type ServiceKind string

const (
	ServiceKindPlex       ServiceKind = "plex"
	ServiceKindRadarr     ServiceKind = "radarr"
	ServiceKindSonarr     ServiceKind = "sonarr"
	ServiceKindProwlarr   ServiceKind = "prowlarr"
	ServiceKindDownloader ServiceKind = "downloader"
	ServiceKindVPN        ServiceKind = "vpn"
	ServiceKindSeedbox    ServiceKind = "seedbox"
	ServiceKindUsenet     ServiceKind = "usenet"
	ServiceKindSync       ServiceKind = "sync"
)

type Config struct {
	Enabled           bool           `yaml:"enabled"`
	MaintenanceWindow string         `yaml:"maintenance_window,omitempty"`
	Services          []StackService `yaml:"services"`
	Mounts            []MountRule    `yaml:"mounts,omitempty"`
	Thresholds        Thresholds     `yaml:"thresholds,omitempty"`
	Policies          Policies       `yaml:"policies"`
}

type StackService struct {
	Name       string      `yaml:"name"`
	Kind       ServiceKind `yaml:"kind"`
	BaseURL    string      `yaml:"base_url,omitempty"`
	HealthPath string      `yaml:"health_path,omitempty"`
	TokenEnv   string      `yaml:"token_env,omitempty"`
	Optional   bool        `yaml:"optional,omitempty"`
}

type MountRule struct {
	Name           string `yaml:"name"`
	Path           string `yaml:"path"`
	ExpectedSource string `yaml:"expected_source,omitempty"`
	Sentinel       string `yaml:"sentinel,omitempty"`
}

type Thresholds struct {
	DiskWarningPercent        int `yaml:"disk_warning_percent,omitempty"`
	DiskCriticalPercent       int `yaml:"disk_critical_percent,omitempty"`
	QueueStalledMinutes       int `yaml:"queue_stalled_minutes,omitempty"`
	ImportLagMinutes          int `yaml:"import_lag_minutes,omitempty"`
	TelemetryFreshnessMinutes int `yaml:"telemetry_freshness_minutes,omitempty"`
}

type Policies struct {
	AutoAllowed      []string `yaml:"auto_allowed,omitempty"`
	NotifyOnly       []string `yaml:"notify_only,omitempty"`
	ApprovalRequired []string `yaml:"approval_required,omitempty"`
	Forbidden        []string `yaml:"forbidden,omitempty"`
}

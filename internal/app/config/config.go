package config

import (
	"bytes"
	"os"
	"path/filepath"

	coremedia "odin-os/internal/core/media"

	"gopkg.in/yaml.v3"
)

type File struct {
	Version int             `yaml:"version"`
	Runtime RuntimeFile     `yaml:"runtime"`
	Service ServiceSettings `yaml:"service"`
}

type RuntimeFile struct {
	Root string `yaml:"root"`
}

type ServiceSettings struct {
	HTTPAddr        string                `yaml:"http_addr"`
	StartupRecovery bool                  `yaml:"startup_recovery"`
	AdminTokenEnv   string                `yaml:"admin_token_env"`
	EmailActions    EmailActionSettings   `yaml:"email_actions"`
	SocialCopilot   SocialCopilotSettings `yaml:"social_copilot"`
}

type EmailActionSettings struct {
	Recipient    string `yaml:"recipient"`
	BaseURL      string `yaml:"base_url"`
	SecretEnv    string `yaml:"secret_env"`
	SendmailPath string `yaml:"sendmail_path"`
	From         string `yaml:"from"`
}

type SocialCopilotSettings struct {
	Enabled        bool   `yaml:"enabled"`
	WorkflowKey    string `yaml:"workflow_key"`
	CadenceSeconds int64  `yaml:"cadence_seconds"`
}

type Config struct {
	Version           int
	RuntimeRoot       string
	Service           ServiceSettings
	MediaConfigPath   string
	Media             *coremedia.Config
	AdminToken        string
	EmailActionSecret string
}

func Load(path string, repoRoot string, env map[string]string) (Config, error) {
	var raw File
	if err := decodeYAMLFile(path, &raw); err != nil {
		return Config{}, err
	}

	cfg := Config{
		Version: raw.Version,
		Service: raw.Service,
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Service.HTTPAddr == "" {
		cfg.Service.HTTPAddr = "127.0.0.1:9443"
	}
	if cfg.Service.AdminTokenEnv == "" {
		cfg.Service.AdminTokenEnv = "ODIN_ADMIN_TOKEN"
	}
	if cfg.Service.EmailActions.SecretEnv == "" {
		cfg.Service.EmailActions.SecretEnv = "ODIN_EMAIL_ACTION_SECRET"
	}
	if cfg.Service.EmailActions.From == "" {
		cfg.Service.EmailActions.From = "odin-os@localhost"
	}
	if !raw.Service.StartupRecovery {
		cfg.Service.StartupRecovery = false
	} else {
		cfg.Service.StartupRecovery = true
	}
	if cfg.Service.SocialCopilot.WorkflowKey == "" {
		cfg.Service.SocialCopilot.WorkflowKey = "marcus-social-growth-workflow"
	}
	if cfg.Service.SocialCopilot.CadenceSeconds <= 0 {
		cfg.Service.SocialCopilot.CadenceSeconds = 1800
	}

	cfg.RuntimeRoot = resolveRuntimeRoot(repoRoot, raw.Runtime.Root)
	if value := env["ODIN_ROOT"]; value != "" {
		cfg.RuntimeRoot = value
	}
	if value := env["ODIN_HTTP_ADDR"]; value != "" {
		cfg.Service.HTTPAddr = value
	}
	if value := env[cfg.Service.AdminTokenEnv]; value != "" {
		cfg.AdminToken = value
	}
	if value := env["ODIN_EMAIL_ACTION_RECIPIENT"]; value != "" {
		cfg.Service.EmailActions.Recipient = value
	}
	if value := env["ODIN_EMAIL_ACTION_BASE_URL"]; value != "" {
		cfg.Service.EmailActions.BaseURL = value
	}
	if value := env["ODIN_EMAIL_ACTION_SENDMAIL_PATH"]; value != "" {
		cfg.Service.EmailActions.SendmailPath = value
	}
	if value := env["ODIN_EMAIL_ACTION_FROM"]; value != "" {
		cfg.Service.EmailActions.From = value
	}
	if value := env[cfg.Service.EmailActions.SecretEnv]; value != "" {
		cfg.EmailActionSecret = value
	}

	mediaPath, mediaConfig, err := loadMediaConfig(repoRoot, env)
	if err != nil {
		return Config{}, err
	}
	cfg.MediaConfigPath = mediaPath
	cfg.Media = mediaConfig

	return cfg, nil
}

func resolveRuntimeRoot(repoRoot string, configured string) string {
	if configured == "" {
		return repoRoot
	}
	if filepath.IsAbs(configured) {
		return configured
	}
	return filepath.Clean(filepath.Join(repoRoot, configured))
}

func decodeYAMLFile(path string, target any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	return decoder.Decode(target)
}

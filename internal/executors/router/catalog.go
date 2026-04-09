package router

import (
	"odin-os/internal/executors/anthropic_api"
	"odin-os/internal/executors/claude_code"
	"odin-os/internal/executors/codex"
	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/gemini_cli"
	"odin-os/internal/executors/google_api"
	"odin-os/internal/executors/openai_api"
	"odin-os/internal/executors/openrouter_api"
	"odin-os/internal/executors/xai_api"
)

func DefaultCatalog() map[string]contract.Executor {
	return map[string]contract.Executor{
		"codex_headless":       codex.NewHeadless(),
		"claude_code_headless": claude_code.NewHeadless(),
		"gemini_cli_headless":  gemini_cli.NewHeadless(),
		"openai_api":           openai_api.New(),
		"anthropic_api":        anthropic_api.New(),
		"google_api":           google_api.New(),
		"xai_api":              xai_api.New(),
		"openrouter_api":       openrouter_api.New(),
	}
}

package commands

import (
	"fmt"
	"strings"
)

const (
	ShellCommandSummary = "/help /mode /scope /overview /project /agent /workflow /memory /skill /tool /transition /observe /compare /jobs /runs /approvals /logs /doctor /self"
	TransitionUsage     = "/transition [status] | /transition set <state> [allow=<csv>] [confirm] because <reason...>"
	ProjectAddUsage     = "/project add <key> <git-root> [name=<value>] [class=local_git_project|github_backed_project] [default_branch=<value>] [github_repo=<owner/name>]"
	AgentUsage          = "/agent [list|show <key>|validate <key>|run <key> [input=value...]]"
	JobsUsage           = "/jobs | /jobs cancel <task-key>"
	MemoryUsage         = "/memory [list [type=<memory_type>] [contains=<text>] [field.<name>=<value> ...] [limit=<n>] [order=asc|desc]|show <id>|remember <memory_type> [field=value ...] -- <summary...>|resolve <id> result=approved|rejected [reason=<value>]|publish <id> [url=<value> [published_at=<rfc3339>]|via=huginn_x]]"
	RunsUsage           = "/runs | /runs show [run-id|active] | /runs cancel [run-id|active]"
	LegacyUsage         = "legacy [status|capabilities] [--json]"
	WorkflowUsage       = "/workflow [list|show <key>|validate <key>|use <key>|clear|social <status|wake reason=<token>|scope replace ...>]"
	SkillUsage          = "/skill [list|show <key>|use <key>|validate <key>|clear]"
	ToolUsage           = "/tool [list|show <key>|run <key> [input=value...]]"
)

func InteractiveHelp() string {
	return strings.Join([]string{
		ShellCommandSummary,
		TransitionUsage,
		ProjectAddUsage,
		AgentUsage,
		WorkflowUsage,
		MemoryUsage,
		JobsUsage,
		RunsUsage,
		SkillUsage,
		ToolUsage,
	}, "\n")
}

func OperatorHelp(binary string) string {
	if strings.TrimSpace(binary) == "" {
		binary = "odin"
	}

	return fmt.Sprintf(`Usage:
  %[1]s                  Start the interactive operator shell
  %[1]s help             Show this help
  %[1]s overview [--json] Show the canonical operator overview
  %[1]s doctor [--json]  Show runtime readiness
  %[1]s legacy           Show read-only legacy Odin status and capability inventory
  %[1]s healthcheck      Exit successfully only when the runtime is ready
  %[1]s serve            Run background service loops and the operational HTTP surface
  %[1]s backup           Create a runtime backup
  %[1]s restore          Restore a runtime backup
  %[1]s verify-backup    Verify a backup artifact
  %[1]s profile          Show runtime profile details
  %[1]s project          Manage enrolled projects
  %[1]s workspace        Manage project Codex workspaces

Interactive shell:
  %[2]s
  %[3]s
  %[4]s
  %[5]s
  %[6]s
  %[7]s
  %[8]s
  %[9]s
  %[10]s
  %[11]s

Project workspace:
  %[1]s project enroll
  %[1]s workspace start [project]
  %[1]s workspace status [project] [--json]
  %[1]s workspace handoff [project] objective=<value>
  %[1]s workspace stop [project] --force

Typical flow:
  %[1]s
  /project <key>
  /skill use <skill-key>
  /tool run <tool-key> key=value
  /mode act
  <durable task request>
`, binary, ShellCommandSummary, ProjectAddUsage, AgentUsage, WorkflowUsage, MemoryUsage, SkillUsage, ToolUsage, TransitionUsage, JobsUsage, RunsUsage)
}

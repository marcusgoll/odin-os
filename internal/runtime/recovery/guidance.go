package recovery

import "strings"

type RetryGuidanceInput struct {
	RetryCount  int
	MaxAttempts int
	WorkKind    string
	RequestedBy string
}

type RetryGuidance struct {
	Decision               string
	RetryEligible          bool
	RecoveryRecommendation string
	Source                 string
}

func RetryGuidanceForTask(input RetryGuidanceInput) RetryGuidance {
	source := recoverySource(input.WorkKind, input.RequestedBy)
	if input.MaxAttempts <= 1 {
		if source == "automation_trigger" {
			return RetryGuidance{
				Decision:               "retry_blocked_non_retryable",
				RetryEligible:          false,
				RecoveryRecommendation: "Trigger-produced work is marked non-retryable. Inspect the trigger rule and failed run logs, then open a follow-up or change task policy before retrying.",
				Source:                 source,
			}
		}
		return RetryGuidance{
			Decision:               "retry_blocked_non_retryable",
			RetryEligible:          false,
			RecoveryRecommendation: "Open a follow-up or change task policy before retrying; this task is marked non-retryable.",
			Source:                 source,
		}
	}
	if input.RetryCount+1 >= input.MaxAttempts {
		if source == "automation_trigger" {
			return RetryGuidance{
				Decision:               "retry_blocked_max_attempts",
				RetryEligible:          false,
				RecoveryRecommendation: "Trigger-produced work reached the retry limit. Inspect the trigger rule and materialization, then open a follow-up or adjust task policy before any further retry.",
				Source:                 source,
			}
		}
		return RetryGuidance{
			Decision:               "retry_blocked_max_attempts",
			RetryEligible:          false,
			RecoveryRecommendation: "Open a follow-up or adjust the task before retrying; max attempts reached.",
			Source:                 source,
		}
	}
	if source == "automation_trigger" {
		return RetryGuidance{
			Decision:               "retry_allowed",
			RetryEligible:          true,
			RecoveryRecommendation: "Trigger-produced work failed. Inspect the trigger materialization and failed run logs, then retry only through odin review act failed-work ID retry or odin work retry within policy.",
			Source:                 source,
		}
	}
	return RetryGuidance{
		Decision:               "retry_allowed",
		RetryEligible:          true,
		RecoveryRecommendation: "Retry is allowed; dispatch the queued task to create the next run attempt.",
		Source:                 source,
	}
}

func recoverySource(workKind string, requestedBy string) string {
	if strings.EqualFold(strings.TrimSpace(workKind), "automation_trigger") || strings.HasPrefix(strings.TrimSpace(requestedBy), "automation_trigger:") {
		return "automation_trigger"
	}
	return "work"
}

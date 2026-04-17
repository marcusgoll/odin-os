package approvals

import coremedia "odin-os/internal/core/media"

type Decision struct {
	Action           string
	Class            coremedia.AutomationClass
	Allowed          bool
	RequiresApproval bool
}

type Service struct{}

func (Service) Evaluate(config coremedia.Config, action string) Decision {
	class := (coremedia.Service{}).ClassifyAction(config, action)
	decision := Decision{
		Action: action,
		Class:  class,
	}

	switch class {
	case coremedia.AutomationClassForbidden:
		decision.Allowed = false
	case coremedia.AutomationClassApprovalRequired:
		decision.Allowed = true
		decision.RequiresApproval = true
	default:
		decision.Allowed = true
	}

	return decision
}

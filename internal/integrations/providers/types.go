package providers

import "odin-os/internal/executors/contract"

type ExecutionRequest struct {
	ProviderKey string
	Stream      bool
	Spec        contract.TaskSpec
}

type CapabilityProfile struct {
	ProviderKey           string
	ExecutorClass         contract.ExecutorClass
	SupportsResume        bool
	SupportsCancel        bool
	SupportsTools         bool
	SupportsStreaming     bool
	SupportsCostEstimate  bool
	SupportsHeadlessPlan  bool
	SupportsBrokerRouting bool
	TaskKinds             []contract.TaskKind
	Scopes                []string
}

func (profile CapabilityProfile) Matches(spec contract.TaskSpec) bool {
	return contract.Capabilities{
		ExecutorClass:         profile.ExecutorClass,
		SupportsResume:        profile.SupportsResume,
		SupportsCancel:        profile.SupportsCancel,
		SupportsTools:         profile.SupportsTools,
		SupportsStreaming:     profile.SupportsStreaming,
		SupportsCostEstimate:  profile.SupportsCostEstimate,
		SupportsHeadlessPlan:  profile.SupportsHeadlessPlan,
		SupportsBrokerRouting: profile.SupportsBrokerRouting,
		TaskKinds:             append([]contract.TaskKind(nil), profile.TaskKinds...),
		Scopes:                append([]string(nil), profile.Scopes...),
	}.Matches(spec)
}

type ProviderError struct {
	ProviderKey string
	Code        string
	Message     string
	Retryable   bool
}

func (err ProviderError) Error() string {
	if err.ProviderKey == "" {
		return err.Message
	}
	return err.ProviderKey + ": " + err.Message
}

type StreamingEventEnvelope struct {
	ProviderKey string
	EventType   string
	Sequence    int
	Content     string
	Metadata    map[string]string
}

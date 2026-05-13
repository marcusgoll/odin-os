package events

import (
	"reflect"
	"testing"
)

func TestConversationTranscriptRecordedContract(t *testing.T) {
	t.Parallel()

	if got := StreamConversation; got != StreamType("conversation_transcript") {
		t.Fatalf("StreamConversation = %q, want %q", got, StreamType("conversation_transcript"))
	}
	if got := EventConversationTranscriptRecorded; got != Type("conversation.transcript_recorded") {
		t.Fatalf("EventConversationTranscriptRecorded = %q, want %q", got, Type("conversation.transcript_recorded"))
	}

	taskID := int64(41)
	runID := int64(99)
	want := ConversationTranscriptRecordedPayload{
		Scope:    "project",
		ScopeKey: "odin-core",
		Mode:     "chat",
		Executor: "codex",
		TaskID:   &taskID,
		RunID:    &runID,
	}

	payload, err := EncodePayload(want)
	if err != nil {
		t.Fatalf("EncodePayload(ConversationTranscriptRecordedPayload) error = %v", err)
	}
	if got := string(payload); got != `{"scope":"project","scope_key":"odin-core","mode":"chat","executor":"codex","task_id":41,"run_id":99}` {
		t.Fatalf("encoded payload = %s", got)
	}

	decoded, err := DecodePayload[ConversationTranscriptRecordedPayload](payload)
	if err != nil {
		t.Fatalf("DecodePayload(ConversationTranscriptRecordedPayload) error = %v", err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded payload = %#v, want %#v", decoded, want)
	}
}

func TestMemorySummaryRecordedContract(t *testing.T) {
	t.Parallel()

	if got := StreamMemorySummary; got != StreamType("memory_summary") {
		t.Fatalf("StreamMemorySummary = %q, want %q", got, StreamType("memory_summary"))
	}
	if got := EventMemorySummaryRecorded; got != Type("memory.summary_recorded") {
		t.Fatalf("EventMemorySummaryRecorded = %q, want %q", got, Type("memory.summary_recorded"))
	}

	sourceTranscriptID := int64(12)
	taskID := int64(41)
	runID := int64(99)
	want := MemorySummaryRecordedPayload{
		Scope:              "project",
		ScopeKey:           "odin-core",
		MemoryType:         "decision",
		SourceTranscriptID: &sourceTranscriptID,
		TaskID:             &taskID,
		RunID:              &runID,
	}

	payload, err := EncodePayload(want)
	if err != nil {
		t.Fatalf("EncodePayload(MemorySummaryRecordedPayload) error = %v", err)
	}
	if got := string(payload); got != `{"scope":"project","scope_key":"odin-core","memory_type":"decision","source_transcript_id":12,"task_id":41,"run_id":99}` {
		t.Fatalf("encoded payload = %s", got)
	}

	decoded, err := DecodePayload[MemorySummaryRecordedPayload](payload)
	if err != nil {
		t.Fatalf("DecodePayload(MemorySummaryRecordedPayload) error = %v", err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded payload = %#v, want %#v", decoded, want)
	}
}

func TestMemorySummaryUpdatedContract(t *testing.T) {
	t.Parallel()

	if got := StreamMemorySummary; got != StreamType("memory_summary") {
		t.Fatalf("StreamMemorySummary = %q, want %q", got, StreamType("memory_summary"))
	}
	if got := EventMemorySummaryUpdated; got != Type("memory.summary_updated") {
		t.Fatalf("EventMemorySummaryUpdated = %q, want %q", got, Type("memory.summary_updated"))
	}

	sourceTranscriptID := int64(12)
	taskID := int64(41)
	runID := int64(99)
	want := MemorySummaryUpdatedPayload{
		Scope:              "project",
		ScopeKey:           "odin-core",
		MemoryType:         "decision",
		SourceTranscriptID: &sourceTranscriptID,
		TaskID:             &taskID,
		RunID:              &runID,
	}

	payload, err := EncodePayload(want)
	if err != nil {
		t.Fatalf("EncodePayload(MemorySummaryUpdatedPayload) error = %v", err)
	}
	if got := string(payload); got != `{"scope":"project","scope_key":"odin-core","memory_type":"decision","source_transcript_id":12,"task_id":41,"run_id":99}` {
		t.Fatalf("encoded payload = %s", got)
	}

	decoded, err := DecodePayload[MemorySummaryUpdatedPayload](payload)
	if err != nil {
		t.Fatalf("DecodePayload(MemorySummaryUpdatedPayload) error = %v", err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded payload = %#v, want %#v", decoded, want)
	}
}

func TestMemoryProposalContract(t *testing.T) {
	t.Parallel()

	if got := EventMemoryProposalCreated; got != Type("memory.proposal_created") {
		t.Fatalf("EventMemoryProposalCreated = %q, want %q", got, Type("memory.proposal_created"))
	}
	if got := EventMemoryProposalResolved; got != Type("memory.proposal_resolved") {
		t.Fatalf("EventMemoryProposalResolved = %q, want %q", got, Type("memory.proposal_resolved"))
	}

	want := MemoryProposalPayload{
		MemoryID:    12,
		Scope:       "project",
		ScopeKey:    "odin-core",
		MemoryType:  "operating_note",
		Status:      "accepted",
		Decision:    "accept",
		SourceType:  "run",
		SourceID:    "44",
		SourceKey:   "run-44",
		Sensitivity: "normal",
		ReviewedBy:  "operator",
		Reason:      "verified source",
	}

	payload, err := EncodePayload(want)
	if err != nil {
		t.Fatalf("EncodePayload(MemoryProposalPayload) error = %v", err)
	}
	if got := string(payload); got != `{"memory_id":12,"scope":"project","scope_key":"odin-core","memory_type":"operating_note","status":"accepted","decision":"accept","source_type":"run","source_id":"44","source_key":"run-44","sensitivity":"normal","reviewed_by":"operator","reason":"verified source"}` {
		t.Fatalf("encoded payload = %s", got)
	}

	decoded, err := DecodePayload[MemoryProposalPayload](payload)
	if err != nil {
		t.Fatalf("DecodePayload(MemoryProposalPayload) error = %v", err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded payload = %#v, want %#v", decoded, want)
	}
}

func TestMobileDeviceAuditContract(t *testing.T) {
	t.Parallel()

	if got := StreamMobileDevice; got != StreamType("mobile_device") {
		t.Fatalf("StreamMobileDevice = %q, want %q", got, StreamType("mobile_device"))
	}
	if got := EventMobileLogin; got != Type("mobile.login") {
		t.Fatalf("EventMobileLogin = %q, want mobile.login", got)
	}
	if got := EventMobileLogout; got != Type("mobile.logout") {
		t.Fatalf("EventMobileLogout = %q, want mobile.logout", got)
	}
	if got := EventMobileIntakeCreated; got != Type("mobile.intake_created") {
		t.Fatalf("EventMobileIntakeCreated = %q, want mobile.intake_created", got)
	}
	if got := EventMobileApprovalResolved; got != Type("mobile.approval_resolved") {
		t.Fatalf("EventMobileApprovalResolved = %q, want mobile.approval_resolved", got)
	}
	if got := EventMobilePushSubscriptionRevoked; got != Type("mobile.push_subscription_revoked") {
		t.Fatalf("EventMobilePushSubscriptionRevoked = %q, want mobile.push_subscription_revoked", got)
	}

	payload, err := EncodePayload(MobileIntakeCreatedPayload{
		DeviceID:     "device-1",
		SessionID:    2,
		IntakeItemID: 3,
		IntakeType:   "idea",
	})
	if err != nil {
		t.Fatalf("EncodePayload(MobileIntakeCreatedPayload) error = %v", err)
	}
	if got := string(payload); got != `{"device_id":"device-1","session_id":2,"intake_item_id":3,"intake_type":"idea"}` {
		t.Fatalf("encoded mobile intake payload = %s", got)
	}
}

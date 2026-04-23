package projects

type LimitedActionKey string

const (
	LimitedActionDocsAuditNote   LimitedActionKey = "docs_audit_note"
	LimitedActionDocsUpdate      LimitedActionKey = "docs_update"
	LimitedActionRepoHygieneNote LimitedActionKey = "repo_hygiene_note"
)

type LimitedActionContentMode string

const (
	LimitedActionContentModeCreateMarkdownNote LimitedActionContentMode = "create_markdown_note"
	LimitedActionContentModeAppendMarkdownNote LimitedActionContentMode = "append_markdown_note"
)

func KnownLimitedActionKeys() map[string]struct{} {
	return map[string]struct{}{
		string(LimitedActionDocsAuditNote):   {},
		string(LimitedActionDocsUpdate):      {},
		string(LimitedActionRepoHygieneNote): {},
	}
}

func SupportsLimitedAction(manifest Manifest, key string) bool {
	if key == "" {
		return false
	}
	_, ok := manifest.Policy.LimitedActions[key]
	return ok
}

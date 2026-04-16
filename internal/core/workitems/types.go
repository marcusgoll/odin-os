package workitems

type WorkItem struct {
	ID           int64
	Key          string
	WorkspaceID  *int64
	InitiativeID *int64
	CompanionID  *int64
	WorkKind     string
	Status       string
}

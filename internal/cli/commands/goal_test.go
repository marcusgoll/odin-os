package commands

import "testing"

func TestParseGoalShowUpdateTransitionWithIDFlagAndListLimit(t *testing.T) {
	t.Parallel()

	show, err := ParseGoal([]string{"show", "--id", "42", "--json"})
	if err != nil {
		t.Fatalf("ParseGoal(show) error = %v", err)
	}
	if show.Name != "show" || show.ID != 42 || !show.JSON {
		t.Fatalf("show command = %+v, want show id 42 json", show)
	}

	update, err := ParseGoal([]string{"update", "--id", "42", "--title", "Updated", "--description", "Details", "--json"})
	if err != nil {
		t.Fatalf("ParseGoal(update) error = %v", err)
	}
	if update.Name != "update" || update.ID != 42 || update.Title != "Updated" || update.Description != "Details" || !update.JSON {
		t.Fatalf("update command = %+v, want update id/title/description/json", update)
	}

	transition, err := ParseGoal([]string{"transition", "--id", "42", "--status", "planned", "--json"})
	if err != nil {
		t.Fatalf("ParseGoal(transition) error = %v", err)
	}
	if transition.Name != "transition" || transition.ID != 42 || transition.Status != "planned" || !transition.JSON {
		t.Fatalf("transition command = %+v, want id/status/json", transition)
	}

	list, err := ParseGoal([]string{"list", "--status", "created", "--limit", "10", "--json"})
	if err != nil {
		t.Fatalf("ParseGoal(list) error = %v", err)
	}
	if list.Name != "list" || list.Status != "created" || list.Limit != 10 || !list.JSON {
		t.Fatalf("list command = %+v, want status/limit/json", list)
	}

	tick, err := ParseGoal([]string{"tick", "--json"})
	if err != nil {
		t.Fatalf("ParseGoal(tick) error = %v", err)
	}
	if tick.Name != "tick" || !tick.JSON {
		t.Fatalf("tick command = %+v, want tick json", tick)
	}
}

func TestParseGoalUpdateRequiresAllowedField(t *testing.T) {
	t.Parallel()

	if _, err := ParseGoal([]string{"update", "--id", "42", "--json"}); err == nil {
		t.Fatal("ParseGoal(update without fields) error = nil, want error")
	}
}

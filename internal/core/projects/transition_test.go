package projects

import "testing"

func TestTransitionAuthorizeRejectsMutationInReadOnlyStates(t *testing.T) {
	t.Parallel()

	for _, state := range []TransitionState{
		TransitionStateInventory,
		TransitionStateShadow,
		TransitionStateCompare,
	} {
		result := AuthorizeTransitionAction(TransitionAuthRequest{
			Transition: RuntimeTransition{
				State:      state,
				Controller: TransitionControllerLegacyOdin,
			},
			Actor:       TransitionControllerOdinOS,
			ActionClass: ActionClassFullMutation,
		})
		if result.Allowed {
			t.Fatalf("%s unexpectedly allowed full mutation", state)
		}
	}
}

func TestTransitionAuthorizeAllowsOnlyExplicitLimitedActions(t *testing.T) {
	t.Parallel()

	transition := RuntimeTransition{
		State:          TransitionStateLimitedAction,
		Controller:     TransitionControllerOdinOS,
		LimitedActions: []string{"branch_proposal"},
	}

	allowed := AuthorizeTransitionAction(TransitionAuthRequest{
		Transition:  transition,
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassIsolatedMutation,
		ActionKey:   "branch_proposal",
	})
	if !allowed.Allowed {
		t.Fatalf("expected explicit isolated mutation to be allowed, got %+v", allowed)
	}

	denied := AuthorizeTransitionAction(TransitionAuthRequest{
		Transition:  transition,
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassFullMutation,
		ActionKey:   "merge_to_main",
	})
	if denied.Allowed {
		t.Fatalf("expected full mutation to be denied in limited_action")
	}
}

func TestTransitionAuthorizeRequiresMatchingController(t *testing.T) {
	t.Parallel()

	result := AuthorizeTransitionAction(TransitionAuthRequest{
		Transition: RuntimeTransition{
			State:      TransitionStateCutover,
			Controller: TransitionControllerOdinOS,
		},
		Actor:       TransitionControllerLegacyOdin,
		ActionClass: ActionClassFullMutation,
	})
	if result.Allowed {
		t.Fatalf("expected controller mismatch to deny mutation")
	}
}

func TestTransitionAuthorizeAllowsCutoverMutationForOdinOS(t *testing.T) {
	t.Parallel()

	result := AuthorizeTransitionAction(TransitionAuthRequest{
		Transition: RuntimeTransition{
			State:      TransitionStateCutover,
			Controller: TransitionControllerOdinOS,
		},
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassFullMutation,
	})
	if !result.Allowed {
		t.Fatalf("expected cutover full mutation to be allowed, got %+v", result)
	}
}

func TestTransitionControlRequiresExplicitTargetState(t *testing.T) {
	t.Parallel()

	result := ValidateTransitionChange(RuntimeTransition{
		State:      TransitionStateCompare,
		Controller: TransitionControllerLegacyOdin,
	}, TransitionChangeRequest{
		Actor: TransitionControllerOdinOS,
	})
	if result.Allowed {
		t.Fatalf("expected empty transition target to be denied")
	}
}

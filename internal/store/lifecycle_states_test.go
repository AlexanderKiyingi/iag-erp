package store

import (
	"errors"
	"testing"
	"time"
)

func allMachines() []StateMachine {
	return []StateMachine{
		RequisitionStates, ApplicationStages, OfferStates, ChecklistStates,
		ChecklistItemStates, ReviewCycleStates, ReviewStates, GoalStates,
		DisciplinaryStates, EnrolmentStates,
	}
}

// Every state a machine can reach must itself be declared, or a record can
// arrive somewhere the machine has no rules for and become stuck.
func TestEveryReachableStateIsDeclared(t *testing.T) {
	for _, m := range allMachines() {
		for from, tos := range m.Allowed {
			for _, to := range tos {
				if _, declared := m.Allowed[to]; !declared {
					t.Errorf("%s: %q → %q, but %q has no entry of its own",
						m.Name, from, to, to)
				}
			}
		}
	}
}

// A machine describing a process with an end must have a terminal state, or a
// record can never be finished with.
//
// Checklist items are the deliberate exception: every state reopens, because
// marking an asset returned and then finding it was not is ordinary, and a
// machine that forbids the correction just moves it somewhere unrecorded. The
// checklist that contains them does terminate, which is what closes the work.
func TestEveryMachineHasATerminalState(t *testing.T) {
	cyclicByDesign := map[string]bool{ChecklistItemStates.Name: true}

	for _, m := range allMachines() {
		terminal := 0
		for state := range m.Allowed {
			if m.IsTerminal(state) {
				terminal++
			}
		}
		if cyclicByDesign[m.Name] {
			if terminal != 0 {
				t.Errorf("%s is documented as cyclic but has %d terminal state(s); "+
					"update the exemption or the machine", m.Name, terminal)
			}
			continue
		}
		if terminal == 0 {
			t.Errorf("%s has no terminal state", m.Name)
		}
	}
}

// Terminal means terminal. A record that has been cancelled, closed or hired
// must not quietly resume.
func TestTerminalStatesRefuseEveryMove(t *testing.T) {
	for _, m := range allMachines() {
		for _, state := range m.States() {
			if !m.IsTerminal(state) {
				continue
			}
			for _, other := range m.States() {
				if other == state {
					continue
				}
				if m.CanTransition(state, other) {
					t.Errorf("%s: terminal %q still allows a move to %q", m.Name, state, other)
				}
			}
		}
	}
}

// Re-saving a record without changing its state must not be an error.
func TestSelfTransitionAlwaysAllowed(t *testing.T) {
	for _, m := range allMachines() {
		for _, state := range m.States() {
			if err := m.Transition(state, state); err != nil {
				t.Errorf("%s: %q → %q rejected: %v", m.Name, state, state, err)
			}
		}
	}
}

func TestTransitionErrorNamesTheAllowedMoves(t *testing.T) {
	err := ApplicationStages.Transition("applied", "offer")
	if err == nil {
		t.Fatal("expected applied → offer to be refused")
	}
	var invalid ErrInvalidTransition
	if !errors.As(err, &invalid) {
		t.Fatalf("expected ErrInvalidTransition, got %T", err)
	}
	if invalid.From != "applied" || invalid.To != "offer" {
		t.Fatalf("error lost the states: %+v", invalid)
	}
	if len(invalid.Allowed) == 0 {
		t.Fatal("error does not say what was allowed instead")
	}
}

// The pipeline must not be skippable. This is the rule the JSONB version had no
// way to express: a candidate went from 'applied' to 'hired' if somebody typed
// it.
func TestApplicationPipelineCannotSkipStages(t *testing.T) {
	forbidden := [][2]string{
		{"applied", "interview"},
		{"applied", "offer"},
		{"applied", "hired"},
		{"screening", "offer"},
		{"screening", "hired"},
		{"interview", "hired"},
		{"rejected", "hired"},
		{"withdrawn", "screening"},
	}
	for _, tc := range forbidden {
		if ApplicationStages.CanTransition(tc[0], tc[1]) {
			t.Errorf("pipeline allows skipping %q → %q", tc[0], tc[1])
		}
	}

	// And the honest path must work end to end.
	path := []string{"applied", "screening", "interview", "offer", "hired"}
	for i := 1; i < len(path); i++ {
		if err := ApplicationStages.Transition(path[i-1], path[i]); err != nil {
			t.Fatalf("normal pipeline blocked at %s → %s: %v", path[i-1], path[i], err)
		}
	}

	// Rejection and withdrawal are available at every live stage.
	for _, stage := range []string{"applied", "screening", "interview", "offer"} {
		for _, exit := range []string{"rejected", "withdrawn"} {
			if err := ApplicationStages.Transition(stage, exit); err != nil {
				t.Errorf("%s cannot end in %s: %v", stage, exit, err)
			}
		}
	}
}

// Due process, as the state machine sees it.
func TestDisciplinaryRequiresInvestigationBeforeDecision(t *testing.T) {
	if DisciplinaryStates.CanTransition("reported", "decided") {
		t.Error("a case can be decided straight from being reported")
	}
	if DisciplinaryStates.CanTransition("reported", "closed") {
		t.Error("a case can be closed without ever being looked at")
	}
	if !DisciplinaryStates.CanTransition("under_investigation", "decided") {
		t.Error("an investigated case cannot be decided")
	}
	// An appeal must be able to reopen a decision, and close from either side.
	if !DisciplinaryStates.CanTransition("decided", "appealed") {
		t.Error("a decision cannot be appealed")
	}
	if !DisciplinaryStates.CanTransition("appealed", "decided") {
		t.Error("an appeal cannot lead to a fresh decision")
	}
	// Dismissing the allegation is available before a decision, not after.
	if DisciplinaryStates.CanTransition("decided", "dismissed") {
		t.Error("a decided case can still be dismissed, erasing the outcome")
	}
}

func TestDisciplinaryOutcomeVocabulary(t *testing.T) {
	for _, o := range DisciplinaryOutcomes {
		if !IsValidDisciplinaryOutcome(o) {
			t.Errorf("%q is listed but rejected", o)
		}
	}
	for _, o := range []string{"", "fired", "warning", "DISMISSAL", "sacked"} {
		if IsValidDisciplinaryOutcome(o) {
			t.Errorf("%q was accepted as an outcome", o)
		}
	}
}

// A review ends when the employee has seen it, not when the manager has written
// it.
func TestReviewMustBeSharedBeforeAcknowledged(t *testing.T) {
	if ReviewStates.CanTransition("manager_review", "acknowledged") {
		t.Error("a review can be acknowledged without ever being shared")
	}
	if ReviewStates.CanTransition("draft", "shared") {
		t.Error("a draft review can be shared")
	}
	if !ReviewStates.CanTransition("shared", "acknowledged") {
		t.Error("a shared review cannot be acknowledged")
	}
	// A disputed review has to be able to go back to the manager.
	if !ReviewStates.CanTransition("shared", "manager_review") {
		t.Error("a shared review cannot be sent back")
	}
	if !ReviewStates.IsTerminal("acknowledged") {
		t.Error("acknowledged should be the end of the review")
	}
}

func TestChecklistItemsReopen(t *testing.T) {
	// Marking an asset returned and then finding it was not is ordinary.
	if !ChecklistItemStates.CanTransition("done", "pending") {
		t.Error("a completed checklist item cannot be reopened")
	}
	if !ChecklistItemStates.CanTransition("blocked", "done") {
		t.Error("a blocked item cannot be completed once unblocked")
	}
	// The checklist itself does not reopen: it is a record once closed.
	if ChecklistStates.CanTransition("completed", "open") {
		t.Error("a completed checklist can be reopened")
	}
}

func TestOfferHasOneLiveOutcome(t *testing.T) {
	if !OfferStates.CanTransition("sent", "accepted") || !OfferStates.CanTransition("sent", "declined") {
		t.Error("a sent offer must be able to be accepted or declined")
	}
	if OfferStates.CanTransition("accepted", "declined") {
		t.Error("an accepted offer can still be declined")
	}
	if OfferStates.CanTransition("draft", "accepted") {
		t.Error("a draft offer can be accepted before it is sent")
	}
}

func TestGoalAchievementIsFinal(t *testing.T) {
	if !GoalStates.CanTransition("active", "achieved") {
		t.Error("an active goal cannot be achieved")
	}
	if GoalStates.CanTransition("achieved", "missed") {
		t.Error("an achieved goal can be re-marked as missed")
	}
	if GoalStates.CanTransition("draft", "achieved") {
		t.Error("a draft goal can be achieved without being started")
	}
}

func TestUnknownStateIsRejected(t *testing.T) {
	for _, m := range allMachines() {
		if err := m.Transition(m.States()[0], "banana"); err == nil {
			t.Errorf("%s accepted an unknown target state", m.Name)
		}
		if m.Knows("banana") {
			t.Errorf("%s claims to know an invented state", m.Name)
		}
	}
}

func TestWeightedRating(t *testing.T) {
	// Equal weights average.
	got := WeightedRating([]ReviewRating{
		{Competency: "delivery", Rating: 4, Weight: 1},
		{Competency: "teamwork", Rating: 2, Weight: 1},
	})
	if got != 3 {
		t.Fatalf("equal weights: got %.2f, want 3", got)
	}
	// Weights shift the result toward the heavier competency.
	got = WeightedRating([]ReviewRating{
		{Competency: "delivery", Rating: 5, Weight: 3},
		{Competency: "teamwork", Rating: 1, Weight: 1},
	})
	if got != 4 {
		t.Fatalf("weighted: got %.2f, want 4", got)
	}
	// A missing weight counts as one rather than zeroing the competency out.
	got = WeightedRating([]ReviewRating{
		{Competency: "delivery", Rating: 4},
		{Competency: "teamwork", Rating: 2},
	})
	if got != 3 {
		t.Fatalf("absent weights: got %.2f, want 3", got)
	}
	if WeightedRating(nil) != 0 {
		t.Fatal("no ratings should weight to zero, not divide by zero")
	}
}

func TestEnrolmentExpiry(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	past := now.AddDate(0, -1, 0)
	future := now.AddDate(0, 1, 0)

	if !enrolmentExpired("completed", &past, now) {
		t.Error("a completed certificate past its date is not reported expired")
	}
	if enrolmentExpired("completed", &future, now) {
		t.Error("a certificate still in date is reported expired")
	}
	// No expiry date means it never lapses.
	if enrolmentExpired("completed", nil, now) {
		t.Error("a certificate with no expiry is reported expired")
	}
	// Only completed enrolments can expire — an in-progress course has not
	// certified anything to lapse.
	if enrolmentExpired("in_progress", &past, now) {
		t.Error("an unfinished enrolment is reported expired")
	}
	if enrolmentExpired("failed", &past, now) {
		t.Error("a failed enrolment is reported expired")
	}
}

package store

import (
	"fmt"
	"sort"
	"strings"
)

// State machines for the HR lifecycle domains.
//
// This is the thing the JSONB modules could not have. A record stored as
// `{"status": "hired"}` accepts any string an operator types and any order they
// type it in: a candidate could go from "applied" to "hired" without an
// interview, a disciplinary case could reach an outcome without a hearing, and
// nothing anywhere would object.
//
// The tables below say which move may follow which. They are data, and they are
// pure — no database, no request — so the rules can be read in one place and
// tested exhaustively.

// StateMachine is a set of permitted transitions between states.
type StateMachine struct {
	Name string
	// Allowed maps a state to the states that may follow it. A state with an
	// empty list is terminal.
	Allowed map[string][]string
}

// ErrInvalidTransition is returned for a move the machine does not permit. It
// carries the states involved because "invalid input" does not tell an operator
// what to do next.
type ErrInvalidTransition struct {
	Machine string
	From    string
	To      string
	Allowed []string
}

func (e ErrInvalidTransition) Error() string {
	if len(e.Allowed) == 0 {
		return fmt.Sprintf("%s: %q is final and cannot move to %q", e.Machine, e.From, e.To)
	}
	return fmt.Sprintf("%s: cannot move from %q to %q (allowed: %s)",
		e.Machine, e.From, e.To, strings.Join(e.Allowed, ", "))
}

// States returns every state the machine knows, sorted, for validation and for
// telling a caller what the vocabulary is.
func (m StateMachine) States() []string {
	seen := map[string]bool{}
	for from, tos := range m.Allowed {
		seen[from] = true
		for _, to := range tos {
			seen[to] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Knows reports whether a state exists in this machine at all.
func (m StateMachine) Knows(state string) bool {
	if _, ok := m.Allowed[state]; ok {
		return true
	}
	for _, tos := range m.Allowed {
		for _, to := range tos {
			if to == state {
				return true
			}
		}
	}
	return false
}

// CanTransition reports whether from → to is permitted.
//
// A move to the same state is permitted and is a no-op: re-saving a record
// without changing its state should not be an error.
func (m StateMachine) CanTransition(from, to string) bool {
	if from == to {
		return true
	}
	for _, allowed := range m.Allowed[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Transition validates a move, returning a described error if it is not allowed.
func (m StateMachine) Transition(from, to string) error {
	if !m.Knows(to) {
		return ErrInvalidTransition{Machine: m.Name, From: from, To: to, Allowed: m.Allowed[from]}
	}
	if !m.CanTransition(from, to) {
		return ErrInvalidTransition{Machine: m.Name, From: from, To: to, Allowed: m.Allowed[from]}
	}
	return nil
}

// IsTerminal reports whether a state has no successors.
func (m StateMachine) IsTerminal(state string) bool {
	return len(m.Allowed[state]) == 0
}

// A requisition is a request to hire that is approved before it is advertised.
var RequisitionStates = StateMachine{
	Name: "job requisition",
	Allowed: map[string][]string{
		"draft":            {"pending_approval", "cancelled"},
		"pending_approval": {"open", "draft", "cancelled"},
		"open":             {"on_hold", "filled", "cancelled"},
		"on_hold":          {"open", "cancelled"},
		"filled":           {},
		"cancelled":        {},
	},
}

// The hiring pipeline. Every stage can end in a rejection or a withdrawal,
// because both happen at every stage; what cannot happen is skipping forward.
var ApplicationStages = StateMachine{
	Name: "application",
	Allowed: map[string][]string{
		"applied":   {"screening", "rejected", "withdrawn"},
		"screening": {"interview", "rejected", "withdrawn"},
		"interview": {"offer", "rejected", "withdrawn"},
		"offer":     {"hired", "rejected", "withdrawn"},
		"hired":     {},
		"rejected":  {},
		"withdrawn": {},
	},
}

var OfferStates = StateMachine{
	Name: "offer",
	Allowed: map[string][]string{
		"draft":     {"sent", "withdrawn"},
		"sent":      {"accepted", "declined", "withdrawn", "expired"},
		"accepted":  {},
		"declined":  {},
		"withdrawn": {},
		"expired":   {},
	},
}

var ChecklistStates = StateMachine{
	Name: "checklist",
	Allowed: map[string][]string{
		"open":      {"completed", "cancelled"},
		"completed": {},
		"cancelled": {},
	},
}

// Checklist items reopen. Marking an asset returned and then finding it was
// not is ordinary, and a state machine that forbids the correction just moves
// the correction somewhere it is not recorded.
var ChecklistItemStates = StateMachine{
	Name: "checklist item",
	Allowed: map[string][]string{
		"pending":        {"done", "not_applicable", "blocked"},
		"blocked":        {"pending", "done", "not_applicable"},
		"done":           {"pending"},
		"not_applicable": {"pending"},
	},
}

var ReviewCycleStates = StateMachine{
	Name: "review cycle",
	Allowed: map[string][]string{
		"draft":  {"open", "closed"},
		"open":   {"closed"},
		"closed": {},
	},
}

// A review is written, shared, and acknowledged. It can be sent back from
// calibration or from being shared, because a review the employee disputes has
// to be able to go back to the manager.
var ReviewStates = StateMachine{
	Name: "performance review",
	Allowed: map[string][]string{
		"draft":          {"self_review", "manager_review", "cancelled"},
		"self_review":    {"manager_review", "cancelled"},
		"manager_review": {"calibration", "shared", "cancelled"},
		"calibration":    {"shared", "manager_review", "cancelled"},
		"shared":         {"acknowledged", "manager_review"},
		"acknowledged":   {},
		"cancelled":      {},
	},
}

var GoalStates = StateMachine{
	Name: "performance goal",
	Allowed: map[string][]string{
		"draft":     {"active", "cancelled"},
		"active":    {"achieved", "missed", "cancelled"},
		"achieved":  {},
		"missed":    {},
		"cancelled": {},
	},
}

// Due process, as states. A case cannot reach an outcome without having been
// investigated, and cannot close while an appeal is still open.
var DisciplinaryStates = StateMachine{
	Name: "disciplinary case",
	Allowed: map[string][]string{
		"reported":            {"under_investigation", "dismissed"},
		"under_investigation": {"hearing_scheduled", "decided", "dismissed"},
		"hearing_scheduled":   {"decided", "under_investigation", "dismissed"},
		"decided":             {"appealed", "closed"},
		"appealed":            {"decided", "closed"},
		"closed":              {},
		"dismissed":           {},
	},
}

var EnrolmentStates = StateMachine{
	Name: "training enrolment",
	Allowed: map[string][]string{
		"enrolled":    {"in_progress", "completed", "failed", "cancelled"},
		"in_progress": {"completed", "failed", "cancelled"},
		"completed":   {},
		"failed":      {},
		"cancelled":   {},
	},
}

// Outcomes a disciplinary case may be decided with. Empty means undecided.
var DisciplinaryOutcomes = []string{
	"no_case", "verbal_warning", "written_warning", "final_warning",
	"suspension", "demotion", "dismissal",
}

// IsValidDisciplinaryOutcome guards the free-text-looking outcome column.
func IsValidDisciplinaryOutcome(outcome string) bool {
	for _, o := range DisciplinaryOutcomes {
		if o == outcome {
			return true
		}
	}
	return false
}

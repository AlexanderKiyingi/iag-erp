/*
The approval desks an IAG payroll run and leave request pass through.

This service models payroll as draft -> approved -> posted and leave as
pending -> approved. The IAG HR app runs five desks and three respectively, and
before this file those desks lived only in the browser: the app posted its
approvals to a different backend, so a run approved and released by an operator
stayed in whatever state CreatePayrollRun left it here.

status keeps its meaning and chain_stage carries the desk. Everything before the
final sign-off is the subject's own pre-decision status (draft, pending) at a
named stage; the sign-off and the release are the existing permissioned verbs,
unchanged. See migrations/020_approval_chain.sql for why it is split that way
rather than by widening the status CHECK.

Nothing here invents a transition. A terminal hop delegates to ApprovePayrollRun,
PostPayrollRun, CancelPayrollRun or DecideLeaveRequest, which keep their own
guards -- self-approval, the posted-period uniqueness index, the leave balance
recomputed in the same transaction as the decision.
*/
package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ApprovalSubject names the table a chain belongs to. Mirrors the CHECK on
// erp_approval_events.subject_type.
type ApprovalSubject string

const (
	SubjectPayrollRun   ApprovalSubject = "payroll_run"
	SubjectLeaveRequest ApprovalSubject = "leave_request"
)

/*
StageReturnedForAmendment is a side state, not a desk.

A request sent back for amendment has not moved along the chain and has not left
it either -- it is with the requestor again. Keeping it out of the ordered list
means "is this a forward move" stays a comparison of two positions, and the one
state that is neither forward nor backward does not have to be special-cased
inside that comparison.
*/
const StageReturnedForAmendment = "returned_for_amendment"

// StageDraft is where a subject sits before anybody has submitted it.
const StageDraft = "draft"

var payrollChainStages = []string{
	StageDraft,
	"submitted",
	"accounts_approved",
	"gm_approved",
	// Terminal: hands off to the approve verb.
	"ceo_approved",
	// Terminal: hands off to the post verb. Money reaches the ledger here.
	"paid",
}

var leaveChainStages = []string{
	StageDraft,
	"submitted",
	"hod_approved",
	// Terminal: hands off to the decide verb.
	"approved",
}

// ChainStages lists the desks of one chain, in order.
func ChainStages(subject ApprovalSubject) []string {
	switch subject {
	case SubjectPayrollRun:
		return payrollChainStages
	case SubjectLeaveRequest:
		return leaveChainStages
	}
	return nil
}

func stageIndex(subject ApprovalSubject, stage string) int {
	stage = normaliseStage(stage)
	if stage == "" {
		stage = StageDraft
	}
	for i, s := range ChainStages(subject) {
		if s == stage {
			return i
		}
	}
	return -1
}

func normaliseStage(stage string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(stage, " ", "_")))
}

/*
IsTerminalStage reports whether reaching this stage changes the subject's status
rather than only its desk.

These are the hops that leave the chain's own bookkeeping and become facts about
the record: an approved payroll run, a posted one, a decided leave request.
*/
func IsTerminalStage(subject ApprovalSubject, stage string) bool {
	switch subject {
	case SubjectPayrollRun:
		return stage == "ceo_approved" || stage == "paid"
	case SubjectLeaveRequest:
		return stage == "approved"
	}
	return false
}

/*
ChainPermission is the codename a hop requires.

Intermediate reviews need only the permission to run the thing at all. The
terminal hops keep the permissions the service already guards them with, because
that split is the separation of duties: approving a run you prepared and
releasing money are deliberately not the same grant as computing it.

Returned as a value rather than checked here because the store does not see the
caller. The handler gates on it.
*/
func ChainPermission(subject ApprovalSubject, action, toStage string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	stage := normaliseStage(toStage)
	switch subject {
	case SubjectPayrollRun:
		if action == "advance" {
			switch stage {
			case "ceo_approved":
				return "erp.approve_payroll"
			case "paid":
				return "erp.post_payroll"
			}
		}
		// Cancelling is the preparer undoing their own work, and an amendment
		// is a request for a change rather than a decision on one.
		return "erp.run_payroll"
	case SubjectLeaveRequest:
		// Deciding leave is never erp.change_leave: editing your own pending
		// request and deciding somebody else's are different acts.
		if action == "reject" || (action == "advance" && stage == "approved") {
			return "erp.approve_leave"
		}
		return "erp.change_leave"
	}
	return ""
}

// ChainInput is one hop, as the caller describes it.
type ChainInput struct {
	// advance, reject or amend.
	Action string `json:"action"`
	// The desk being moved to. Ignored for reject and amend, which have one
	// destination each.
	ToStage string `json:"to_stage"`
	// What the approver said. Kept per hop: the question a trail answers is
	// who sent this back and why, which is about the hop, not the state.
	Note string `json:"note"`
	// Who acted. Falls back to the caller's employee number in the handler.
	ActorEmployeeNo string `json:"actor_employee_no"`
}

/*
validateHop decides whether a move is allowed by the chain's own shape.

Forward jumps are permitted -- the app lets an approver forward straight to the
CEO or to Finance, and refusing that here would make the service disagree with
the desk it is recording. Backward moves are not: going back is what amendment
is for, and it is recorded as an amendment rather than as a quiet rewind.
*/
func validateHop(subject ApprovalSubject, current, to string) error {
	toIdx := stageIndex(subject, to)
	if toIdx < 0 {
		return fmt.Errorf("%w: %q is not a stage of the %s chain", ErrBadInput, to, subject)
	}
	// Amendment sits outside the order. Coming back from it re-enters the chain
	// wherever the requestor resubmits to, which is a forward move from draft.
	if normaliseStage(current) == StageReturnedForAmendment {
		return nil
	}
	if toIdx <= stageIndex(subject, current) {
		return fmt.Errorf("%w: %s is not ahead of %s", ErrConflict, to, current)
	}
	return nil
}

/*
recordHop writes the trail entry and the new desk together.

One transaction, because a desk that moved with no event explaining it is the
gap this table exists to close.
*/
func (s *Store) recordHop(
	ctx context.Context,
	subject ApprovalSubject,
	id uuid.UUID,
	table, fromStage, toStage string,
	in ChainInput,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO erp_approval_events
			(subject_type, subject_id, action, from_stage, to_stage, note, actor_employee_no)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		string(subject), id, strings.ToLower(strings.TrimSpace(in.Action)),
		fromStage, toStage, strings.TrimSpace(in.Note),
		strings.TrimSpace(in.ActorEmployeeNo)); err != nil {
		return err
	}

	// The table name is not caller-supplied -- it is chosen by the exported
	// wrapper from a closed set of two -- so this cannot carry an injection.
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(`UPDATE %s SET chain_stage = $2 WHERE id = $1`, table),
		id, toStage); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ChainEvent is one hop as it is read back.
type ChainEvent struct {
	Action     string `json:"action"`
	FromStage  string `json:"from_stage"`
	ToStage    string `json:"to_stage"`
	Note       string `json:"note"`
	Actor      string `json:"actor_employee_no"`
	OccurredAt string `json:"occurred_at"`
}

// ListApprovalEvents returns one subject's trail, oldest first.
func (s *Store) ListApprovalEvents(
	ctx context.Context, subject ApprovalSubject, id uuid.UUID,
) ([]ChainEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT action, from_stage, to_stage, note, actor_employee_no, occurred_at
		FROM erp_approval_events
		WHERE subject_type = $1 AND subject_id = $2
		ORDER BY occurred_at, id`, string(subject), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChainEvent{}
	for rows.Next() {
		var e ChainEvent
		var occurred any
		if err := rows.Scan(&e.Action, &e.FromStage, &e.ToStage, &e.Note, &e.Actor, &occurred); err != nil {
			return nil, err
		}
		e.OccurredAt = fmt.Sprint(occurred)
		out = append(out, e)
	}
	return out, rows.Err()
}

/*
MovePayrollRunChain moves a run one hop along the desks.

Ordering, deliberately: a terminal hop performs the status change first and
records the trail second. If the record fails, the run is approved or posted
with its last hop missing from the trail -- recoverable, and status is still the
truth. The other order would write a trail entry saying a run was approved when
the approve verb had refused it, which is a false record of an approval and the
worse half by a distance.
*/
func (s *Store) MovePayrollRunChain(
	ctx context.Context, id uuid.UUID, in ChainInput,
) (*PayrollRun, error) {
	run, err := s.GetPayrollRun(ctx, id)
	if err != nil {
		return nil, err
	}
	from := run.ChainStage
	if from == "" {
		from = StageDraft
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))

	switch action {
	case "amend":
		// Status is untouched: an amendment is a request for a change, not a
		// decision on one, and the run is still a draft.
		if err := s.recordHop(ctx, SubjectPayrollRun, id, "erp_payroll_runs",
			from, StageReturnedForAmendment, in); err != nil {
			return nil, err
		}
		return s.GetPayrollRun(ctx, id)

	case "reject":
		if _, err := s.CancelPayrollRun(ctx, id); err != nil {
			return nil, err
		}
		if err := s.recordHop(ctx, SubjectPayrollRun, id, "erp_payroll_runs",
			from, "rejected", in); err != nil {
			return nil, err
		}
		return s.GetPayrollRun(ctx, id)

	case "advance":
		to := normaliseStage(in.ToStage)
		if err := validateHop(SubjectPayrollRun, from, to); err != nil {
			return nil, err
		}
		switch to {
		case "ceo_approved":
			if _, err := s.ApprovePayrollRun(ctx, id, strings.TrimSpace(in.ActorEmployeeNo)); err != nil {
				return nil, err
			}
		case "paid":
			if _, err := s.PostPayrollRun(ctx, id); err != nil {
				return nil, err
			}
		}
		if err := s.recordHop(ctx, SubjectPayrollRun, id, "erp_payroll_runs",
			from, to, in); err != nil {
			return nil, err
		}
		return s.GetPayrollRun(ctx, id)
	}
	return nil, fmt.Errorf("%w: action must be advance, reject or amend", ErrBadInput)
}

// MoveLeaveRequestChain is the same shape for leave. The terminal hop is the
// decide verb, which deducts the balance in the same transaction as the
// decision -- so approval still cannot land without its balance.
func (s *Store) MoveLeaveRequestChain(
	ctx context.Context, id uuid.UUID, in ChainInput,
) (*LeaveRequest, error) {
	req, err := s.GetLeaveRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	from := req.ChainStage
	if from == "" {
		from = StageDraft
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))

	switch action {
	case "amend":
		if err := s.recordHop(ctx, SubjectLeaveRequest, id, "erp_leave_requests",
			from, StageReturnedForAmendment, in); err != nil {
			return nil, err
		}
		return s.GetLeaveRequest(ctx, id)

	case "reject":
		if _, err := s.DecideLeaveRequest(ctx, id, "reject",
			in.ActorEmployeeNo, in.Note, in.ActorEmployeeNo); err != nil {
			return nil, err
		}
		if err := s.recordHop(ctx, SubjectLeaveRequest, id, "erp_leave_requests",
			from, "rejected", in); err != nil {
			return nil, err
		}
		return s.GetLeaveRequest(ctx, id)

	case "advance":
		to := normaliseStage(in.ToStage)
		if err := validateHop(SubjectLeaveRequest, from, to); err != nil {
			return nil, err
		}
		if to == "approved" {
			if _, err := s.DecideLeaveRequest(ctx, id, "approve",
				in.ActorEmployeeNo, in.Note, in.ActorEmployeeNo); err != nil {
				return nil, err
			}
		}
		if err := s.recordHop(ctx, SubjectLeaveRequest, id, "erp_leave_requests",
			from, to, in); err != nil {
			return nil, err
		}
		return s.GetLeaveRequest(ctx, id)
	}
	return nil, fmt.Errorf("%w: action must be advance, reject or amend", ErrBadInput)
}

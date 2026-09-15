package store

import (
	"errors"
	"testing"
)

// The desks have to be in the order an operator walks them, because "is this a
// forward move" is decided by comparing positions in this slice. A reordering
// would silently permit a backward hop and call it an advance.
func TestChainStagesAreInDeskOrder(t *testing.T) {
	payroll := ChainStages(SubjectPayrollRun)
	want := []string{"draft", "submitted", "accounts_approved", "gm_approved", "ceo_approved", "paid"}
	if len(payroll) != len(want) {
		t.Fatalf("payroll chain has %d stages, want %d", len(payroll), len(want))
	}
	for i := range want {
		if payroll[i] != want[i] {
			t.Errorf("payroll stage %d is %q, want %q", i, payroll[i], want[i])
		}
	}

	leave := ChainStages(SubjectLeaveRequest)
	wantLeave := []string{"draft", "submitted", "hod_approved", "approved"}
	for i := range wantLeave {
		if leave[i] != wantLeave[i] {
			t.Errorf("leave stage %d is %q, want %q", i, leave[i], wantLeave[i])
		}
	}
}

func TestOnlyStatusChangingStagesAreTerminal(t *testing.T) {
	// These two are the hops that leave the chain's bookkeeping and become facts
	// about the record, so they are the ones that delegate to a permissioned
	// verb. Marking an intermediate desk terminal would approve a run at the
	// Accounts Assistant.
	for _, stage := range []string{"ceo_approved", "paid"} {
		if !IsTerminalStage(SubjectPayrollRun, stage) {
			t.Errorf("payroll %q should be terminal", stage)
		}
	}
	for _, stage := range []string{"draft", "submitted", "accounts_approved", "gm_approved"} {
		if IsTerminalStage(SubjectPayrollRun, stage) {
			t.Errorf("payroll %q must not be terminal — it changes no status", stage)
		}
	}
	if !IsTerminalStage(SubjectLeaveRequest, "approved") {
		t.Error("leave approval is terminal")
	}
	if IsTerminalStage(SubjectLeaveRequest, "hod_approved") {
		t.Error("an HOD approval is a desk, not a decision")
	}
}

// The mappings that are easy to get wrong, and expensive when they are: each of
// these is a separation of duties that exists precisely because holding one
// grant is deliberately not holding the next.
func TestChainPermissionKeepsTheDutiesSeparate(t *testing.T) {
	cases := []struct {
		name    string
		subject ApprovalSubject
		action  string
		stage   string
		want    string
	}{
		{"CEO sign-off is not the same grant as running payroll",
			SubjectPayrollRun, "advance", "ceo_approved", "erp.approve_payroll"},
		{"releasing money is its own grant again",
			SubjectPayrollRun, "advance", "paid", "erp.post_payroll"},
		{"an intermediate desk needs only run_payroll",
			SubjectPayrollRun, "advance", "gm_approved", "erp.run_payroll"},
		{"cancelling is the preparer undoing their own work",
			SubjectPayrollRun, "reject", "", "erp.run_payroll"},
		{"an amendment is a request for a change, not a decision",
			SubjectPayrollRun, "amend", "", "erp.run_payroll"},

		{"deciding leave is approve_leave, never change_leave",
			SubjectLeaveRequest, "advance", "approved", "erp.approve_leave"},
		{"rejecting leave is a decision too",
			SubjectLeaveRequest, "reject", "", "erp.approve_leave"},
		{"an HOD hop is not yet a decision",
			SubjectLeaveRequest, "advance", "hod_approved", "erp.change_leave"},
	}
	for _, tc := range cases {
		if got := ChainPermission(tc.subject, tc.action, tc.stage); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestValidateHopRefusesBackwardsAndUnknownStages(t *testing.T) {
	if err := validateHop(SubjectPayrollRun, "submitted", "gm_approved"); err != nil {
		t.Errorf("forward hop should be allowed: %v", err)
	}
	// Forward jumps are allowed on purpose: the app lets an approver forward
	// straight to the CEO, and refusing it here would make the service disagree
	// with the desk it is recording.
	if err := validateHop(SubjectPayrollRun, "submitted", "paid"); err != nil {
		t.Errorf("forward jump should be allowed: %v", err)
	}
	if err := validateHop(SubjectPayrollRun, "gm_approved", "submitted"); !errors.Is(err, ErrConflict) {
		t.Errorf("backward hop should conflict, got %v", err)
	}
	// Same desk twice is not a move; it would write a second trail entry saying
	// nothing happened.
	if err := validateHop(SubjectPayrollRun, "gm_approved", "gm_approved"); !errors.Is(err, ErrConflict) {
		t.Errorf("a hop to the current desk should conflict, got %v", err)
	}
	if err := validateHop(SubjectPayrollRun, "draft", "hod_approved"); !errors.Is(err, ErrBadInput) {
		t.Errorf("a leave desk is not a payroll desk, got %v", err)
	}
	// Coming back from an amendment re-enters the chain, which is not backward.
	if err := validateHop(SubjectPayrollRun, StageReturnedForAmendment, "submitted"); err != nil {
		t.Errorf("resubmitting after an amendment should be allowed: %v", err)
	}
}

// An empty chain_stage is every row that existed before the chain did. It has
// to read as the start of the chain rather than as an unknown stage, or the
// first hop on a pre-existing run would be rejected as invalid.
func TestEmptyStageIsTheStartOfTheChain(t *testing.T) {
	if stageIndex(SubjectPayrollRun, "") != 0 {
		t.Error("an empty stage should read as draft")
	}
	if err := validateHop(SubjectPayrollRun, "", "submitted"); err != nil {
		t.Errorf("a run that never went through the desks can still enter them: %v", err)
	}
}

// The app sends its own labels. "GM Approved" and "gm_approved" are the same
// desk, and a chain that refused the first would work only for callers who
// already spoke the service's dialect.
func TestStageNamesAreNormalised(t *testing.T) {
	if err := validateHop(SubjectPayrollRun, "Submitted", "GM Approved"); err != nil {
		t.Errorf("labels from the app should normalise: %v", err)
	}
	if ChainPermission(SubjectPayrollRun, "advance", "CEO Approved") != "erp.approve_payroll" {
		t.Error("a Title Case terminal stage must still raise the permission")
	}
}

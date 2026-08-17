package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrBadInput = errors.New("bad input")

	// ErrInsufficientLeave means the request costs more days than the employee
	// has left. Distinct from ErrBadInput so the caller can say which it was:
	// "you have 3 days left" is actionable, "invalid input" is not.
	ErrInsufficientLeave = errors.New("insufficient leave balance")

	// ErrNoWorkingDays means the requested range contains no working day.
	ErrNoWorkingDays = errors.New("range contains no working days")

	// ErrForbidden means the caller may use the endpoint but not on this record
	// — someone else's employee, leave or pay. Distinct from a missing
	// permission, which the middleware rejects before the store is reached.
	ErrForbidden = errors.New("forbidden")
)

type Store struct {
	pool            *pgxpool.Pool
	events          EventPublisher
	workWeek        WorkWeek
	leaveCheckBasis string
}

// SetLeaveCheckBasis selects which balance a new leave request is checked
// against: the year's entitlement, or what has accrued by the request date.
// Called once at boot; an unrecognised value keeps the entitlement default.
func (s *Store) SetLeaveCheckBasis(basis string) {
	if basis == LeaveBasisAccrual || basis == LeaveBasisEntitlement {
		s.leaveCheckBasis = basis
	}
}

// LeaveCheckBasis is the policy in force.
func (s *Store) LeaveCheckBasis() string {
	if s.leaveCheckBasis == "" {
		return LeaveBasisEntitlement
	}
	return s.leaveCheckBasis
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

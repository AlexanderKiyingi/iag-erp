package store

import (
	"context"
)

func (s *Store) ImportEmployees(ctx context.Context, items []CreateEmployeeInput) (int, error) {
	if len(items) == 0 {
		return 0, ErrBadInput
	}
	n := 0
	for _, in := range items {
		if _, err := s.CreateEmployee(ctx, in); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Store) ImportLeaveRequests(ctx context.Context, items []CreateLeaveRequestInput) (int, error) {
	if len(items) == 0 {
		return 0, ErrBadInput
	}
	n := 0
	for _, in := range items {
		if _, err := s.CreateLeaveRequest(ctx, in); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Store) ImportAttendance(ctx context.Context, items []CreateAttendanceInput) (int, error) {
	if len(items) == 0 {
		return 0, ErrBadInput
	}
	n := 0
	for _, in := range items {
		if _, err := s.UpsertAttendance(ctx, in); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

package store

import (
	"context"
	"fmt"
)

type ImportRowError struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

type ImportResult struct {
	Imported int              `json:"imported"`
	Failed   []ImportRowError `json:"failed,omitempty"`
}

func (s *Store) ImportEmployees(ctx context.Context, items []CreateEmployeeInput) (ImportResult, error) {
	if len(items) == 0 {
		return ImportResult{}, ErrBadInput
	}
	var out ImportResult
	for i, in := range items {
		if _, err := s.CreateEmployee(ctx, in); err != nil {
			out.Failed = append(out.Failed, ImportRowError{Index: i, Message: err.Error()})
			continue
		}
		out.Imported++
	}
	if out.Imported == 0 && len(out.Failed) > 0 {
		return out, fmt.Errorf("all rows failed")
	}
	return out, nil
}

func (s *Store) ImportLeaveRequests(ctx context.Context, items []CreateLeaveRequestInput) (ImportResult, error) {
	if len(items) == 0 {
		return ImportResult{}, ErrBadInput
	}
	var out ImportResult
	for i, in := range items {
		if _, err := s.CreateLeaveRequest(ctx, in); err != nil {
			out.Failed = append(out.Failed, ImportRowError{Index: i, Message: err.Error()})
			continue
		}
		out.Imported++
	}
	if out.Imported == 0 && len(out.Failed) > 0 {
		return out, fmt.Errorf("all rows failed")
	}
	return out, nil
}

func (s *Store) ImportAttendance(ctx context.Context, items []CreateAttendanceInput) (ImportResult, error) {
	if len(items) == 0 {
		return ImportResult{}, ErrBadInput
	}
	var out ImportResult
	for i, in := range items {
		if _, err := s.UpsertAttendance(ctx, in); err != nil {
			out.Failed = append(out.Failed, ImportRowError{Index: i, Message: err.Error()})
			continue
		}
		out.Imported++
	}
	if out.Imported == 0 && len(out.Failed) > 0 {
		return out, fmt.Errorf("all rows failed")
	}
	return out, nil
}

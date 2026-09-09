package store

import "strings"

// The closed vocabularies HR columns are constrained to.
//
// Each of these is a database CHECK, and until now the CHECK was the only place
// the list existed. That put the vocabulary out of reach of the code that
// accepts values for it, with two consequences seen in the field:
//
//   - A frontend offering the same concept in a different shape ("Half-day"
//     for 'half_day', Title Case for lower) failed at the constraint, deep
//     inside a store call, as a 500. The value was never wrong — only
//     unrepresentable — and the operator was told the server broke.
//   - Two write paths for the same column normalised differently.
//     CreateEmployee lower-cased employment_type; UpdateEmployee did not
//     lower-case status, so picking "Active" from a dropdown 500'd on update
//     while creating the same employee succeeded.
//
// Normalising here turns both into an accepted value, and anything genuinely
// outside the vocabulary into ErrBadInput — a 400 that names the field —
// rather than a constraint violation. The lists are paired against the
// migrations by TestHRVocabularyMatchesMigrationConstraints, so widening one
// without the other fails the build.

// EmploymentTypes are the values erp_employees.employment_type and
// erp_job_requisitions.employment_type accept. A requisition and the employee
// hired into it must share the list or the requisition cannot be filled.
var EmploymentTypes = []string{"permanent", "contract", "casual", "intern"}

// EmployeeStatuses are the values erp_employees.status accepts.
var EmployeeStatuses = []string{"active", "on_leave", "terminated", "probation", "suspended"}

// AttendanceStatuses are the values erp_attendance_records.status accepts.
var AttendanceStatuses = []string{"present", "absent", "half_day", "leave", "late", "remote", "holiday"}

// normaliseVocabulary maps a caller's spelling onto the stored one.
//
// Case and the separator are the two things callers get differently and the
// two that carry no meaning: "Half-day", "half day" and "HALF_DAY" are all the
// same outcome. Anything else is left alone so it fails as the unknown value it
// is, rather than being bent into a neighbouring one — silently recording
// somebody as 'present' because 'psesent' was close would be worse than the
// rejection.
func normaliseVocabulary(value string, allowed []string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", true // absent; the caller decides whether a default applies
	}
	candidate := strings.ReplaceAll(strings.ToLower(trimmed), "-", "_")
	candidate = strings.ReplaceAll(candidate, " ", "_")
	for _, valid := range allowed {
		if candidate == valid {
			return valid, true
		}
	}
	return "", false
}

// NormaliseEmploymentType accepts any spelling of a known employment type.
func NormaliseEmploymentType(value string) (string, bool) {
	return normaliseVocabulary(value, EmploymentTypes)
}

// NormaliseEmployeeStatus accepts any spelling of a known employee status.
func NormaliseEmployeeStatus(value string) (string, bool) {
	return normaliseVocabulary(value, EmployeeStatuses)
}

// NormaliseAttendanceStatus accepts any spelling of a known attendance status.
func NormaliseAttendanceStatus(value string) (string, bool) {
	return normaliseVocabulary(value, AttendanceStatuses)
}

package store

import (
	"context"
	"strings"
)

// ResolveDepartmentFilter maps a UI department label (name or code) to erp_departments.code.
func (s *Store) ResolveDepartmentFilter(ctx context.Context, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" || strings.EqualFold(input, "all departments") {
		return "", nil
	}
	upper := strings.ToUpper(input)
	var code string
	err := s.pool.QueryRow(ctx, `
		SELECT code FROM erp_departments
		WHERE code = $1 OR name ILIKE $1
		LIMIT 1`, upper).Scan(&code)
	if err != nil {
		// UI aliases used by HRMIAG
		aliases := map[string]string{
			"PRODUCTION":      "PROD",
			"QUALITY CONTROL": "QC",
			"HUMAN RESOURCES": "HR",
			"HR":              "HR",
			"LOGISTICS":       "LOG",
			"MAINTENANCE":     "MAINT",
		}
		if c, ok := aliases[upper]; ok {
			return c, nil
		}
		return input, nil
	}
	return code, nil
}

// ResolvePlantFilter maps worksite UI labels to plant_code values.
func ResolvePlantFilter(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	aliases := map[string]string{
		"Africa Coffee Park · Ntungamo": "kampala",
		"Kampala Regional Office":       "kampala",
		"Mbarara Processing Unit":       "mbale",
		"Entebbe Warehouse":             "kampala",
		"kampala":                       "kampala",
		"mbale":                         "mbale",
	}
	if code, ok := aliases[input]; ok {
		return code
	}
	if idx := strings.Index(input, "·"); idx > 0 {
		return strings.TrimSpace(input[:idx])
	}
	return strings.ToLower(input)
}

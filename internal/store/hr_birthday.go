package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"iag-erp/backend/internal/config"
	"iag-erp/backend/internal/notify"
)

type BirthdayReminderResult struct {
	EmployeesMatched int `json:"employees_matched"`
	Sent             int `json:"sent"`
}

func matchesBirthday(birthDate, today time.Time) bool {
	return birthDate.Month() == today.Month() && birthDate.Day() == today.Day()
}

type birthdayCandidate struct {
	EmployeeNo     string
	FirstName      string
	LastName       string
	Email          *string
	DepartmentCode *string
	JobTitle       string
	ManagerEmail   *string
}

func (s *Store) SendBirthdayReminders(ctx context.Context, pub *notify.Publisher, cfg *config.Config) (BirthdayReminderResult, error) {
	var out BirthdayReminderResult
	if pub == nil || !pub.Enabled() {
		return out, ErrBadInput
	}
	today := time.Now().UTC()
	reminderOn := today.Truncate(24 * time.Hour)
	candidates, err := s.listBirthdayCandidates(ctx, int(today.Month()), today.Day())
	if err != nil {
		return out, err
	}
	out.EmployeesMatched = len(candidates)
	hrEmails, err := s.listHRNotifyEmails(ctx, cfg)
	if err != nil {
		return out, err
	}

	for _, emp := range candidates {
		name := strings.TrimSpace(emp.FirstName + " " + emp.LastName)
		dept := ""
		if emp.DepartmentCode != nil {
			dept = *emp.DepartmentCode
		}
		vars := map[string]string{
			"EmployeeName":  name,
			"EmployeeNo":    emp.EmployeeNo,
			"Department":    dept,
			"JobTitle":      emp.JobTitle,
			"BirthdayDate":  reminderOn.Format("2 January"),
			"Title":         "Birthday reminder",
		}

		if emp.Email != nil && strings.TrimSpace(*emp.Email) != "" {
			sent, err := s.sendBirthdayReminder(ctx, pub, reminderOn, emp.EmployeeNo, "employee", strings.TrimSpace(*emp.Email), notify.TemplateBirthdayEmployee, vars, cfg)
			if err != nil {
				return out, err
			}
			out.Sent += sent
		}

		if emp.ManagerEmail != nil && strings.TrimSpace(*emp.ManagerEmail) != "" {
			mgrVars := copyVars(vars)
			mgrVars["Title"] = "Team member birthday today"
			mgrVars["Body"] = fmt.Sprintf("%s (%s) is celebrating a birthday today.", name, emp.EmployeeNo)
			sent, err := s.sendBirthdayReminder(ctx, pub, reminderOn, emp.EmployeeNo, "manager", strings.TrimSpace(*emp.ManagerEmail), notify.TemplateBirthdayManager, mgrVars, cfg)
			if err != nil {
				return out, err
			}
			out.Sent += sent
		}

		for _, hrEmail := range hrEmails {
			hrVars := copyVars(vars)
			hrVars["Title"] = "Employee birthday today"
			hrVars["Body"] = fmt.Sprintf("%s (%s) has a birthday today.", name, emp.EmployeeNo)
			sent, err := s.sendBirthdayReminder(ctx, pub, reminderOn, emp.EmployeeNo, "hr", hrEmail, notify.TemplateBirthdayHR, hrVars, cfg)
			if err != nil {
				return out, err
			}
			out.Sent += sent
		}
	}
	return out, nil
}

func copyVars(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *Store) sendBirthdayReminder(ctx context.Context, pub *notify.Publisher, reminderOn time.Time, employeeNo, role, email, templateID string, vars map[string]string, cfg *config.Config) (int, error) {
	eventID := fmt.Sprintf("erp.birthday:%s:%s:%s:%s", employeeNo, reminderOn.Format("2006-01-02"), role, strings.ToLower(email))
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO erp_birthday_reminder_log (employee_no, reminder_on, recipient_role, recipient_email, event_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (employee_no, reminder_on, recipient_role, recipient_email) DO NOTHING`,
		employeeNo, reminderOn, role, email, eventID)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, nil
	}
	if cfg != nil && cfg.AppName != "" {
		vars["AppName"] = cfg.AppName
	}
	if err := pub.PublishEmail(ctx, eventID, email, templateID, vars); err != nil {
		return 0, err
	}
	return 1, nil
}

func (s *Store) listBirthdayCandidates(ctx context.Context, month, day int) ([]birthdayCandidate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.employee_no, e.first_name, e.last_name, e.email, d.code, e.job_title, m.email
		FROM erp_employees e
		LEFT JOIN erp_departments d ON d.id = e.department_id
		LEFT JOIN erp_employees m ON m.id = e.manager_id
		WHERE e.status IN ('active', 'on_leave')
		  AND e.birth_date IS NOT NULL
		  AND EXTRACT(MONTH FROM e.birth_date) = $1
		  AND EXTRACT(DAY FROM e.birth_date) = $2
		ORDER BY e.last_name, e.first_name`, month, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []birthdayCandidate
	for rows.Next() {
		var c birthdayCandidate
		if err := rows.Scan(&c.EmployeeNo, &c.FirstName, &c.LastName, &c.Email, &c.DepartmentCode, &c.JobTitle, &c.ManagerEmail); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) listHRNotifyEmails(ctx context.Context, cfg *config.Config) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	add := func(email string) {
		email = strings.TrimSpace(strings.ToLower(email))
		if email == "" {
			return
		}
		if _, ok := seen[email]; ok {
			return
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	if cfg != nil {
		for _, e := range cfg.HRBirthdayNotifyEmails {
			add(e)
		}
	}
	dept := "HR"
	if cfg != nil && strings.TrimSpace(cfg.HRBirthdayDepartmentCode) != "" {
		dept = strings.ToUpper(strings.TrimSpace(cfg.HRBirthdayDepartmentCode))
	}
	rows, err := s.pool.Query(ctx, `
		SELECT e.email FROM erp_employees e
		JOIN erp_departments d ON d.id = e.department_id
		WHERE d.code = $1 AND e.status = 'active' AND e.email IS NOT NULL AND TRIM(e.email) <> ''`,
		dept)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var email *string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		if email != nil {
			add(*email)
		}
	}
	return out, rows.Err()
}

package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/alvor-technologies/iag-platform-go/corsenv"
	"github.com/alvor-technologies/iag-platform-go/runtimeenv"
	"github.com/joho/godotenv"
)

type Config struct {
	Environment string
	ServiceName string
	Port        string
	LogLevel    string

	DatabaseURL string
	AutoMigrate bool

	AuthMode                 string
	JWTIssuer                string
	JWKSURL                  string
	Audience                 string
	ServiceClientID          string
	ServiceClientSecret      string
	AuthTokenURL             string
	CORSOrigins              []string
	GatewayAPIPrefix         string
	KafkaBrokers             []string
	KafkaClientID            string
	KafkaOperationsTopic     string
	KafkaNotificationsTopic  string
	EventBusEnabled          bool
	HRBirthdayNotifyEmails   []string
	HRBirthdayDepartmentCode string
	AppName                  string

	// HRScopeEnforced narrows every HR endpoint to the caller's own record and
	// reporting tree unless they hold erp.view_all_hr. Off by default: turning
	// it on changes what existing tokens can reach, so it is rolled out after
	// the HR groups have been granted that permission.
	HRScopeEnforced bool

	// HRWorkWeek is the deployment's working weekdays as ISO numbers
	// (1 = Monday … 7 = Sunday). Leave is charged in working days, so this
	// decides what a leave request costs. Defaults to Monday–Friday.
	HRWorkWeek string

	// HRLeaveCheckBasis decides what a new leave request is checked against:
	// "entitlement" (the whole year available from 1 January, the historical
	// behaviour) or "accrual" (only what has been earned by that date).
	HRLeaveCheckBasis string

	// PayrollEnabled exposes the payroll endpoints. Off by default: payroll
	// computes statutory deductions against live pay data, and it should not be
	// reachable anywhere the tax tables have not been reviewed.
	PayrollEnabled bool
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	env := strings.ToLower(strings.TrimSpace(getenv("ENVIRONMENT", "development")))
	authMode := strings.ToLower(strings.TrimSpace(getenv("AUTH_MODE", "jwt")))
	if authMode != "jwt" {
		return nil, fmt.Errorf("AUTH_MODE must be jwt (got %q)", authMode)
	}

	c := &Config{
		Environment:              env,
		ServiceName:              getenv("SERVICE_NAME", "erp"),
		Port:                     getenv("PORT", "4001"),
		LogLevel:                 getenv("LOG_LEVEL", "info"),
		DatabaseURL:              strings.TrimSpace(os.Getenv("DATABASE_URL")),
		AutoMigrate:              getenv("AUTO_MIGRATE", "true") != "false",
		AuthMode:                 authMode,
		JWTIssuer:                getenv("JWT_ISSUER", "http://localhost:3001"),
		JWKSURL:                  getenv("JWKS_URL", "http://localhost:3001/.well-known/jwks.json"),
		Audience:                 getenv("AUDIENCE", "iag.erp"),
		ServiceClientID:          getenv("SERVICE_CLIENT_ID", "iag-erp"),
		ServiceClientSecret:      os.Getenv("SERVICE_CLIENT_SECRET"),
		CORSOrigins:              splitCSV(corsenv.Allowlist("http://localhost:3000,http://localhost:8080")),
		GatewayAPIPrefix:         getenv("GATEWAY_API_PREFIX", "/api/v1/erp"),
		KafkaBrokers:             splitCSV(getenv("KAFKA_BROKERS", "")),
		KafkaClientID:            getenv("KAFKA_CLIENT_ID", "iag-erp"),
		KafkaOperationsTopic:     getenv("KAFKA_OPERATIONS_TOPIC", "iag.operations"),
		KafkaNotificationsTopic:  getenv("KAFKA_NOTIFICATIONS_TOPIC", "iag.notifications"),
		EventBusEnabled:          getenv("EVENT_BUS_ENABLED", "true") != "false",
		HRBirthdayNotifyEmails:   splitCSV(getenv("HR_BIRTHDAY_NOTIFY_EMAILS", "")),
		HRBirthdayDepartmentCode: getenv("HR_BIRTHDAY_DEPARTMENT_CODE", "HR"),
		AppName:                  getenv("APP_NAME", "IAG Platform"),
		HRScopeEnforced:          getenv("HR_SCOPE_ENFORCED", "false") == "true",
		HRWorkWeek:               getenv("HR_WORK_WEEK", "1,2,3,4,5"),
		HRLeaveCheckBasis:        strings.ToLower(getenv("HR_LEAVE_CHECK_BASIS", "entitlement")),
		PayrollEnabled:           getenv("PAYROLL_ENABLED", "false") == "true",
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if c.AuthTokenURL == "" {
		c.AuthTokenURL = strings.TrimRight(c.JWTIssuer, "/") + "/oauth/token"
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if c.IsProduction() {
		if c.HasWildcardCORS() {
			return fmt.Errorf("set ALLOWED_ORIGINS in production (not *)")
		}
		if c.ServiceClientSecret == "" {
			return fmt.Errorf("SERVICE_CLIENT_SECRET is required in production")
		}
		if len(c.ServiceClientSecret) < 16 {
			return fmt.Errorf("SERVICE_CLIENT_SECRET must be at least 16 characters in production")
		}
		if c.AutoMigrate {
			return fmt.Errorf("AUTO_MIGRATE must be false in production (run migrations out of band)")
		}
	}
	return nil
}

func (c Config) IsProduction() bool {
	return c.Environment == "production" || c.Environment == "prod"
}

// StrictRBAC denies access when a verified token carries no permissions
// (fail-closed).
func (c Config) StrictRBAC() bool { return c.HardenedRuntime() }

// HardenedRuntime reports whether production safeguards apply.
//
// The decision lives in shared/platform-go/runtimeenv so there is one copy of
// it. It used to be inlined here, and in twelve other services, which is how
// two of them were missed when the fail-open bug was fixed.
func (c Config) HardenedRuntime() bool {
	return runtimeenv.Hardened(c.Environment)
}

func (c Config) HasWildcardCORS() bool {
	for _, o := range c.CORSOrigins {
		if strings.TrimSpace(o) == "*" {
			return true
		}
	}
	return false
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

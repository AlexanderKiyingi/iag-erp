package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/alvor-technologies/iag-platform-go/corsenv"
	"github.com/joho/godotenv"
)

type Config struct {
	Environment string
	ServiceName string
	Port        string
	LogLevel    string

	DatabaseURL string
	AutoMigrate bool

	AuthMode            string
	JWTIssuer           string
	JWKSURL             string
	Audience            string
	ServiceClientID     string
	ServiceClientSecret string
	AuthTokenURL        string
	CORSOrigins         []string
	GatewayAPIPrefix    string
	KafkaBrokers        []string
	KafkaClientID       string
	KafkaOperationsTopic   string
	KafkaNotificationsTopic string
	EventBusEnabled        bool
	HRBirthdayNotifyEmails []string
	HRBirthdayDepartmentCode string
	AppName                string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	env := strings.ToLower(strings.TrimSpace(getenv("ENVIRONMENT", "development")))
	authMode := strings.ToLower(strings.TrimSpace(getenv("AUTH_MODE", "jwt")))
	if authMode != "jwt" {
		return nil, fmt.Errorf("AUTH_MODE must be jwt (got %q)", authMode)
	}

	c := &Config{
		Environment:         env,
		ServiceName:           getenv("SERVICE_NAME", "erp"),
		Port:                  getenv("PORT", "4001"),
		LogLevel:              getenv("LOG_LEVEL", "info"),
		DatabaseURL:           strings.TrimSpace(os.Getenv("DATABASE_URL")),
		AutoMigrate:           getenv("AUTO_MIGRATE", "true") != "false",
		AuthMode:              authMode,
		JWTIssuer:             getenv("JWT_ISSUER", "http://localhost:3001"),
		JWKSURL:               getenv("JWKS_URL", "http://localhost:3001/.well-known/jwks.json"),
		Audience:              getenv("AUDIENCE", "iag.erp"),
		ServiceClientID:       getenv("SERVICE_CLIENT_ID", "iag-erp"),
		ServiceClientSecret:   os.Getenv("SERVICE_CLIENT_SECRET"),
		CORSOrigins:           splitCSV(corsenv.Allowlist("http://localhost:3000,http://localhost:8080")),
		GatewayAPIPrefix:      getenv("GATEWAY_API_PREFIX", "/api/v1/erp"),
		KafkaBrokers:          splitCSV(getenv("KAFKA_BROKERS", "")),
		KafkaClientID:         getenv("KAFKA_CLIENT_ID", "iag-erp"),
		KafkaOperationsTopic:    getenv("KAFKA_OPERATIONS_TOPIC", "iag.operations"),
		KafkaNotificationsTopic: getenv("KAFKA_NOTIFICATIONS_TOPIC", "iag.notifications"),
		EventBusEnabled:         getenv("EVENT_BUS_ENABLED", "true") != "false",
		HRBirthdayNotifyEmails:  splitCSV(getenv("HR_BIRTHDAY_NOTIFY_EMAILS", "")),
		HRBirthdayDepartmentCode: getenv("HR_BIRTHDAY_DEPARTMENT_CODE", "HR"),
		AppName:                 getenv("APP_NAME", "IAG Platform"),
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
// It deliberately does not just return IsProduction(). That required
// ENVIRONMENT=production, which the Railway runbooks never told anyone to set,
// so a hosted instance fell back to the "development" default and ran
// fail-OPEN: the permission middleware grants EVERY permission to a token
// carrying an empty permissions array. An unset ENVIRONMENT on a deployed
// instance now hardens instead; only an explicit dev-like value opts out.
//
// This cannot prevent boot — the worst case is a 403 for a caller that should
// never have had access. Boot-time validation stays keyed on ENVIRONMENT alone.
//
// Mirrors iag-fleet's config.HardenedRuntime; the intent is one shared
// implementation in shared/platform-go once every service is on it.
func (c Config) HardenedRuntime() bool {
	// An explicit production value always hardens, including on a Config built
	// by hand in a test rather than through Load.
	if c.IsProduction() {
		return true
	}
	if environmentExplicitlySet() {
		return !c.isDevLike()
	}
	return deployedRuntime()
}

// isDevLike reports an environment where fail-open behaviour is a deliberate
// local convenience rather than an accident.
func (c Config) isDevLike() bool {
	switch c.Environment {
	case "development", "dev", "local", "test":
		return true
	}
	return false
}

// environmentExplicitlySet distinguishes a deliberately configured environment
// from the "development" value Load falls back to when nothing is set. Read
// from the process rather than captured on Config: StrictRBAC is resolved once
// during startup wiring, and the environment does not change under us.
func environmentExplicitlySet() bool {
	return strings.TrimSpace(os.Getenv("ENVIRONMENT")) != "" ||
		strings.TrimSpace(os.Getenv("APP_ENV")) != ""
}

// deployedRuntime distinguishes a hosted instance from a laptop: Railway's
// injected variables, or gin in release mode, which the Dockerfiles set.
func deployedRuntime() bool {
	if strings.TrimSpace(os.Getenv("RAILWAY_ENVIRONMENT")) != "" ||
		strings.TrimSpace(os.Getenv("RAILWAY_PROJECT_ID")) != "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GIN_MODE")), "release")
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

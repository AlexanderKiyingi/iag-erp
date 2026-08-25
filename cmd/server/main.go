package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alvor-technologies/iag-platform-go/authclient"
	platformotel "github.com/alvor-technologies/iag-platform-go/otel"

	"iag-erp/backend/internal/auditlog"
	"iag-erp/backend/internal/config"
	"iag-erp/backend/internal/db"
	"iag-erp/backend/internal/events"
	"iag-erp/backend/internal/handlers"
	"iag-erp/backend/internal/middleware"
	"iag-erp/backend/internal/migrate"
	"iag-erp/backend/internal/notify"
	"iag-erp/backend/internal/outbox"
	"iag-erp/backend/internal/store"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// OpenTelemetry → otel-collector:4317 (non-blocking dial).
	if tp, err := platformotel.Init(ctx, platformotel.Config{
		ServiceName: cfg.ServiceName,
		Environment: cfg.Environment,
	}); err != nil {
		log.Printf("otel disabled: %v", err)
	} else {
		defer func() {
			sc, c := context.WithTimeout(context.Background(), 5*time.Second)
			defer c()
			_ = tp.Shutdown(sc)
		}()
	}

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	if cfg.AutoMigrate {
		if err := migrate.Up(ctx, pool); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	}

	st := store.New(pool)

	// A widened allowlist in Go and the CHECK constraint that mirrors it land at
	// different moments — the binary on restart, the migration when it runs. In
	// between, those modules read fine and fail every write, which is only
	// discovered by someone attempting one. Say it at boot instead.
	if gap, err := st.HRModuleConstraintGap(ctx); err != nil {
		log.Printf("hr module constraint check failed: %v", err)
	} else if len(gap) > 0 {
		log.Printf(
			"WARNING: erp_hr_module_records rejects module(s) %v that this build accepts. "+
				"migrations/012_hr_module_keys.sql has not been applied — those modules "+
				"will read but fail every write. Apply it, or set AUTO_MIGRATE=true.",
			gap,
		)
	}
	// Leave is charged in working days, so the work week decides what a leave
	// request costs and what the resulting liability is worth.
	st.SetWorkWeek(store.ParseWorkWeek(cfg.HRWorkWeek))
	st.SetLeaveCheckBasis(cfg.HRLeaveCheckBasis)
	auditStore := auditlog.NewStore(pool)
	outboxStore := outbox.NewStore(pool)

	bus := events.New(events.Config{
		Brokers:         cfg.KafkaBrokers,
		Enabled:         cfg.EventBusEnabled && len(cfg.KafkaBrokers) > 0,
		OperationsTopic: cfg.KafkaOperationsTopic,
	})
	bus.SetOutbox(outboxStore)
	st.SetEventBus(bus)
	defer bus.Close()

	if bus.Enabled() {
		pub := outbox.NewPublisher(outboxStore, bus)
		go pub.Run(ctx)
	}

	var verifier *authclient.Verifier
	if cfg.AuthMode == "jwt" {
		verifier = authclient.NewVerifier(authclient.Options{
			JWKSURL:  cfg.JWKSURL,
			Issuer:   cfg.JWTIssuer,
			Audience: cfg.Audience,
		})
		// Never crash-loop the whole service on a transient JWKS hiccup at boot:
		// retry briefly, then degrade to the background refresh loop below. Until
		// keys load, tokens fail closed (401) — far better than a 503 outage.
		if err := refreshJWKSWithRetry(ctx, verifier); err != nil {
			log.Printf("warning: initial jwks refresh failed after retries; continuing with background refresh: %v", err)
		}
		go jwksRefreshLoop(verifier)
	}

	platformAuth := middleware.NewPlatformAuth(middleware.PlatformAuthOptions{
		Mode:     cfg.AuthMode,
		Verifier: verifier,
	})

	if cfg.AuthMode == "jwt" && cfg.ServiceClientSecret != "" {
		go registerPermissionsLoop(ctx, cfg)
	} else if cfg.AuthMode == "jwt" {
		log.Printf("erp: SERVICE_CLIENT_SECRET unset — skipping permissions registration")
	}

	api := &handlers.API{Cfg: cfg, Store: st, Audit: auditStore, Bus: bus, Pool: pool, Notify: notify.New(notify.Config{
		Brokers:  cfg.KafkaBrokers,
		ClientID: cfg.KafkaClientID,
		Topic:    cfg.KafkaNotificationsTopic,
	})}
	router := handlers.NewRouter(handlers.RouterDeps{
		API:            api,
		Audit:          auditStore,
		PlatformAuth:   platformAuth,
		CORSOrigins:    cfg.CORSOrigins,
		StrictRBAC:     cfg.StrictRBAC(),
		PayrollEnabled: cfg.PayrollEnabled,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("erp listening on :%s (aud=%s events=%v)", cfg.Port, cfg.Audience, bus.Enabled())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// refreshJWKSWithRetry does a bounded set of boot-time refresh attempts with a
// short linear backoff. On persistent failure it returns the last error so the
// caller can degrade to the background refresh loop instead of exiting.
func refreshJWKSWithRetry(ctx context.Context, v *authclient.Verifier) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = v.Refresh(c)
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
		}
	}
	return err
}

func jwksRefreshLoop(v *authclient.Verifier) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := v.Refresh(ctx); err != nil {
			log.Printf("jwks refresh: %v", err)
		}
		cancel()
	}
}

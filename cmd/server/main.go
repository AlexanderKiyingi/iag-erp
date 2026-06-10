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
		initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := verifier.Refresh(initCtx); err != nil {
			cancel()
			log.Fatalf("jwks refresh: %v", err)
		}
		cancel()
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
		API:          api,
		Audit:        auditStore,
		PlatformAuth: platformAuth,
		CORSOrigins:  cfg.CORSOrigins,
		StrictRBAC:   cfg.StrictRBAC(),
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

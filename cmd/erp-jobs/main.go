package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"iag-erp/backend/internal/config"
	"iag-erp/backend/internal/db"
	"iag-erp/backend/internal/notify"
	"iag-erp/backend/internal/store"
)

func main() {
	leaveReconcile := flag.Bool("leave-reconcile", false, "Reconcile employee on_leave status from approved leave dates")
	birthdayReminders := flag.Bool("birthday-reminders", false, "Send birthday reminder emails (employee, manager, HR)")
	leaveDaemon := flag.Bool("daemon", false, "Run leave reconcile hourly")
	birthdayDaemon := flag.Bool("birthday-daemon", false, "Run birthday reminders daily at 08:00 UTC")
	flag.Parse()

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

	st := store.New(pool)

	if *leaveReconcile || (*leaveDaemon && !*birthdayReminders) {
		runLeaveReconcile(ctx, st)
		if !*leaveDaemon {
			if !*birthdayReminders && !*birthdayDaemon {
				return
			}
		}
	}

	if *birthdayReminders {
		runBirthdayReminders(ctx, st, cfg)
		if !*birthdayDaemon && !*leaveDaemon {
			return
		}
	}

	if *leaveDaemon {
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				runLeaveReconcile(ctx, st)
			}
		}()
	}

	if *birthdayDaemon {
		for {
			sleepUntilNextUTC(8, 0)
			runBirthdayReminders(ctx, st, cfg)
		}
	}

	if !*leaveReconcile && !*birthdayReminders && !*leaveDaemon && !*birthdayDaemon {
		runLeaveReconcile(ctx, st)
	}
}

func runLeaveReconcile(ctx context.Context, st *store.Store) {
	n, err := st.ReconcileAllEmployeeLeaveStatuses(ctx)
	if err != nil {
		log.Printf("leave reconcile failed: %v", err)
		os.Exit(1)
	}
	log.Printf("leave reconcile: updated %d employees", n)
}

func runBirthdayReminders(ctx context.Context, st *store.Store, cfg *config.Config) {
	pub := notify.New(notify.Config{
		Brokers:  cfg.KafkaBrokers,
		ClientID: cfg.KafkaClientID,
		Topic:    cfg.KafkaNotificationsTopic,
	})
	defer pub.Close()
	if !pub.Enabled() {
		log.Fatal("birthday reminders require KAFKA_BROKERS")
	}
	result, err := st.SendBirthdayReminders(ctx, pub, cfg)
	if err != nil {
		log.Printf("birthday reminders failed: %v", err)
		os.Exit(1)
	}
	log.Printf("birthday reminders: matched=%d sent=%d", result.EmployeesMatched, result.Sent)
}

func sleepUntilNextUTC(hour, minute int) {
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	time.Sleep(time.Until(next))
}

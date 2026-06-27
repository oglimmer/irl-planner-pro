// Command server is the IRL attendance app backend HTTP server.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	// Embed the IANA timezone database into the binary. The production image is
	// distroless/static, which ships no system zoneinfo, so without this
	// time.LoadLocation("Europe/Paris") fails and every event-tz computation
	// (deadlines, reminders, "is past") silently falls back to UTC.
	_ "time/tzdata"

	"irlplanner/internal/config"
	"irlplanner/internal/db"
	"irlplanner/internal/email"
	"irlplanner/internal/metrics"
	"irlplanner/internal/server"
	"irlplanner/internal/slack"
)

func main() {
	cfg := config.Load()

	pool, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	metrics.RegisterDBStats(pool)

	app := &server.App{
		Cfg:   cfg,
		DB:    pool,
		Store: server.NewStore(pool),
		Email: email.Sender{
			Host:        cfg.SMTPHost,
			Port:        cfg.SMTPPort,
			Username:    cfg.SMTPUsername,
			Password:    cfg.SMTPPassword,
			From:        cfg.SMTPFrom,
			UseTLS:      cfg.SMTPUseTLS,
			ImplicitTLS: cfg.SMTPImplicitTLS,
		},
		Slack: slack.Notifier{Token: cfg.SlackBotToken},
	}

	// rootCtx is cancelled on SIGINT/SIGTERM. Every background worker derives
	// from it, so a shutdown signal cancels in-flight DB queries instead of
	// letting them outlive the process.
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// bg tracks long-lived background goroutines so shutdown can wait for them
	// to unwind after rootCtx is cancelled. Schedulers are added in later phases.
	var bg sync.WaitGroup

	if cfg.AuthMode == "oidc" {
		if err := app.InitOIDC(rootCtx); err != nil {
			log.Fatalf("oidc init: %v", err)
		}
	}

	// Reminder + daily-digest scheduler.
	app.StartReminders(rootCtx, &bg)

	// Phase 7 — periodic GC of expired OAuth rows. No-op unless MCP is configured.
	app.StartOAuthGC(rootCtx, &bg)

	app.MarkReady()

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.NewRouter(app),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-rootCtx.Done()
	log.Println("shutting down")
	stop() // restore default signal handling so a second signal kills hard

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}

	done := make(chan struct{})
	go func() { bg.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		log.Println("shutdown: background workers did not exit in time")
	}
}

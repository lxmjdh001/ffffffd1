package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"appdock/internal/platform"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := http.Client{Timeout: 3 * time.Second}
		resp, e := client.Get("http://127.0.0.1:8088/healthz")
		if e != nil {
			os.Exit(1)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "check-landing-browser" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		result, err := platform.CheckLandingScreenshot(ctx)
		if err != nil {
			slog.Error("landing screenshot browser check failed", "error", err)
			os.Exit(1)
		}
		slog.Info("landing screenshot browser ready", "browser", result.Browser, "width", result.Width, "height", result.Height, "sandbox", result.Sandbox)
		return
	}
	c, e := platform.LoadConfig()
	if e != nil {
		slog.Error("configuration invalid", "error", e)
		os.Exit(1)
	}
	db, e := platform.OpenDB(c)
	if e != nil {
		slog.Error("database unavailable", "error", e)
		os.Exit(1)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if e = platform.Migrate(db); e != nil {
		slog.Error("migration failed", "error", e)
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "reset-admin" {
		if e = platform.ResetAdmin(db, c); e != nil {
			slog.Error("admin reset failed", "error", e)
			os.Exit(1)
		}
		slog.Info("administrator password changed and sessions revoked")
		return
	}
	if e = platform.BootstrapAdmin(db, c); e != nil {
		slog.Error("admin bootstrap failed", "error", e)
		os.Exit(1)
	}
	app := platform.NewServer(db, c)
	app.Media, e = platform.NewR2MediaStore(c)
	if e != nil {
		slog.Error("R2 media storage unavailable", "error", e)
		os.Exit(1)
	}
	app.Blobs, e = platform.NewBlobStore(c)
	if e != nil {
		slog.Error("storage unavailable", "error", e)
		os.Exit(1)
	}
	app.PageImages, e = platform.NewPageImageStore(c)
	if e != nil {
		slog.Error("page image storage unavailable", "error", e)
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "migrate-r2" {
		if e = app.MigrateFilesToR2(context.Background()); e != nil {
			slog.Error("R2 migration failed; local originals retained", "error", e)
			os.Exit(1)
		}
		slog.Info("R2 migration verified; local originals retained as backup")
		return
	}
	if app.PageImages != nil {
		publishCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		e = app.PublishLandingScripts(publishCtx)
		cancel()
		if e != nil {
			slog.Warn("landing JS publication unavailable; using local scripts", "error", e)
			if len(os.Args) > 1 && os.Args[1] == "publish-landing-js" {
				os.Exit(1)
			}
		} else {
			slog.Info("landing assets published to R2", "scripts", len(app.LandingScripts.Files), "images", len(app.LandingScripts.Images), "url", app.LandingScripts.BaseURL)
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "publish-landing-js" {
		if app.LandingScripts == nil {
			slog.Error("public R2 page-image configuration is required for landing JS")
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: c.Addr, Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 20 * time.Minute, WriteTimeout: 30 * time.Minute, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32768}
	adPrepareCtx, stopAdPrepare := context.WithCancel(context.Background())
	adPrepareDone := make(chan struct{})
	go func() { app.RunDownloadAdWorker(adPrepareCtx); close(adPrepareDone) }()
	go app.RunCompletionWorker(ctx)
	if c.Role != "download" {
		go app.RunWorker(ctx)
		go app.RunAdWorker(ctx)
		go app.RunPWAPushWorker(ctx)
	} else {
		go app.RunFileWorker(ctx)
	}
	go func() {
		slog.Info("D1 Apk ready", "address", c.Addr, "environment", c.Env)
		if e := server.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
			slog.Error("server stopped", "error", e)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	stopAdPrepare()
	select {
	case <-adPrepareDone:
	case <-shutdown.Done():
	}
}

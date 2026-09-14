package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hello-coder/hello-coder/internal/auth"
	"github.com/hello-coder/hello-coder/internal/config"
	"github.com/hello-coder/hello-coder/internal/desktop"
	"github.com/hello-coder/hello-coder/internal/secret"
	"github.com/hello-coder/hello-coder/internal/server"
	"github.com/hello-coder/hello-coder/internal/store"
	"github.com/hello-coder/hello-coder/internal/version"
	"gorm.io/gorm"
)

type App struct {
	cfg    config.Config
	db     *gorm.DB
	server *server.Server
}

func New(cfg config.Config) (*App, error) {
	setupLogger(cfg)
	if strings.TrimSpace(cfg.StaticDir) == "" && os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := store.Open(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}

	secrets, err := secret.Open(cfg.DataDir, cfg.DataKey, cfg.JWTSecret)
	if err != nil {
		return nil, fmt.Errorf("secret: %w", err)
	}

	tokens := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTTTL)

	opts := server.RouterOpts{AuthRequired: cfg.AuthRequired}
	if !cfg.AuthRequired {
		uid, name, err := store.EnsureDesktopUser(db)
		if err != nil {
			return nil, fmt.Errorf("desktop user: %w", err)
		}
		opts.LocalUserID = uid
		opts.LocalUser = name
		slog.Info("auth disabled (desktop); using admin user", "userId", uid, "username", name)
	}

	engine := server.NewRouter(db, tokens, secrets, cfg.StaticDir, opts)
	srv := server.New(cfg.Addr, engine)

	return &App{cfg: cfg, db: db, server: srv}, nil
}

func (a *App) Run() error {
	slog.Info("hello-coder starting", "version", version.Short(), "mode", a.cfg.Mode, "addr", a.cfg.Addr, "authRequired", a.cfg.AuthRequired, "dataDir", a.cfg.DataDir, "staticDir", a.cfg.StaticDir)
	if a.cfg.Mode == config.ModeDesktop && desktop.Supported() {
		return a.runDesktop()
	}
	if a.cfg.Mode == config.ModeDesktop && !desktop.Supported() {
		slog.Info("desktop window is windows-only; running as server")
	}
	return a.server.Run()
}

func (a *App) runDesktop() error {
	addr, err := a.server.Start()
	if err != nil {
		return err
	}
	base := strings.TrimRight(server.LocalURL(addr), "/")
	// Skip login page when desktop auth is off.
	url := base + "/"
	if !a.cfg.AuthRequired {
		url = base + "/app"
	}
	if err := waitReady(base, 8*time.Second); err != nil {
		a.stopServer()
		return err
	}
	slog.Info("opening desktop window", "url", url)
	if err := desktop.Open(url, a.cfg.DataDir); err != nil {
		a.stopServer()
		return err
	}
	a.stopServer()
	return a.server.Wait()
}

func (a *App) stopServer() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.server.Shutdown(ctx); err != nil {
		slog.Warn("shutdown", "err", err)
	}
}

func waitReady(baseURL string, timeout time.Duration) error {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	health := strings.TrimRight(baseURL, "/") + "/api/health"
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(health)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
	return fmt.Errorf("server not ready at %s", health)
}

func setupLogger(cfg config.Config) {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	writers := []io.Writer{os.Stderr}
	if cfg.Mode == config.ModeDesktop {
		if err := os.MkdirAll(cfg.DataDir, 0o755); err == nil {
			f, err := os.OpenFile(filepath.Join(cfg.DataDir, "hello-coder.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err == nil {
				writers = append(writers, f)
			}
		}
	}
	h := slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}

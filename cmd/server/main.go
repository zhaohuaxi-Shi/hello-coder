package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/hello-coder/hello-coder/internal/app"
	"github.com/hello-coder/hello-coder/internal/config"
	"github.com/hello-coder/hello-coder/internal/desktop"
	"github.com/hello-coder/hello-coder/internal/version"
)

func main() {
	if wantsVersion() {
		fmt.Println(version.String())
		return
	}

	chdirToExe()
	cfg := config.Load()

	application, err := app.New(cfg)
	if err != nil {
		slog.Error("failed to bootstrap app", "err", err)
		if cfg.Mode == config.ModeDesktop {
			desktop.NotifyError("Hello Coder", err.Error())
		}
		os.Exit(1)
	}

	if err := application.Run(); err != nil {
		slog.Error("server stopped with error", "err", err)
		if cfg.Mode == config.ModeDesktop {
			desktop.NotifyError("Hello Coder", err.Error())
		}
		os.Exit(1)
	}
}

func chdirToExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return
	}
	lower := strings.ToLower(exe)
	sep := string(filepath.Separator)
	if strings.Contains(lower, sep+"go-build") {
		return
	}
	_ = os.Chdir(filepath.Dir(exe))
}

func wantsVersion() bool {
	for _, a := range os.Args[1:] {
		switch a {
		case "--version", "-version", "version":
			return true
		}
	}
	return false
}

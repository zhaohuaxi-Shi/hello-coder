package config

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	ModeDesktop = "desktop"
	ModeServer  = "server"
)

// Config holds process-level settings (not tool connection configs).
type Config struct {
	Mode         string // desktop | server
	Addr         string // e.g. "127.0.0.1:10240" or ":10240"
	DataDir      string
	LogLevel     string
	JWTSecret    string
	JWTTTL       time.Duration
	DataKey      string // AES key material for connection secrets at rest
	StaticDir    string // if set, serve web UI from this dir (dev); empty = embed
	AuthRequired bool   // server (Linux/LAN) requires login; desktop EXE skips it
}

func Load() Config {
	mode := resolveMode()
	ttlHours := GetEnvInt("HELLO_CODER_JWT_TTL_HOURS", 4)
	return Config{
		Mode:         mode,
		Addr:         getEnv("HELLO_CODER_ADDR", defaultAddr(mode)),
		DataDir:      getEnv("HELLO_CODER_DATA_DIR", "./data"),
		LogLevel:     getEnv("HELLO_CODER_LOG_LEVEL", "info"),
		JWTSecret:    getEnv("HELLO_CODER_JWT_SECRET", "hello-coder-dev-secret-change-me"),
		JWTTTL:       time.Duration(ttlHours) * time.Hour,
		DataKey:      getEnv("HELLO_CODER_DATA_KEY", ""),
		StaticDir:    getEnv("HELLO_CODER_STATIC_DIR", ""),
		AuthRequired: resolveAuthRequired(mode),
	}
}

// resolveAuthRequired: desktop (Windows EXE) skips login; server (Linux/LAN) requires it.
// Override with HELLO_CODER_AUTH=on|off (or 1/0/true/false).
func resolveAuthRequired(mode string) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("HELLO_CODER_AUTH")))
	switch raw {
	case "on", "1", "true", "yes":
		return true
	case "off", "0", "false", "no":
		return false
	}
	return mode == ModeServer
}

func resolveMode() string {
	mode := strings.ToLower(strings.TrimSpace(flagValue("mode")))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(os.Getenv("HELLO_CODER_MODE")))
	}
	switch mode {
	case ModeDesktop, ModeServer:
		return mode
	}
	if runtime.GOOS == "windows" {
		return ModeDesktop
	}
	return ModeServer
}

func defaultAddr(mode string) string {
	if mode == ModeDesktop {
		return "127.0.0.1:10240"
	}
	return ":10240"
}

func flagValue(name string) string {
	prefix := "--" + name
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == prefix && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, prefix+"=") {
			return strings.TrimPrefix(a, prefix+"=")
		}
	}
	return ""
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func GetEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

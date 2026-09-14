package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hello-coder/hello-coder/internal/model"
	goredis "github.com/redis/go-redis/v9"
)

const (
	ModeStandalone = "standalone"
	ModeCluster    = "cluster"
	ModeSentinel   = "sentinel"
)

type PingResult struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Role      string `json:"role,omitempty"`
	Message   string `json:"message,omitempty"`
}

func splitAddrs(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = strings.TrimPrefix(p, "redis://")
		p = strings.TrimPrefix(p, "rediss://")
		out = append(out, p)
	}
	return out
}

func newClient(conn *model.RedisConn) (goredis.UniversalClient, error) {
	return newClientDB(conn, conn.DB)
}

func newClientDB(conn *model.RedisConn, db int) (goredis.UniversalClient, error) {
	addrs := splitAddrs(conn.Addresses)
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses configured")
	}
	mode := strings.ToLower(strings.TrimSpace(conn.Mode))
	if mode == "" {
		mode = ModeStandalone
	}
	if db < 0 {
		db = 0
	}
	if mode == ModeCluster {
		db = 0
	}
	switch mode {
	case ModeStandalone:
		dial, read, write, pool := dialOptions()
		return goredis.NewClient(&goredis.Options{
			Addr:         addrs[0],
			Username:     conn.Username,
			Password:     conn.Password,
			DB:           db,
			DialTimeout:  dial,
			ReadTimeout:  read,
			WriteTimeout: write,
			PoolTimeout:  pool,
			PoolSize:     8,
			MinIdleConns: 1,
		}), nil
	case ModeCluster:
		dial, read, write, pool := dialOptions()
		return goredis.NewClusterClient(&goredis.ClusterOptions{
			Addrs:        addrs,
			Username:     conn.Username,
			Password:     conn.Password,
			DialTimeout:  dial,
			ReadTimeout:  read,
			WriteTimeout: write,
			PoolTimeout:  pool,
			PoolSize:     8,
			MinIdleConns: 1,
		}), nil
	case ModeSentinel:
		master := strings.TrimSpace(conn.MasterName)
		if master == "" {
			return nil, fmt.Errorf("master name is required for sentinel mode")
		}
		dial, read, write, pool := dialOptions()
		return goredis.NewFailoverClient(&goredis.FailoverOptions{
			MasterName:    master,
			SentinelAddrs: addrs,
			Username:      conn.Username,
			Password:      conn.Password,
			DB:            db,
			DialTimeout:   dial,
			ReadTimeout:   read,
			WriteTimeout:  write,
			PoolTimeout:   pool,
			PoolSize:      8,
			MinIdleConns:  1,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported mode: %s", conn.Mode)
	}
}

func closeClient(c goredis.UniversalClient) {
	if c != nil {
		_ = c.Close()
	}
}

func parseInfoField(info, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

// Ping checks availability with Redis PING. Optionally enriches with INFO server fields.
func Ping(conn *model.RedisConn) PingResult {
	client, err := newClient(conn)
	if err != nil {
		return PingResult{Available: false, Message: err.Error()}
	}
	defer closeClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return PingResult{Available: false, Message: err.Error()}
	}

	out := PingResult{
		Available: true,
		Mode:      conn.Mode,
		Message:   "ok",
	}
	if srv, e := client.Info(ctx, "server").Result(); e == nil {
		out.Version = parseInfoField(srv, "redis_version")
		if m := parseInfoField(srv, "redis_mode"); m != "" {
			out.Mode = m
		}
	}
	if repl, e := client.Info(ctx, "replication").Result(); e == nil {
		out.Role = parseInfoField(repl, "role")
	}
	return out
}

// FetchVersion reads redis_version via INFO server (used once on save).
func FetchVersion(conn *model.RedisConn) string {
	client, err := newClient(conn)
	if err != nil {
		return ""
	}
	defer closeClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return ""
	}
	info, err := client.Info(ctx, "server").Result()
	if err != nil {
		return ""
	}
	return parseInfoField(info, "redis_version")
}

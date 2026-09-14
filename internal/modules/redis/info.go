package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hello-coder/hello-coder/internal/model"
	goredis "github.com/redis/go-redis/v9"
)

// DBDetail is one logical database from INFO keyspace.
type DBDetail struct {
	DB      int   `json:"db"`
	Keys    int64 `json:"keys"`
	Expires int64 `json:"expires"` // keys with TTL
}

// ConnDetail is Redis runtime / persistence / keyspace snapshot for the detail drawer.
type ConnDetail struct {
	Available bool   `json:"available"`
	Message   string `json:"message,omitempty"`
	Mode      string `json:"mode,omitempty"`

	// Runtime & memory
	Version         string `json:"version"`
	UptimeDays      int64  `json:"uptimeDays"`
	UsedMemory      string `json:"usedMemory"`
	UsedMemoryBytes int64  `json:"usedMemoryBytes"`
	MaxMemory       string `json:"maxMemory"`
	MaxMemoryBytes  int64  `json:"maxMemoryBytes"`
	EvictedKeys     int64  `json:"evictedKeys"`

	// Persistence
	RDBEnabled             bool   `json:"rdbEnabled"`
	RDBLastBgsaveStatus    string `json:"rdbLastBgsaveStatus"`
	RDBLastBgsaveTimeSec   int64  `json:"rdbLastBgsaveTimeSec"`
	AOFEnabled             bool   `json:"aofEnabled"`
	AOFLastRewriteTimeSec  int64  `json:"aofLastRewriteTimeSec"`
	AOFLastBgrewriteStatus string `json:"aofLastBgrewriteStatus"`

	Databases []DBDetail `json:"databases"`
}

// FetchConnDetail gathers INFO server/memory/stats/persistence + keyspace (+ CONFIG save).
func FetchConnDetail(conn *model.RedisConn) ConnDetail {
	client, err := getSharedClient(conn, 0)
	if err != nil {
		return ConnDetail{Available: false, Message: err.Error(), Mode: conn.Mode}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return ConnDetail{Available: false, Message: err.Error(), Mode: conn.Mode}
	}

	out := ConnDetail{
		Available: true,
		Mode:      conn.Mode,
		Message:   "ok",
		MaxMemory: "Unlimited",
	}

	if srv, e := client.Info(ctx, "server").Result(); e == nil {
		out.Version = parseInfoField(srv, "redis_version")
		out.UptimeDays = parseInfoInt64(srv, "uptime_in_days")
		if m := parseInfoField(srv, "redis_mode"); m != "" {
			out.Mode = m
		}
	}
	if mem, e := client.Info(ctx, "memory").Result(); e == nil {
		out.UsedMemory = parseInfoField(mem, "used_memory_human")
		out.UsedMemoryBytes = parseInfoInt64(mem, "used_memory")
		out.MaxMemoryBytes = parseInfoInt64(mem, "maxmemory")
		if human := parseInfoField(mem, "maxmemory_human"); out.MaxMemoryBytes > 0 && human != "" && human != "0B" {
			out.MaxMemory = human
		} else if out.MaxMemoryBytes > 0 {
			out.MaxMemory = formatBytes(out.MaxMemoryBytes)
		} else {
			out.MaxMemory = "Unlimited"
		}
		if out.UsedMemory == "" && out.UsedMemoryBytes > 0 {
			out.UsedMemory = formatBytes(out.UsedMemoryBytes)
		}
		if out.UsedMemory == "" {
			out.UsedMemory = "0B"
		}
	}
	if stats, e := client.Info(ctx, "stats").Result(); e == nil {
		out.EvictedKeys = parseInfoInt64(stats, "evicted_keys")
	}
	if pers, e := client.Info(ctx, "persistence").Result(); e == nil {
		out.RDBLastBgsaveStatus = parseInfoField(pers, "rdb_last_bgsave_status")
		out.RDBLastBgsaveTimeSec = parseInfoInt64(pers, "rdb_last_bgsave_time_sec")
		out.AOFEnabled = parseInfoField(pers, "aof_enabled") == "1"
		out.AOFLastRewriteTimeSec = parseInfoInt64(pers, "aof_last_rewrite_time_sec")
		out.AOFLastBgrewriteStatus = parseInfoField(pers, "aof_last_bgrewrite_status")
		// Redis still reports default "ok" / -1 when AOF was never used; hide when disabled.
		if !out.AOFEnabled {
			out.AOFLastBgrewriteStatus = ""
			out.AOFLastRewriteTimeSec = -1
		}
	}

	// RDB auto-save enabled when CONFIG save is non-empty.
	out.RDBEnabled = true
	if cfg, e := client.ConfigGet(ctx, "save").Result(); e == nil {
		save := strings.TrimSpace(cfg["save"])
		out.RDBEnabled = save != ""
	}
	if !out.RDBEnabled {
		out.RDBLastBgsaveStatus = ""
		out.RDBLastBgsaveTimeSec = -1
	}

	out.Databases = listDBDetails(ctx, client, conn.Mode)
	return out
}

func listDBDetails(ctx context.Context, client goredis.UniversalClient, mode string) []DBDetail {
	info, err := client.Info(ctx, "keyspace").Result()
	if err != nil {
		return []DBDetail{}
	}

	found := map[int]DBDetail{}
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "db") {
			continue
		}
		// db0:keys=1,expires=0,avg_ttl=0
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		dbNum, err := strconv.Atoi(strings.TrimPrefix(parts[0], "db"))
		if err != nil {
			continue
		}
		d := DBDetail{DB: dbNum}
		for _, kv := range strings.Split(parts[1], ",") {
			kv = strings.TrimSpace(kv)
			if strings.HasPrefix(kv, "keys=") {
				if n, e := strconv.ParseInt(strings.TrimPrefix(kv, "keys="), 10, 64); e == nil {
					d.Keys = n
				}
			} else if strings.HasPrefix(kv, "expires=") {
				if n, e := strconv.ParseInt(strings.TrimPrefix(kv, "expires="), 10, 64); e == nil {
					d.Expires = n
				}
			}
		}
		found[dbNum] = d
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == ModeCluster {
		if d, ok := found[0]; ok && d.Keys > 0 {
			return []DBDetail{d}
		}
		return []DBDetail{}
	}

	out := make([]DBDetail, 0, len(found))
	for i := 0; i < 16; i++ {
		if d, ok := found[i]; ok && d.Keys > 0 {
			out = append(out, d)
		}
	}
	return out
}

func parseInfoInt64(info, key string) int64 {
	v := parseInfoField(info, key)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func formatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

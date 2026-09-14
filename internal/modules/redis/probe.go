package redis

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hello-coder/hello-coder/internal/model"
	goredis "github.com/redis/go-redis/v9"
)

const probeTimeout = 500 * time.Millisecond

type connStatusDTO struct {
	Total    int `json:"total"`
	Abnormal int `json:"abnormal"`
}

func newProbeClient(conn *model.RedisConn) (goredis.UniversalClient, error) {
	addrs := splitAddrs(conn.Addresses)
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses configured")
	}
	mode := strings.ToLower(strings.TrimSpace(conn.Mode))
	if mode == "" {
		mode = ModeStandalone
	}

	opts := &goredis.Options{
		Addr:         addrs[0],
		Username:     conn.Username,
		Password:     conn.Password,
		DB:           0,
		DialTimeout:  probeTimeout,
		ReadTimeout:  probeTimeout,
		WriteTimeout: probeTimeout,
		PoolTimeout:  probeTimeout,
		PoolSize:     1,
		MinIdleConns: 0,
	}

	switch mode {
	case ModeStandalone, ModeCluster:
		// Fastest: PING the first seed as a single node (avoids CLUSTER SLOTS).
		return goredis.NewClient(opts), nil
	case ModeSentinel:
		master := strings.TrimSpace(conn.MasterName)
		if master == "" {
			return nil, fmt.Errorf("master name is required for sentinel mode")
		}
		return goredis.NewFailoverClient(&goredis.FailoverOptions{
			MasterName:    master,
			SentinelAddrs: addrs,
			Username:      conn.Username,
			Password:      conn.Password,
			DB:            0,
			DialTimeout:   probeTimeout,
			ReadTimeout:   probeTimeout,
			WriteTimeout:  probeTimeout,
			PoolTimeout:   probeTimeout,
			PoolSize:      1,
			MinIdleConns:  0,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported mode: %s", conn.Mode)
	}
}

// Probe uses Redis PING with a 0.5s budget — the cheapest protocol check.
func Probe(conn *model.RedisConn) bool {
	client, err := newProbeClient(conn)
	if err != nil {
		return false
	}
	defer closeClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return client.Ping(ctx).Err() == nil
}

func summarizeConns(rows []model.RedisConn, open func(*model.RedisConn) (*model.RedisConn, error)) connStatusDTO {
	var abnormal atomic.Int32
	var wg sync.WaitGroup
	for i := range rows {
		wg.Add(1)
		go func(row *model.RedisConn) {
			defer wg.Done()
			alive := false
			defer func() {
				_ = recover()
				if !alive {
					abnormal.Add(1)
				}
			}()
			use, err := open(row)
			if err != nil {
				return
			}
			alive = Probe(use)
		}(&rows[i])
	}
	wg.Wait()
	return connStatusDTO{Total: len(rows), Abnormal: int(abnormal.Load())}
}

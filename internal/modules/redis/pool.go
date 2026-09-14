package redis

import (
	"fmt"
	"sync"
	"time"

	"github.com/hello-coder/hello-coder/internal/model"
	goredis "github.com/redis/go-redis/v9"
)

// Shared clients across requests (ARDM keeps one connection; reconnecting every SCAN is slow).
var sharedClients sync.Map // key -> goredis.UniversalClient

func clientCacheKey(conn *model.RedisConn, db int) string {
	return fmt.Sprintf("%d|%d|%s|%s|%s|%s|%s",
		conn.ID, db, conn.Mode, conn.Addresses, conn.MasterName, conn.Username, conn.Password)
}

func getSharedClient(conn *model.RedisConn, db int) (goredis.UniversalClient, error) {
	key := clientCacheKey(conn, db)
	if v, ok := sharedClients.Load(key); ok {
		return v.(goredis.UniversalClient), nil
	}
	client, err := newClientDB(conn, db)
	if err != nil {
		return nil, err
	}
	actual, loaded := sharedClients.LoadOrStore(key, client)
	if loaded {
		_ = client.Close()
		return actual.(goredis.UniversalClient), nil
	}
	return client, nil
}

// InvalidateConnClients drops cached clients for a connection (after update/delete).
func InvalidateConnClients(connID uint) {
	prefix := fmt.Sprintf("%d|", connID)
	sharedClients.Range(func(k, v any) bool {
		key, _ := k.(string)
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			if c, ok := v.(goredis.UniversalClient); ok {
				_ = c.Close()
			}
			sharedClients.Delete(k)
		}
		return true
	})
}

func dialOptions() (dialTimeout, readTimeout, writeTimeout, poolTimeout time.Duration) {
	return 3 * time.Second, 8 * time.Second, 8 * time.Second, 4 * time.Second
}

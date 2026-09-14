package redis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hello-coder/hello-coder/internal/model"
	goredis "github.com/redis/go-redis/v9"
)

type ScanResult struct {
	Keys               []string `json:"keys"`
	Cursor             uint64   `json:"cursor"` // numeric cursor for standalone; 0 when done
	CursorToken        string   `json:"cursorToken,omitempty"`
	Done               bool     `json:"done"`
	DB                 int      `json:"db"`
	Match              string   `json:"match"`
	Mode               string   `json:"mode,omitempty"` // scan | cluster-scan | keys | exact | random | unsupported
	ListingUnsupported bool     `json:"listingUnsupported,omitempty"`
	Message            string   `json:"message,omitempty"`
}

type KeyEntry struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	TTL   int64  `json:"ttl"` // seconds; -1 no expire; -2 missing
	Value any    `json:"value"`
}

type DBInfo struct {
	DB   int   `json:"db"`
	Keys int64 `json:"keys"`
}

func ListDatabases(conn *model.RedisConn) ([]DBInfo, error) {
	mode := strings.ToLower(strings.TrimSpace(conn.Mode))
	if mode == ModeCluster {
		return []DBInfo{{DB: 0}}, nil
	}
	client, err := getSharedClient(conn, 0)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	info, err := client.Info(ctx, "keyspace").Result()
	if err != nil {
		return nil, err
	}
	found := map[int]int64{}
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
		keys := int64(0)
		for _, kv := range strings.Split(parts[1], ",") {
			kv = strings.TrimSpace(kv)
			if strings.HasPrefix(kv, "keys=") {
				if n, e := strconv.ParseInt(strings.TrimPrefix(kv, "keys="), 10, 64); e == nil {
					keys = n
				}
			}
		}
		found[dbNum] = keys
	}
	// Always expose 0-15 so empty DBs are selectable.
	out := make([]DBInfo, 0, 16)
	for i := 0; i < 16; i++ {
		out = append(out, DBInfo{DB: i, Keys: found[i]})
	}
	return out, nil
}

func ScanKeys(conn *model.RedisConn, db int, cursorToken string, count int64, rawQuery string) (ScanResult, error) {
	if count <= 0 {
		count = 20
	}
	if count > 5000 {
		count = 5000
	}
	rawQuery = strings.TrimSpace(rawQuery)
	match := rawQuery
	fuzzyWrapped := false
	if match == "" {
		match = "*"
	} else if !strings.Contains(match, "*") && !strings.Contains(match, "?") && !strings.Contains(match, "[") {
		match = "*" + match + "*"
		fuzzyWrapped = true
	}

	mode := strings.ToLower(strings.TrimSpace(conn.Mode))
	if mode == "" {
		mode = ModeStandalone
	}

	if mode == ModeCluster {
		return scanClusterLikeARDM(conn, db, cursorToken, count, match)
	}

	client, err := getSharedClient(conn, db)
	if err != nil {
		return ScanResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	var cursor uint64
	if cursorToken != "" && cursorToken != "0" {
		cursor, _ = strconv.ParseUint(cursorToken, 10, 64)
	}

	keys, next, err := scanFill(ctx, client, cursor, match, count)
	if err == nil {
		tok := "0"
		if next != 0 {
			tok = strconv.FormatUint(next, 10)
		}
		return ScanResult{
			Keys:        keys,
			Cursor:      next,
			CursorToken: tok,
			Done:        next == 0,
			DB:          db,
			Match:       match,
			Mode:        "scan",
		}, nil
	}
	scanErr := err
	if !isUnknownCommand(scanErr, "scan") && !strings.Contains(strings.ToLower(scanErr.Error()), "not allowed") {
		return ScanResult{}, scanErr
	}

	// Only after SCAN fails: detect sentinel / try cluster / KEYS.
	if mode == ModeStandalone {
		if rm := detectRedisMode(ctx, client); rm == "sentinel" {
			return ScanResult{
				Keys:               []string{},
				Done:               true,
				DB:                 db,
				Match:              match,
				ListingUnsupported: true,
				Mode:               "unsupported",
				Message:            "当前地址连到的是 Sentinel，不是数据节点。请像 ARDM 一样把模式改成 Sentinel，并填写 Master name。",
			}, nil
		}
		if cursorToken == "" || cursorToken == "0" {
			if r, e := scanClusterLikeARDM(conn, db, "0", count, match); e == nil && len(r.Keys) > 0 {
				r.Message = "已按 Cluster 方式扫描。建议连接模式改为 Cluster。"
				return r, nil
			}
		}
	}

	if cursorToken == "" || cursorToken == "0" {
		if keys, err := client.Keys(ctx, match).Result(); err == nil {
			if keys == nil {
				keys = []string{}
			}
			return ScanResult{
				Keys:        keys,
				Cursor:      0,
				CursorToken: "0",
				Done:        true,
				DB:          db,
				Match:       match,
				Mode:        "keys",
			}, nil
		} else if !isUnknownCommand(err, "keys") && !strings.Contains(strings.ToLower(err.Error()), "not allowed") {
			return ScanResult{}, fmt.Errorf("SCAN unsupported and KEYS failed: %w", err)
		}
	} else {
		return ScanResult{Keys: []string{}, Cursor: 0, CursorToken: "0", Done: true, DB: db, Match: match, Mode: "keys"}, nil
	}

	return scanWithoutListing(ctx, client, db, rawQuery, match, fuzzyWrapped, count)
}

type redisScanner interface {
	Scan(ctx context.Context, cursor uint64, match string, count int64) *goredis.ScanCmd
}

// scanFill keeps calling SCAN until it collects `want` keys or the cursor wraps to 0.
// Redis COUNT is only a hint, so a single SCAN may return fewer (or zero) keys.
func scanFill(ctx context.Context, client redisScanner, cursor uint64, match string, want int64) ([]string, uint64, error) {
	if want <= 0 {
		want = 20
	}
	keys := make([]string, 0, want)
	next := cursor
	for round := 0; round < 256; round++ {
		batch, n, err := client.Scan(ctx, next, match, want).Result()
		if err != nil {
			return nil, 0, err
		}
		keys = append(keys, batch...)
		next = n
		if next == 0 || int64(len(keys)) >= want {
			break
		}
	}
	if keys == nil {
		keys = []string{}
	}
	return keys, next, nil
}

type clusterCursorState struct {
	Cursors []uint64 `json:"c"`
	Done    []bool   `json:"d"`
	Addrs   []string `json:"a"`
}

func encodeClusterCursor(state clusterCursorState) string {
	b, err := json.Marshal(state)
	if err != nil {
		return "0"
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeClusterCursor(token string) clusterCursorState {
	if token == "" || token == "0" {
		return clusterCursorState{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return clusterCursorState{}
	}
	var parsed clusterCursorState
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return clusterCursorState{}
	}
	return parsed
}

// scanClusterLikeARDM walks masters in stable address order and fills one page of keys.
func scanClusterLikeARDM(conn *model.RedisConn, db int, cursorToken string, count int64, match string) (ScanResult, error) {
	cp := *conn
	cp.Mode = ModeCluster
	client, err := getSharedClient(&cp, 0)
	if err != nil {
		return ScanResult{}, err
	}
	cluster, ok := client.(*goredis.ClusterClient)
	if !ok {
		return ScanResult{}, fmt.Errorf("failed to open cluster client")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	prev := decodeClusterCursor(cursorToken)
	prevByAddr := map[string]struct {
		cursor uint64
		done   bool
	}{}
	for i, addr := range prev.Addrs {
		st := struct {
			cursor uint64
			done   bool
		}{}
		if i < len(prev.Cursors) {
			st.cursor = prev.Cursors[i]
		}
		if i < len(prev.Done) {
			st.done = prev.Done[i]
		}
		prevByAddr[addr] = st
	}

	type nodeRef struct {
		addr string
		node *goredis.Client
	}
	var (
		mu    sync.Mutex
		nodes []nodeRef
	)
	err = cluster.ForEachMaster(ctx, func(_ context.Context, node *goredis.Client) error {
		mu.Lock()
		nodes = append(nodes, nodeRef{addr: node.Options().Addr, node: node})
		mu.Unlock()
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}
	if len(nodes) == 0 {
		return ScanResult{Keys: []string{}, CursorToken: "0", Done: true, DB: db, Match: match, Mode: "cluster-scan"}, nil
	}

	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			if nodes[j].addr < nodes[i].addr {
				nodes[i], nodes[j] = nodes[j], nodes[i]
			}
		}
	}

	var keys []string
	state := clusterCursorState{
		Cursors: make([]uint64, len(nodes)),
		Done:    make([]bool, len(nodes)),
		Addrs:   make([]string, len(nodes)),
	}
	allDone := true
	for i, n := range nodes {
		st := prevByAddr[n.addr]
		state.Addrs[i] = n.addr
		if st.done {
			state.Done[i] = true
			continue
		}
		remain := count - int64(len(keys))
		if remain <= 0 {
			state.Cursors[i] = st.cursor
			allDone = false
			continue
		}
		ks, next, e := scanFill(ctx, n.node, st.cursor, match, remain)
		if e != nil {
			if isUnknownCommand(e, "scan") || strings.Contains(strings.ToLower(e.Error()), "not allowed") {
				return ScanResult{
					Keys:               []string{},
					Done:               true,
					DB:                 db,
					Match:              match,
					ListingUnsupported: true,
					Mode:               "unsupported",
					Message:            "Cluster 节点拒绝 SCAN。请确认与 ARDM 使用相同模式（Cluster）和地址。",
				}, nil
			}
			return ScanResult{}, e
		}
		keys = append(keys, ks...)
		state.Cursors[i] = next
		state.Done[i] = next == 0
		if next != 0 {
			allDone = false
		}
	}
	tok := "0"
	if !allDone {
		tok = encodeClusterCursor(state)
	}
	if keys == nil {
		keys = []string{}
	}
	return ScanResult{
		Keys:        keys,
		CursorToken: tok,
		Done:        allDone,
		DB:          db,
		Match:       match,
		Mode:        "cluster-scan",
	}, nil
}

func detectRedisMode(ctx context.Context, client goredis.UniversalClient) string {
	info, err := client.Info(ctx, "server").Result()
	if err != nil {
		return ""
	}
	return strings.ToLower(parseInfoField(info, "redis_mode"))
}

func isUnknownCommand(err error, cmd string) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	cmd = strings.ToLower(cmd)
	if strings.Contains(msg, "not allowed") && strings.Contains(msg, cmd) {
		return true
	}
	if !strings.Contains(msg, "unknown command") {
		return false
	}
	return strings.Contains(msg, "'"+cmd+"'") ||
		strings.Contains(msg, `"`+cmd+`"`) ||
		strings.Contains(msg, " "+cmd+",") ||
		strings.Contains(msg, " "+cmd+" ") ||
		strings.HasSuffix(msg, " "+cmd) ||
		strings.HasSuffix(msg, cmd)
}

// scanWithoutListing handles Redis instances that disable SCAN/KEYS.
// Tries exact key lookup, then RANDOMKEY sampling.
func scanWithoutListing(ctx context.Context, client goredis.UniversalClient, db int, rawQuery, match string, fuzzyWrapped bool, count int64) (ScanResult, error) {
	base := ScanResult{
		Keys:               []string{},
		Cursor:             0,
		Done:               true,
		DB:                 db,
		Match:              match,
		ListingUnsupported: true,
		Message:            "This Redis disables SCAN/KEYS. Use an exact key name, or sample with Refresh.",
	}

	// Exact key: user typed a concrete name (we may have wrapped it for fuzzy scan).
	exact := rawQuery
	if exact != "" && (!fuzzyWrapped || (!strings.Contains(rawQuery, "*") && !strings.Contains(rawQuery, "?") && !strings.Contains(rawQuery, "["))) {
		n, err := client.Exists(ctx, exact).Result()
		if err == nil && n > 0 {
			base.Keys = []string{exact}
			base.Mode = "exact"
			base.ListingUnsupported = true
			base.Message = "SCAN/KEYS disabled; opened exact key match."
			return base, nil
		}
		if err != nil && !isUnknownCommand(err, "exists") {
			return ScanResult{}, err
		}
	}

	// Sample via RANDOMKEY when browsing all / no exact hit.
	sampled, err := sampleRandomKeys(ctx, client, int(count))
	if err != nil {
		if isUnknownCommand(err, "randomkey") {
			base.Mode = "unsupported"
			base.Message = "This Redis disables SCAN, KEYS and RANDOMKEY. Enter an exact key name and click Open."
			return base, nil
		}
		return ScanResult{}, err
	}
	if rawQuery != "" && fuzzyWrapped {
		q := strings.ToLower(rawQuery)
		filtered := make([]string, 0, len(sampled))
		for _, k := range sampled {
			if strings.Contains(strings.ToLower(k), q) {
				filtered = append(filtered, k)
			}
		}
		sampled = filtered
	}
	base.Keys = sampled
	base.Mode = "random"
	base.Message = "SCAN/KEYS disabled; showing RANDOMKEY samples (incomplete). Prefer opening an exact key."
	return base, nil
}

func sampleRandomKeys(ctx context.Context, client goredis.UniversalClient, want int) ([]string, error) {
	if want <= 0 {
		want = 50
	}
	if want > 200 {
		want = 200
	}
	seen := make(map[string]struct{}, want)
	out := make([]string, 0, want)
	for i := 0; i < want*8 && len(out) < want; i++ {
		k, err := client.RandomKey(ctx).Result()
		if err == goredis.Nil {
			break
		}
		if err != nil {
			return nil, err
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out, nil
}

func GetEntry(conn *model.RedisConn, db int, key string) (KeyEntry, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return KeyEntry{}, fmt.Errorf("key is required")
	}
	client, err := getSharedClient(conn, db)
	if err != nil {
		return KeyEntry{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	typ, err := client.Type(ctx, key).Result()
	if err != nil {
		return KeyEntry{}, err
	}
	ttl, _ := client.TTL(ctx, key).Result()
	// go-redis: -1/-2 直接存为 Duration(-1)/(-2) 纳秒，不能再 /time.Second，否则会变成 0
	var ttlSec int64
	if ttl < 0 {
		ttlSec = int64(ttl)
	} else {
		ttlSec = int64(ttl / time.Second)
	}
	if typ == "none" {
		return KeyEntry{Key: key, Type: typ, TTL: -2, Value: nil}, nil
	}

	var value any
	switch typ {
	case "string":
		value, err = client.Get(ctx, key).Result()
	case "hash":
		value, err = client.HGetAll(ctx, key).Result()
	case "list":
		value, err = client.LRange(ctx, key, 0, -1).Result()
	case "set":
		value, err = client.SMembers(ctx, key).Result()
	case "zset":
		zs, e := client.ZRangeWithScores(ctx, key, 0, -1).Result()
		err = e
		if e == nil {
			items := make([]map[string]any, 0, len(zs))
			for _, z := range zs {
				items = append(items, map[string]any{"member": z.Member, "score": z.Score})
			}
			value = items
		}
	case "stream":
		n, e := client.XLen(ctx, key).Result()
		err = e
		if e == nil {
			value = map[string]any{"length": n, "note": "stream preview not expanded"}
		}
	default:
		value = nil
	}
	if err != nil {
		return KeyEntry{}, err
	}
	return KeyEntry{Key: key, Type: typ, TTL: ttlSec, Value: value}, nil
}

func SetEntry(conn *model.RedisConn, db int, key, typ, value string, ttlSec int64) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("key is required")
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ == "" {
		typ = "string"
	}
	client, err := getSharedClient(conn, db)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch typ {
	case "string":
		if ttlSec > 0 {
			return client.Set(ctx, key, value, time.Duration(ttlSec)*time.Second).Err()
		}
		return client.Set(ctx, key, value, 0).Err()
	case "hash":
		var m map[string]string
		if err := json.Unmarshal([]byte(value), &m); err != nil {
			var anyMap map[string]any
			if e2 := json.Unmarshal([]byte(value), &anyMap); e2 != nil {
				return fmt.Errorf("hash value must be JSON object: %w", err)
			}
			m = make(map[string]string, len(anyMap))
			for k, v := range anyMap {
				m[k] = anyToString(v)
			}
		}
		if len(m) == 0 {
			return fmt.Errorf("hash value is empty")
		}
		if err := client.Del(ctx, key).Err(); err != nil {
			return err
		}
		args := make([]any, 0, len(m)*2)
		for k, v := range m {
			args = append(args, k, v)
		}
		if err := client.HSet(ctx, key, args...).Err(); err != nil {
			return err
		}
	case "list":
		arr, err := decodeStringArray(value, "list")
		if err != nil {
			return err
		}
		if err := client.Del(ctx, key).Err(); err != nil {
			return err
		}
		vals := make([]any, len(arr))
		for i, v := range arr {
			vals[i] = v
		}
		if err := client.RPush(ctx, key, vals...).Err(); err != nil {
			return err
		}
	case "set":
		arr, err := decodeStringArray(value, "set")
		if err != nil {
			return err
		}
		if err := client.Del(ctx, key).Err(); err != nil {
			return err
		}
		vals := make([]any, len(arr))
		for i, v := range arr {
			vals[i] = v
		}
		if err := client.SAdd(ctx, key, vals...).Err(); err != nil {
			return err
		}
	case "zset":
		var items []map[string]any
		if err := json.Unmarshal([]byte(value), &items); err != nil {
			return fmt.Errorf("zset value must be JSON array of {member, score}: %w", err)
		}
		if len(items) == 0 {
			return fmt.Errorf("zset value is empty")
		}
		zs := make([]goredis.Z, 0, len(items))
		for _, it := range items {
			zs = append(zs, goredis.Z{Member: fmt.Sprint(it["member"]), Score: toFloat(it["score"])})
		}
		if err := client.Del(ctx, key).Err(); err != nil {
			return err
		}
		if err := client.ZAdd(ctx, key, zs...).Err(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported type for create: %s (use string/hash/list/set/zset)", typ)
	}
	if typ != "string" && ttlSec > 0 {
		return client.Expire(ctx, key, time.Duration(ttlSec)*time.Second).Err()
	}
	return nil
}

func decodeStringArray(value, kind string) ([]string, error) {
	var arr []string
	if err := json.Unmarshal([]byte(value), &arr); err == nil {
		if len(arr) == 0 {
			return nil, fmt.Errorf("%s value is empty", kind)
		}
		return arr, nil
	}
	var anyArr []any
	if err := json.Unmarshal([]byte(value), &anyArr); err != nil {
		return nil, fmt.Errorf("%s value must be JSON array: %w", kind, err)
	}
	if len(anyArr) == 0 {
		return nil, fmt.Errorf("%s value is empty", kind)
	}
	out := make([]string, len(anyArr))
	for i, v := range anyArr {
		out[i] = fmt.Sprint(v)
	}
	return out, nil
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		f, _ := strconv.ParseFloat(fmt.Sprint(v), 64)
		return f
	}
}

func anyToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

// SetKeyTTL updates key expiration. ttlSec <= 0 removes expiration (PERSIST).
func SetKeyTTL(conn *model.RedisConn, db int, key string, ttlSec int64) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("key is required")
	}
	client, err := getSharedClient(conn, db)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	typ, err := client.Type(ctx, key).Result()
	if err != nil {
		return err
	}
	if typ == "none" {
		return fmt.Errorf("key not found")
	}
	if ttlSec <= 0 {
		return client.Persist(ctx, key).Err()
	}
	ok, err := client.Expire(ctx, key, time.Duration(ttlSec)*time.Second).Result()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("failed to set expire")
	}
	return nil
}

func DeleteEntries(conn *model.RedisConn, db int, keys []string) (int64, error) {
	cleaned := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k != "" {
			cleaned = append(cleaned, k)
		}
	}
	if len(cleaned) == 0 {
		return 0, fmt.Errorf("keys is required")
	}
	client, err := getSharedClient(conn, db)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Cluster-safe: delete one by one if cluster (CROSSSLOT); otherwise pipeline Del.
	if strings.ToLower(strings.TrimSpace(conn.Mode)) == ModeCluster {
		var n int64
		for _, k := range cleaned {
			c, e := client.Del(ctx, k).Result()
			if e != nil {
				return n, e
			}
			n += c
		}
		return n, nil
	}
	return client.Del(ctx, cleaned...).Result()
}

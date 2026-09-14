package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/hello-coder/hello-coder/internal/model"
)

type IndexInfo struct {
	UUID         string   `json:"uuid"`
	Name         string   `json:"name"`
	DocsCount    int64    `json:"docsCount"`
	DocsDeleted  int64    `json:"docsDeleted"`
	StoreSize    string   `json:"storeSize"`
	StoreBytes   int64    `json:"storeBytes"`
	Health       string   `json:"health"`
	Status       string   `json:"status"`
	Aliases      []string `json:"aliases"`
	Segments     int64    `json:"segments"`
	PrimaryShards int     `json:"primaryShards"`
	Replicas     int      `json:"replicas"`
}

type IndexListResult struct {
	Items []IndexInfo `json:"items"`
	Total int         `json:"total"`
}

type IndexStructure struct {
	Name     string         `json:"name"`
	Aliases  []string       `json:"aliases"`
	Mappings map[string]any `json:"mappings"`
	Settings map[string]any `json:"settings"`
}

func ListIndices(conn *model.EsConn, q string) (IndexListResult, error) {
	client, err := newClient(conn)
	if err != nil {
		return IndexListResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	catRes, err := client.Cat.Indices(
		client.Cat.Indices.WithContext(ctx),
		client.Cat.Indices.WithFormat("json"),
		client.Cat.Indices.WithBytes("b"),
		client.Cat.Indices.WithH(
			"health", "status", "index", "uuid",
			"docs.count", "docs.deleted", "store.size",
			"pri", "rep",
		),
	)
	if err != nil {
		return IndexListResult{}, err
	}
	var catRows []map[string]string
	if err := decodeJSON(catRes, &catRows); err != nil {
		return IndexListResult{}, err
	}

	aliasMap, _ := fetchAliasMap(client, ctx)
	segMap, _ := fetchSegmentCounts(client, ctx)

	q = strings.ToLower(strings.TrimSpace(q))
	items := make([]IndexInfo, 0, len(catRows))
	for _, row := range catRows {
		name := row["index"]
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue // skip system indices by default
		}
		if q != "" && !strings.Contains(strings.ToLower(name), q) {
			continue
		}
		aliases := aliasMap[name]
		if aliases == nil {
			aliases = []string{}
		}
		items = append(items, IndexInfo{
			UUID:          row["uuid"],
			Name:          name,
			DocsCount:     parseInt64(row["docs.count"]),
			DocsDeleted:   parseInt64(row["docs.deleted"]),
			StoreSize:     formatBytes(parseInt64(row["store.size"])),
			StoreBytes:    parseInt64(row["store.size"]),
			Health:        row["health"],
			Status:        row["status"],
			Aliases:       aliases,
			Segments:      segMap[name],
			PrimaryShards: int(parseInt64(row["pri"])),
			Replicas:      int(parseInt64(row["rep"])),
		})
	}
	return IndexListResult{Items: items, Total: len(items)}, nil
}

func GetIndexStructure(conn *model.EsConn, index string) (IndexStructure, error) {
	index = strings.TrimSpace(index)
	if index == "" {
		return IndexStructure{}, fmt.Errorf("index is required")
	}
	client, err := newClient(conn)
	if err != nil {
		return IndexStructure{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	res, err := client.Indices.Get(
		[]string{index},
		client.Indices.Get.WithContext(ctx),
	)
	if err != nil {
		return IndexStructure{}, err
	}
	var raw map[string]struct {
		Aliases  map[string]any `json:"aliases"`
		Mappings map[string]any `json:"mappings"`
		Settings map[string]any `json:"settings"`
	}
	if err := decodeJSON(res, &raw); err != nil {
		return IndexStructure{}, err
	}
	info, ok := raw[index]
	if !ok {
		for k, v := range raw {
			index = k
			info = v
			ok = true
			break
		}
	}
	if !ok {
		return IndexStructure{}, fmt.Errorf("index not found: %s", index)
	}
	aliases := make([]string, 0, len(info.Aliases))
	for a := range info.Aliases {
		aliases = append(aliases, a)
	}
	return IndexStructure{
		Name:     index,
		Aliases:  aliases,
		Mappings: info.Mappings,
		Settings: sanitizeSettings(info.Settings),
	}, nil
}

func CreateIndex(conn *model.EsConn, index string, mappings, settings, aliases map[string]any) error {
	index = strings.TrimSpace(index)
	if index == "" {
		return fmt.Errorf("index is required")
	}
	client, err := newClient(conn)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return createIndexWithStructure(client, ctx, index, mappings, settings, aliases)
}

func DeleteIndex(conn *model.EsConn, index string) error {
	index = strings.TrimSpace(index)
	if index == "" {
		return fmt.Errorf("index is required")
	}
	client, err := newClient(conn)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := client.Indices.Delete(
		[]string{index},
		client.Indices.Delete.WithContext(ctx),
	)
	if err != nil {
		return err
	}
	return decodeJSON(res, nil)
}

func ClearIndex(conn *model.EsConn, index string) error {
	index = strings.TrimSpace(index)
	if index == "" {
		return fmt.Errorf("index is required")
	}
	client, err := newClient(conn)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	body := strings.NewReader(`{"query":{"match_all":{}}}`)
	res, err := client.DeleteByQuery(
		[]string{index},
		body,
		client.DeleteByQuery.WithContext(ctx),
		client.DeleteByQuery.WithRefresh(true),
		client.DeleteByQuery.WithWaitForCompletion(true),
		client.DeleteByQuery.WithConflicts("proceed"),
	)
	if err != nil {
		return err
	}
	return decodeJSON(res, nil)
}

func SetIndexAlias(conn *model.EsConn, index, alias string, replace bool) error {
	index = strings.TrimSpace(index)
	alias = strings.TrimSpace(alias)
	if index == "" || alias == "" {
		return fmt.Errorf("index and alias are required")
	}
	client, err := newClient(conn)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	actions := make([]map[string]any, 0, 4)
	if replace {
		cur, err := GetIndexStructure(conn, index)
		if err == nil {
			for _, a := range cur.Aliases {
				actions = append(actions, map[string]any{
					"remove": map[string]any{"index": index, "alias": a},
				})
			}
		}
	}
	actions = append(actions, map[string]any{
		"add": map[string]any{"index": index, "alias": alias},
	})
	payload, _ := json.Marshal(map[string]any{"actions": actions})
	res, err := client.Indices.UpdateAliases(
		bytes.NewReader(payload),
		client.Indices.UpdateAliases.WithContext(ctx),
	)
	if err != nil {
		return err
	}
	return decodeJSON(res, nil)
}

func CloneIndex(conn *model.EsConn, index, target string) error {
	index = strings.TrimSpace(index)
	target = strings.TrimSpace(target)
	if index == "" || target == "" {
		return fmt.Errorf("index and target are required")
	}
	if index == target {
		return fmt.Errorf("target must differ from source index")
	}
	client, err := newClient(conn)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Clone requires write block on source in many ES versions.
	if err := setWriteBlock(client, ctx, index, true); err != nil {
		return fmt.Errorf("set write block: %w", err)
	}
	defer func() { _ = setWriteBlock(client, ctx, index, false) }()

	res, err := client.Indices.Clone(
		index,
		target,
		client.Indices.Clone.WithContext(ctx),
		client.Indices.Clone.WithWaitForActiveShards("1"),
	)
	if err != nil {
		return err
	}
	if err := decodeJSON(res, nil); err != nil {
		return err
	}
	return setWriteBlock(client, ctx, index, false)
}

type RebuildResult struct {
	TempIndex string `json:"tempIndex"`
	Index     string `json:"index"`
	Message   string `json:"message"`
}

func RebuildIndex(conn *model.EsConn, index string, mappings map[string]any, settings map[string]any) (RebuildResult, error) {
	index = strings.TrimSpace(index)
	if index == "" {
		return RebuildResult{}, fmt.Errorf("index is required")
	}
	client, err := newClient(conn)
	if err != nil {
		return RebuildResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	orig, err := GetIndexStructure(conn, index)
	if err != nil {
		return RebuildResult{}, err
	}
	if mappings == nil {
		mappings = orig.Mappings
	}
	if settings == nil {
		settings = orig.Settings
	} else {
		settings = sanitizeSettings(settings)
	}

	temp := fmt.Sprintf("%s.__rebuild_tmp_%d", index, time.Now().Unix())
	if len(temp) > 200 {
		temp = fmt.Sprintf(".__rebuild_tmp_%d", time.Now().Unix())
	}

	// 1) create temp with original structure
	if err := createIndexWithStructure(client, ctx, temp, orig.Mappings, orig.Settings, nil); err != nil {
		return RebuildResult{}, fmt.Errorf("create temp index: %w", err)
	}
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = deleteIndexRaw(client, ctx, temp)
		}
	}()

	// 2) reindex original -> temp
	if err := reindexWait(client, ctx, index, temp); err != nil {
		return RebuildResult{}, fmt.Errorf("copy to temp: %w", err)
	}

	// 3) delete original
	if err := deleteIndexRaw(client, ctx, index); err != nil {
		return RebuildResult{}, fmt.Errorf("delete original: %w", err)
	}

	// 4) create original with new structure
	if err := createIndexWithStructure(client, ctx, index, mappings, settings, nil); err != nil {
		// best effort: try restore from temp with old structure
		_ = createIndexWithStructure(client, ctx, index, orig.Mappings, orig.Settings, nil)
		_ = reindexWait(client, ctx, temp, index)
		return RebuildResult{}, fmt.Errorf("create rebuilt index: %w", err)
	}

	// 5) reindex temp -> original
	if err := reindexWait(client, ctx, temp, index); err != nil {
		return RebuildResult{}, fmt.Errorf("copy back from temp: %w", err)
	}

	// restore aliases
	for _, a := range orig.Aliases {
		_ = SetIndexAlias(conn, index, a, false)
	}

	// 6) delete temp
	if err := deleteIndexRaw(client, ctx, temp); err != nil {
		cleanupTemp = false
		return RebuildResult{
			TempIndex: temp,
			Index:     index,
			Message:   "rebuilt, but failed to delete temp index: " + err.Error(),
		}, nil
	}
	cleanupTemp = false
	return RebuildResult{
		TempIndex: temp,
		Index:     index,
		Message:   "rebuild completed",
	}, nil
}

func fetchAliasMap(client *elasticsearch.Client, ctx context.Context) (map[string][]string, error) {
	res, err := client.Cat.Aliases(
		client.Cat.Aliases.WithContext(ctx),
		client.Cat.Aliases.WithFormat("json"),
		client.Cat.Aliases.WithH("alias", "index"),
	)
	if err != nil {
		return nil, err
	}
	var rows []map[string]string
	if err := decodeJSON(res, &rows); err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, r := range rows {
		idx := r["index"]
		alias := r["alias"]
		if idx == "" || alias == "" {
			continue
		}
		out[idx] = append(out[idx], alias)
	}
	return out, nil
}

func fetchSegmentCounts(client *elasticsearch.Client, ctx context.Context) (map[string]int64, error) {
	res, err := client.Indices.Stats(
		client.Indices.Stats.WithContext(ctx),
		client.Indices.Stats.WithMetric("segments"),
	)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Indices map[string]struct {
			Primaries struct {
				Segments struct {
					Count int64 `json:"count"`
				} `json:"segments"`
			} `json:"primaries"`
		} `json:"indices"`
	}
	if err := decodeJSON(res, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(raw.Indices))
	for name, info := range raw.Indices {
		out[name] = info.Primaries.Segments.Count
	}
	return out, nil
}

func setWriteBlock(client *elasticsearch.Client, ctx context.Context, index string, blocked bool) error {
	body := fmt.Sprintf(`{"index":{"blocks.write":%t}}`, blocked)
	res, err := client.Indices.PutSettings(
		strings.NewReader(body),
		client.Indices.PutSettings.WithContext(ctx),
		client.Indices.PutSettings.WithIndex(index),
	)
	if err != nil {
		return err
	}
	return decodeJSON(res, nil)
}

func createIndexWithStructure(client *elasticsearch.Client, ctx context.Context, name string, mappings, settings, aliases map[string]any) error {
	body := map[string]any{}
	if len(mappings) > 0 {
		body["mappings"] = mappings
	}
	if len(settings) > 0 {
		// settings may already be wrapped as {"index":{...}} or flat
		body["settings"] = settings
	}
	if len(aliases) > 0 {
		body["aliases"] = aliases
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	res, err := client.Indices.Create(
		name,
		client.Indices.Create.WithContext(ctx),
		client.Indices.Create.WithBody(bytes.NewReader(payload)),
	)
	if err != nil {
		return err
	}
	return decodeJSON(res, nil)
}

func deleteIndexRaw(client *elasticsearch.Client, ctx context.Context, name string) error {
	res, err := client.Indices.Delete(
		[]string{name},
		client.Indices.Delete.WithContext(ctx),
		client.Indices.Delete.WithIgnoreUnavailable(true),
	)
	if err != nil {
		return err
	}
	return decodeJSON(res, nil)
}

func reindexWait(client *elasticsearch.Client, ctx context.Context, source, dest string) error {
	payload, _ := json.Marshal(map[string]any{
		"source": map[string]any{"index": source},
		"dest":   map[string]any{"index": dest},
	})
	res, err := client.Reindex(
		bytes.NewReader(payload),
		client.Reindex.WithContext(ctx),
		client.Reindex.WithWaitForCompletion(true),
		client.Reindex.WithRefresh(true),
	)
	if err != nil {
		return err
	}
	return decodeJSON(res, nil)
}

func sanitizeSettings(settings map[string]any) map[string]any {
	if settings == nil {
		return map[string]any{}
	}
	// Clone via JSON to avoid mutating caller.
	b, err := json.Marshal(settings)
	if err != nil {
		return map[string]any{}
	}
	var cloned map[string]any
	if err := json.Unmarshal(b, &cloned); err != nil {
		return map[string]any{}
	}

	stripKeys := map[string]bool{
		"uuid":                 true,
		"creation_date":        true,
		"creation_date_string": true,
		"version":              true,
		"provided_name":        true,
		"routing_num_shards":   true,
		"resize":               true,
		"blocks":               true,
	}

	cleanIndexMap := func(m map[string]any) map[string]any {
		out := make(map[string]any, len(m))
		for k, v := range m {
			if stripKeys[k] {
				continue
			}
			out[k] = v
		}
		return out
	}

	if idx, ok := cloned["index"].(map[string]any); ok {
		cleaned := cleanIndexMap(idx)
		if len(cleaned) == 0 {
			return map[string]any{}
		}
		return map[string]any{"index": cleaned}
	}
	return cleanIndexMap(cloned)
}

func parseInt64(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		f, err2 := strconv.ParseFloat(s, 64)
		if err2 != nil {
			return 0
		}
		return int64(f)
	}
	return n
}

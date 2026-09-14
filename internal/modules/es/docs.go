package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/hello-coder/hello-coder/internal/model"
)

type DocFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"` // * | = | != | > | >= | < | <=
	Value string `json:"value"`
}

type DocSearchRequest struct {
	Filters   []DocFilter     `json:"filters"`
	From      int             `json:"from"`
	Size      int             `json:"size"`
	SortField string          `json:"sortField"`
	SortOrder string          `json:"sortOrder"` // asc | desc
	Dsl       json.RawMessage `json:"dsl,omitempty"` // raw ES query or full search body
}

type DocHit struct {
	ID     string         `json:"id"`
	Index  string         `json:"index"`
	Score  *float64       `json:"score,omitempty"`
	Source map[string]any `json:"source"`
}

type DocSearchResult struct {
	Total  int64    `json:"total"`
	Items  []DocHit `json:"items"`
	Fields []string `json:"fields,omitempty"`
}

type FieldInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type DocCreateRequest struct {
	ID     string         `json:"id"`
	Source map[string]any `json:"source"`
}

type DocCreateResult struct {
	ID     string `json:"id"`
	Index  string `json:"index"`
	Result string `json:"result"`
}

func ListIndexFields(conn *model.EsConn, index string) ([]string, error) {
	infos, err := ListIndexFieldInfos(conn, index, true)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(infos)+1)
	out = append(out, "_id")
	for _, f := range infos {
		out = append(out, f.Name)
	}
	return out, nil
}

func ListIndexFieldInfos(conn *model.EsConn, index string, includeMulti bool) ([]FieldInfo, error) {
	st, err := GetIndexStructure(conn, index)
	if err != nil {
		return nil, err
	}
	return collectMappingFieldInfos(st.Mappings, "", includeMulti), nil
}

func SearchDocs(conn *model.EsConn, index string, req DocSearchRequest) (DocSearchResult, error) {
	index = strings.TrimSpace(index)
	if index == "" {
		return DocSearchResult{}, fmt.Errorf("index is required")
	}
	client, err := newClient(conn)
	if err != nil {
		return DocSearchResult{}, err
	}

	from := req.From
	if from < 0 {
		from = 0
	}
	size := req.Size
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}

	var body map[string]any
	if len(bytes.TrimSpace(req.Dsl)) > 0 && string(req.Dsl) != "null" {
		body, err = buildBodyFromDsl(req.Dsl, from, size)
		if err != nil {
			return DocSearchResult{}, err
		}
	} else {
		body = map[string]any{
			"from":  from,
			"size":  size,
			"query": buildDocQuery(req.Filters),
			"sort":  buildDocSort(req.SortField, req.SortOrder),
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return DocSearchResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := client.Search(
		client.Search.WithContext(ctx),
		client.Search.WithIndex(index),
		client.Search.WithBody(bytes.NewReader(payload)),
		client.Search.WithTrackTotalHits(true),
	)
	if err != nil {
		return DocSearchResult{}, err
	}

	var raw struct {
		Hits struct {
			Total any `json:"total"`
			Hits  []struct {
				ID     string         `json:"_id"`
				Index  string         `json:"_index"`
				Score  *float64       `json:"_score"`
				Source map[string]any `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := decodeJSON(res, &raw); err != nil {
		return DocSearchResult{}, err
	}

	items := make([]DocHit, 0, len(raw.Hits.Hits))
	for _, h := range raw.Hits.Hits {
		src := h.Source
		if src == nil {
			src = map[string]any{}
		}
		items = append(items, DocHit{
			ID:     h.ID,
			Index:  h.Index,
			Score:  h.Score,
			Source: src,
		})
	}
	return DocSearchResult{
		Total: parseHitsTotal(raw.Hits.Total),
		Items: items,
	}, nil
}

func CreateDoc(conn *model.EsConn, index string, req DocCreateRequest) (DocCreateResult, error) {
	return indexDoc(conn, index, strings.TrimSpace(req.ID), req.Source, false)
}

// UpdateDoc rewrites the entire _source for the given document id via Index + DocumentID.
func UpdateDoc(conn *model.EsConn, index, docID string, source map[string]any) (DocCreateResult, error) {
	return indexDoc(conn, index, strings.TrimSpace(docID), source, true)
}

func indexDoc(conn *model.EsConn, index, docID string, source map[string]any, requireID bool) (DocCreateResult, error) {
	index = strings.TrimSpace(index)
	if index == "" {
		return DocCreateResult{}, fmt.Errorf("index is required")
	}
	if requireID && docID == "" {
		return DocCreateResult{}, fmt.Errorf("document id is required")
	}
	if source == nil {
		source = map[string]any{}
	}
	client, err := newClient(conn)
	if err != nil {
		return DocCreateResult{}, err
	}
	payload, err := json.Marshal(source)
	if err != nil {
		return DocCreateResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	opts := []func(*esapi.IndexRequest){
		client.Index.WithContext(ctx),
		client.Index.WithRefresh("true"),
	}
	if docID != "" {
		opts = append(opts, client.Index.WithDocumentID(docID))
	}

	res, err := client.Index(index, bytes.NewReader(payload), opts...)
	if err != nil {
		return DocCreateResult{}, err
	}

	var raw struct {
		ID     string `json:"_id"`
		Index  string `json:"_index"`
		Result string `json:"result"`
	}
	if err := decodeJSON(res, &raw); err != nil {
		return DocCreateResult{}, err
	}
	return DocCreateResult{
		ID:     raw.ID,
		Index:  raw.Index,
		Result: raw.Result,
	}, nil
}

type DocIDsRequest struct {
	IDs []string `json:"ids"`
}

type DocBulkDeleteResult struct {
	Deleted int64 `json:"deleted"`
}

type DocBulkUpdateRequest struct {
	IDs  []string       `json:"ids"`
	Doc  map[string]any `json:"doc"`
}

type DocBulkUpdateResult struct {
	Updated int64 `json:"updated"`
	Failed  int64 `json:"failed"`
}

func normalizeDocIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func DeleteDocs(conn *model.EsConn, index string, ids []string) (DocBulkDeleteResult, error) {
	index = strings.TrimSpace(index)
	if index == "" {
		return DocBulkDeleteResult{}, fmt.Errorf("index is required")
	}
	ids = normalizeDocIDs(ids)
	if len(ids) == 0 {
		return DocBulkDeleteResult{}, fmt.Errorf("ids are required")
	}
	client, err := newClient(conn)
	if err != nil {
		return DocBulkDeleteResult{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"query": map[string]any{
			"ids": map[string]any{"values": ids},
		},
	})
	if err != nil {
		return DocBulkDeleteResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := client.DeleteByQuery(
		[]string{index},
		bytes.NewReader(payload),
		client.DeleteByQuery.WithContext(ctx),
		client.DeleteByQuery.WithRefresh(true),
		client.DeleteByQuery.WithWaitForCompletion(true),
		client.DeleteByQuery.WithConflicts("proceed"),
	)
	if err != nil {
		return DocBulkDeleteResult{}, err
	}

	var raw struct {
		Deleted int64 `json:"deleted"`
	}
	if err := decodeJSON(res, &raw); err != nil {
		return DocBulkDeleteResult{}, err
	}
	return DocBulkDeleteResult{Deleted: raw.Deleted}, nil
}

func BulkUpdateDocs(conn *model.EsConn, index string, ids []string, doc map[string]any) (DocBulkUpdateResult, error) {
	index = strings.TrimSpace(index)
	if index == "" {
		return DocBulkUpdateResult{}, fmt.Errorf("index is required")
	}
	ids = normalizeDocIDs(ids)
	if len(ids) == 0 {
		return DocBulkUpdateResult{}, fmt.Errorf("ids are required")
	}
	if doc == nil || len(doc) == 0 {
		return DocBulkUpdateResult{}, fmt.Errorf("doc is required")
	}
	client, err := newClient(conn)
	if err != nil {
		return DocBulkUpdateResult{}, err
	}

	var buf bytes.Buffer
	for _, id := range ids {
		meta, err := json.Marshal(map[string]any{
			"update": map[string]any{"_id": id},
		})
		if err != nil {
			return DocBulkUpdateResult{}, err
		}
		body, err := json.Marshal(map[string]any{"doc": doc})
		if err != nil {
			return DocBulkUpdateResult{}, err
		}
		buf.Write(meta)
		buf.WriteByte('\n')
		buf.Write(body)
		buf.WriteByte('\n')
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := client.Bulk(
		bytes.NewReader(buf.Bytes()),
		client.Bulk.WithContext(ctx),
		client.Bulk.WithIndex(index),
		client.Bulk.WithRefresh("true"),
	)
	if err != nil {
		return DocBulkUpdateResult{}, err
	}

	var raw struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			Status int            `json:"status"`
			Error  map[string]any `json:"error"`
			Result string         `json:"result"`
		} `json:"items"`
	}
	if err := decodeJSON(res, &raw); err != nil {
		return DocBulkUpdateResult{}, err
	}

	var updated, failed int64
	for _, item := range raw.Items {
		op, ok := item["update"]
		if !ok {
			failed++
			continue
		}
		if op.Error != nil || op.Status >= 300 {
			failed++
			continue
		}
		updated++
	}
	return DocBulkUpdateResult{Updated: updated, Failed: failed}, nil
}

func buildBodyFromDsl(raw json.RawMessage, from, size int) (map[string]any, error) {
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("invalid dsl json: %w", err)
	}
	m, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("dsl must be a JSON object")
	}
	if _, hasQuery := m["query"]; hasQuery {
		body := make(map[string]any, len(m)+2)
		for k, v := range m {
			body[k] = v
		}
		body["from"] = from
		body["size"] = size
		return body, nil
	}
	return map[string]any{
		"from":  from,
		"size":  size,
		"query": m,
	}, nil
}

func buildDocQuery(filters []DocFilter) map[string]any {
	must := make([]map[string]any, 0, len(filters))
	for _, f := range filters {
		field := strings.TrimSpace(f.Field)
		value := strings.TrimSpace(f.Value)
		if field == "" || value == "" {
			continue
		}
		op := strings.TrimSpace(f.Op)
		switch op {
		case "*", "contains":
			must = append(must, queryContains(field, value))
		case "=", "equals", "eq":
			must = append(must, queryEquals(field, value))
		case "!=", "not_equals", "ne", "neq":
			must = append(must, boolMustNot(queryEquals(field, value)))
		case ">", "gt":
			must = append(must, queryRange(field, "gt", value))
		case ">=", "gte", "ge":
			must = append(must, queryRange(field, "gte", value))
		case "<", "lt":
			must = append(must, queryRange(field, "lt", value))
		case "<=", "lte", "le":
			must = append(must, queryRange(field, "lte", value))
		default:
			must = append(must, queryContains(field, value))
		}
	}
	if len(must) == 0 {
		return map[string]any{"match_all": map[string]any{}}
	}
	return map[string]any{
		"bool": map[string]any{"must": must},
	}
}

func buildDocSort(field, order string) []any {
	order = strings.ToLower(strings.TrimSpace(order))
	if order != "desc" {
		order = "asc"
	}
	field = strings.TrimSpace(field)
	// _id has no doc_values / mapping for sort in modern ES; use _doc instead.
	if field == "" || field == "_doc" || field == "_id" {
		return []any{map[string]any{"_doc": order}}
	}
	return []any{
		map[string]any{
			field: map[string]any{
				"order":         order,
				"unmapped_type": "keyword",
				"missing":       "_last",
			},
		},
	}
}

func boolMustNot(q map[string]any) map[string]any {
	return map[string]any{
		"bool": map[string]any{"must_not": []map[string]any{q}},
	}
}

func queryContains(field, value string) map[string]any {
	if field == "_id" {
		return map[string]any{
			"wildcard": map[string]any{
				"_id": map[string]any{
					"value":            "*" + escapeWildcard(value) + "*",
					"case_insensitive": true,
				},
			},
		}
	}
	return map[string]any{
		"query_string": map[string]any{
			"fields":           []string{field},
			"query":            "*" + escapeQueryString(value) + "*",
			"analyze_wildcard": true,
			"default_operator": "AND",
		},
	}
}

func queryEquals(field, value string) map[string]any {
	if field == "_id" {
		return map[string]any{"ids": map[string]any{"values": []string{value}}}
	}
	return map[string]any{
		"match": map[string]any{
			field: map[string]any{
				"query":    value,
				"operator": "and",
			},
		},
	}
}

func queryRange(field, op, value string) map[string]any {
	var v any = value
	if n, err := strconv.ParseFloat(value, 64); err == nil {
		v = n
	}
	return map[string]any{
		"range": map[string]any{
			field: map[string]any{op: v},
		},
	}
}

func escapeWildcard(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`)
	return replacer.Replace(s)
}

func escapeQueryString(s string) string {
	special := `\+-!():^[]"{}~*?|&/`
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func parseHitsTotal(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	case map[string]any:
		switch vv := t["value"].(type) {
		case float64:
			return int64(vv)
		case json.Number:
			n, _ := vv.Int64()
			return n
		}
	}
	return 0
}

func collectMappingFields(node map[string]any, prefix string) []string {
	infos := collectMappingFieldInfos(node, prefix, true)
	out := make([]string, 0, len(infos))
	for _, f := range infos {
		out = append(out, f.Name)
	}
	return out
}

func collectMappingFieldInfos(node map[string]any, prefix string, includeMulti bool) []FieldInfo {
	if node == nil {
		return nil
	}
	props, _ := node["properties"].(map[string]any)
	if props == nil {
		// legacy typed mappings: { "_doc": { "properties": {...} } }
		for _, raw := range node {
			child, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if nested, ok := child["properties"].(map[string]any); ok && nested != nil {
				return collectMappingFieldInfos(map[string]any{"properties": nested}, prefix, includeMulti)
			}
		}
		return nil
	}

	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]FieldInfo, 0, len(props))
	for _, name := range names {
		child, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if nested, ok := child["properties"].(map[string]any); ok && nested != nil {
			out = append(out, collectMappingFieldInfos(map[string]any{"properties": nested}, path, includeMulti)...)
			continue
		}
		typ, _ := child["type"].(string)
		if typ == "" {
			typ = "object"
		}
		out = append(out, FieldInfo{Name: path, Type: typ})
		if !includeMulti {
			continue
		}
		if fields, ok := child["fields"].(map[string]any); ok {
			subs := make([]string, 0, len(fields))
			for sub := range fields {
				subs = append(subs, sub)
			}
			sort.Strings(subs)
			for _, sub := range subs {
				subPath := path + "." + sub
				subType := "keyword"
				if m, ok := fields[sub].(map[string]any); ok {
					if t, ok := m["type"].(string); ok && t != "" {
						subType = t
					}
				}
				out = append(out, FieldInfo{Name: subPath, Type: subType})
			}
		}
	}
	return out
}

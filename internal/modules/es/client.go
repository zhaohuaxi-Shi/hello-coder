package es

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/hello-coder/hello-coder/internal/model"
)

type PingResult struct {
	Available   bool   `json:"available"`
	Status      string `json:"status,omitempty"`
	Version     string `json:"version,omitempty"`
	ClusterName string `json:"clusterName,omitempty"`
	NodeCount   int    `json:"nodeCount,omitempty"`
	Message     string `json:"message,omitempty"`
}

type ClusterOverview struct {
	Available            bool   `json:"available"`
	Status               string `json:"status,omitempty"`
	ClusterName          string `json:"clusterName,omitempty"`
	ClusterUUID          string `json:"clusterUuid,omitempty"`
	Version              string `json:"version,omitempty"`
	NumberOfNodes        int    `json:"numberOfNodes,omitempty"`
	NumberOfDataNodes    int    `json:"numberOfDataNodes,omitempty"`
	ActivePrimaryShards  int    `json:"activePrimaryShards,omitempty"`
	ActiveShards         int    `json:"activeShards,omitempty"`
	RelocatingShards     int    `json:"relocatingShards,omitempty"`
	InitializingShards   int    `json:"initializingShards,omitempty"`
	UnassignedShards     int    `json:"unassignedShards,omitempty"`
	DelayedUnassigned    int    `json:"delayedUnassignedShards,omitempty"`
	TimedOut             bool   `json:"timedOut,omitempty"`
	Message              string `json:"message,omitempty"`
}

type NodeDetail struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Host                 string   `json:"host"`
	IP                   string   `json:"ip"`
	TransportAddress     string   `json:"transportAddress"`
	HTTPPublishAddress   string   `json:"httpPublishAddress,omitempty"`
	Version              string   `json:"version"`
	Roles                []string `json:"roles"`
	IsMaster             bool     `json:"isMaster"`
	IsMasterEligible     bool     `json:"isMasterEligible"`
	IsData               bool     `json:"isData"`
	IsIngest             bool     `json:"isIngest"`
	IsCoordinatingOnly   bool     `json:"isCoordinatingOnly"`
	OSName               string   `json:"osName,omitempty"`
	OSArch               string   `json:"osArch,omitempty"`
	OSPrettyName         string   `json:"osPrettyName,omitempty"`
	AvailableProcessors  int      `json:"availableProcessors,omitempty"`
	AllocatedProcessors  int      `json:"allocatedProcessors,omitempty"`
	JVMVersion           string   `json:"jvmVersion,omitempty"`
	JVMVMName            string   `json:"jvmVmName,omitempty"`
	JVMHeapInit          string   `json:"jvmHeapInit,omitempty"`
	JVMHeapMax           string   `json:"jvmHeapMax,omitempty"`
	JVMPID               int64    `json:"jvmPid,omitempty"`
	TotalIndexingBuffer  string   `json:"totalIndexingBuffer,omitempty"`
	Plugins              []string `json:"plugins,omitempty"`
}

type ClusterDetail struct {
	Overview     ClusterOverview `json:"overview"`
	MasterNodeID string          `json:"masterNodeId,omitempty"`
	Nodes        []NodeDetail    `json:"nodes"`
}

func addressList(conn *model.EsConn) []string {
	parts := strings.Split(conn.Addresses, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "http://") && !strings.HasPrefix(p, "https://") {
			p = "http://" + p
		}
		out = append(out, strings.TrimRight(p, "/"))
	}
	return out
}

func newClient(conn *model.EsConn) (*elasticsearch.Client, error) {
	addrs := addressList(conn)
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses configured")
	}
	cfg := elasticsearch.Config{
		Addresses: addrs,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // local ops tool
		},
	}
	if conn.Username != "" {
		cfg.Username = conn.Username
		cfg.Password = conn.Password
	}
	return elasticsearch.NewClient(cfg)
}

func decodeJSON(res *esapi.Response, dest any) error {
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.IsError() {
		return fmt.Errorf("elasticsearch %s: %s", res.Status(), strings.TrimSpace(string(body)))
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(body, dest)
}

func Ping(conn *model.EsConn) PingResult {
	ov := FetchOverview(conn)
	return PingResult{
		Available:   ov.Available,
		Status:      ov.Status,
		Version:     ov.Version,
		ClusterName: ov.ClusterName,
		NodeCount:   ov.NumberOfNodes,
		Message:     ov.Message,
	}
}

func FetchOverview(conn *model.EsConn) ClusterOverview {
	client, err := newClient(conn)
	if err != nil {
		return ClusterOverview{Available: false, Message: err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	infoRes, err := client.Info(client.Info.WithContext(ctx))
	if err != nil {
		return ClusterOverview{Available: false, Message: err.Error()}
	}
	var info struct {
		ClusterName string `json:"cluster_name"`
		ClusterUUID string `json:"cluster_uuid"`
		Version     struct {
			Number string `json:"number"`
		} `json:"version"`
	}
	if err := decodeJSON(infoRes, &info); err != nil {
		return ClusterOverview{Available: false, Message: err.Error()}
	}

	healthRes, err := client.Cluster.Health(client.Cluster.Health.WithContext(ctx))
	if err != nil {
		return ClusterOverview{
			Available:   true,
			ClusterName: info.ClusterName,
			ClusterUUID: info.ClusterUUID,
			Version:     info.Version.Number,
			Message:     "connected, but health failed: " + err.Error(),
		}
	}
	var health struct {
		ClusterName                 string `json:"cluster_name"`
		Status                      string `json:"status"`
		TimedOut                    bool   `json:"timed_out"`
		NumberOfNodes               int    `json:"number_of_nodes"`
		NumberOfDataNodes           int    `json:"number_of_data_nodes"`
		ActivePrimaryShards         int    `json:"active_primary_shards"`
		ActiveShards                int    `json:"active_shards"`
		RelocatingShards            int    `json:"relocating_shards"`
		InitializingShards          int    `json:"initializing_shards"`
		UnassignedShards            int    `json:"unassigned_shards"`
		DelayedUnassignedShards     int    `json:"delayed_unassigned_shards"`
	}
	if err := decodeJSON(healthRes, &health); err != nil {
		return ClusterOverview{
			Available:   true,
			ClusterName: info.ClusterName,
			ClusterUUID: info.ClusterUUID,
			Version:     info.Version.Number,
			Message:     "connected, but health parse failed: " + err.Error(),
		}
	}

	name := health.ClusterName
	if name == "" {
		name = info.ClusterName
	}
	return ClusterOverview{
		Available:           true,
		Status:              strings.ToLower(health.Status),
		ClusterName:         name,
		ClusterUUID:         info.ClusterUUID,
		Version:             info.Version.Number,
		NumberOfNodes:       health.NumberOfNodes,
		NumberOfDataNodes:   health.NumberOfDataNodes,
		ActivePrimaryShards: health.ActivePrimaryShards,
		ActiveShards:        health.ActiveShards,
		RelocatingShards:    health.RelocatingShards,
		InitializingShards:  health.InitializingShards,
		UnassignedShards:    health.UnassignedShards,
		DelayedUnassigned:   health.DelayedUnassignedShards,
		TimedOut:            health.TimedOut,
		Message:             "ok",
	}
}

func FetchClusterDetail(conn *model.EsConn) (ClusterDetail, error) {
	overview := FetchOverview(conn)
	if !overview.Available {
		return ClusterDetail{Overview: overview, Nodes: []NodeDetail{}}, fmt.Errorf("%s", overview.Message)
	}

	client, err := newClient(conn)
	if err != nil {
		return ClusterDetail{Overview: overview, Nodes: []NodeDetail{}}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	masterID := ""
	stateRes, err := client.Cluster.State(
		client.Cluster.State.WithContext(ctx),
		client.Cluster.State.WithMetric("master_node"),
	)
	if err == nil {
		var state struct {
			MasterNode string `json:"master_node"`
		}
		if err := decodeJSON(stateRes, &state); err == nil {
			masterID = state.MasterNode
		}
	} else if stateRes != nil && stateRes.Body != nil {
		_ = stateRes.Body.Close()
	}

	nodesRes, err := client.Nodes.Info(
		client.Nodes.Info.WithContext(ctx),
		client.Nodes.Info.WithMetric("settings", "os", "jvm", "http", "plugins"),
	)
	if err != nil {
		return ClusterDetail{Overview: overview, MasterNodeID: masterID, Nodes: []NodeDetail{}}, err
	}

	var raw struct {
		Nodes map[string]struct {
			Name             string   `json:"name"`
			Host             string   `json:"host"`
			IP               string   `json:"ip"`
			TransportAddress string   `json:"transport_address"`
			Version          string   `json:"version"`
			Roles            []string `json:"roles"`
			TotalIndexingBuf any      `json:"total_indexing_buffer"`
			HTTP             struct {
				PublishAddress string `json:"publish_address"`
			} `json:"http"`
			OS struct {
				Name                 string `json:"name"`
				PrettyName           string `json:"pretty_name"`
				Arch                 string `json:"arch"`
				AvailableProcessors  int    `json:"available_processors"`
				AllocatedProcessors  int    `json:"allocated_processors"`
			} `json:"os"`
			JVM struct {
				PID     int64  `json:"pid"`
				Version string `json:"version"`
				VMName  string `json:"vm_name"`
				Mem     struct {
					HeapInitInBytes int64 `json:"heap_init_in_bytes"`
					HeapMaxInBytes  int64 `json:"heap_max_in_bytes"`
				} `json:"mem"`
			} `json:"jvm"`
			Plugins []struct {
				Name string `json:"name"`
			} `json:"plugins"`
		} `json:"nodes"`
	}
	if err := decodeJSON(nodesRes, &raw); err != nil {
		return ClusterDetail{Overview: overview, MasterNodeID: masterID, Nodes: []NodeDetail{}}, err
	}

	nodes := make([]NodeDetail, 0, len(raw.Nodes))
	for id, n := range raw.Nodes {
		roles := n.Roles
		if roles == nil {
			roles = []string{}
		}
		isMasterEligible := hasRole(roles, "master")
		isData := hasAnyRole(roles, "data", "data_hot", "data_warm", "data_cold", "data_frozen", "data_content")
		isIngest := hasRole(roles, "ingest")
		isCoordOnly := !isMasterEligible && !isData && !isIngest &&
			!hasRole(roles, "ml") && !hasRole(roles, "transform") && !hasRole(roles, "voting_only")

		plugins := make([]string, 0, len(n.Plugins))
		for _, p := range n.Plugins {
			if p.Name != "" {
				plugins = append(plugins, p.Name)
			}
		}
		sort.Strings(plugins)

		nodes = append(nodes, NodeDetail{
			ID:                  id,
			Name:                n.Name,
			Host:                n.Host,
			IP:                  n.IP,
			TransportAddress:    n.TransportAddress,
			HTTPPublishAddress:  n.HTTP.PublishAddress,
			Version:             n.Version,
			Roles:               roles,
			IsMaster:            masterID != "" && id == masterID,
			IsMasterEligible:    isMasterEligible,
			IsData:              isData,
			IsIngest:            isIngest,
			IsCoordinatingOnly:  isCoordOnly,
			OSName:              n.OS.Name,
			OSArch:              n.OS.Arch,
			OSPrettyName:        n.OS.PrettyName,
			AvailableProcessors: n.OS.AvailableProcessors,
			AllocatedProcessors: n.OS.AllocatedProcessors,
			JVMVersion:          n.JVM.Version,
			JVMVMName:           n.JVM.VMName,
			JVMHeapInit:         formatBytes(n.JVM.Mem.HeapInitInBytes),
			JVMHeapMax:          formatBytes(n.JVM.Mem.HeapMaxInBytes),
			JVMPID:              n.JVM.PID,
			TotalIndexingBuffer: formatAnySize(n.TotalIndexingBuf),
			Plugins:             plugins,
		})
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsMaster != nodes[j].IsMaster {
			return nodes[i].IsMaster
		}
		return nodes[i].Name < nodes[j].Name
	})

	return ClusterDetail{
		Overview:     overview,
		MasterNodeID: masterID,
		Nodes:        nodes,
	}, nil
}

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if strings.EqualFold(r, want) {
			return true
		}
	}
	return false
}

func hasAnyRole(roles []string, wants ...string) bool {
	for _, w := range wants {
		if hasRole(roles, w) {
			return true
		}
	}
	return false
}

func formatAnySize(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return formatBytes(int64(t))
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return t.String()
		}
		return formatBytes(n)
	default:
		return fmt.Sprint(t)
	}
}

func formatBytes(n int64) string {
	if n <= 0 {
		return ""
	}
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

package kafka

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/IBM/sarama"
	"github.com/hello-coder/hello-coder/internal/model"
)

type BrokerNode struct {
	ID           int32  `json:"id"`
	Addr         string `json:"addr"`
	Host         string `json:"host,omitempty"`
	Port         string `json:"port,omitempty"`
	Rack         string `json:"rack,omitempty"`
	IsController bool   `json:"isController"`
	Connected    bool   `json:"connected"`
	LeaderCount  int    `json:"leaderCount"`
	ReplicaCount int    `json:"replicaCount"`
	LogDirCount  int    `json:"logDirCount,omitempty"`
	LogDirSize   int64  `json:"logDirSize,omitempty"`
}

type ClusterOverview struct {
	Available      bool   `json:"available"`
	Message        string `json:"message,omitempty"`
	ClusterID      string `json:"clusterId,omitempty"`
	ControllerID   int32  `json:"controllerId"`
	ControllerAddr string `json:"controllerAddr,omitempty"`
	BrokerCount    int    `json:"brokerCount"`
	TopicCount     int    `json:"topicCount"`
	PartitionCount int    `json:"partitionCount"`
	GroupCount     int    `json:"groupCount"`
}

type ClusterDetail struct {
	Overview ClusterOverview `json:"overview"`
	Nodes    []BrokerNode    `json:"nodes"`
}

func FetchClusterDetail(conn *model.KafkaConn) (ClusterDetail, error) {
	empty := ClusterDetail{
		Overview: ClusterOverview{Available: false, ControllerID: -1},
		Nodes:    []BrokerNode{},
	}

	client, err := openClient(conn)
	if err != nil {
		empty.Overview.Message = err.Error()
		return empty, err
	}
	defer client.Close()

	if err := client.RefreshMetadata(); err != nil {
		empty.Overview.Message = err.Error()
		return empty, err
	}

	controllerID := int32(-1)
	controllerAddr := ""
	if ctrl, err := client.Controller(); err == nil && ctrl != nil {
		controllerID = ctrl.ID()
		controllerAddr = ctrl.Addr()
	}

	nodesByID := make(map[int32]*BrokerNode)
	for _, b := range client.Brokers() {
		connected := false
		if err := b.Open(client.Config()); err == nil {
			connected, _ = b.Connected()
		}
		host, port := splitBrokerAddr(b.Addr())
		nodesByID[b.ID()] = &BrokerNode{
			ID:           b.ID(),
			Addr:         b.Addr(),
			Host:         host,
			Port:         port,
			Rack:         b.Rack(),
			IsController: controllerID >= 0 && b.ID() == controllerID,
			Connected:    connected,
		}
	}

	clusterID := ""
	topicCount := 0
	partitionCount := 0
	if meta, err := fetchClusterMetadata(client); err == nil && meta != nil {
		if meta.ClusterID != nil {
			clusterID = strings.TrimSpace(*meta.ClusterID)
		}
		if controllerID < 0 && meta.ControllerID >= 0 {
			controllerID = meta.ControllerID
		}
		for _, tm := range meta.Topics {
			if tm == nil {
				continue
			}
			if !isInternalTopic(tm.Name, tm.IsInternal) {
				topicCount++
			}
			for _, p := range tm.Partitions {
				if p == nil {
					continue
				}
				partitionCount++
				if n := nodesByID[p.Leader]; n != nil {
					n.LeaderCount++
				}
				for _, rid := range p.Replicas {
					if n := nodesByID[rid]; n != nil {
						n.ReplicaCount++
					}
				}
			}
		}
		for _, b := range meta.Brokers {
			if b == nil {
				continue
			}
			if _, ok := nodesByID[b.ID()]; ok {
				continue
			}
			host, port := splitBrokerAddr(b.Addr())
			nodesByID[b.ID()] = &BrokerNode{
				ID:           b.ID(),
				Addr:         b.Addr(),
				Host:         host,
				Port:         port,
				Rack:         b.Rack(),
				IsController: controllerID >= 0 && b.ID() == controllerID,
			}
		}
	} else {
		topics, err := client.Topics()
		if err == nil {
			for _, name := range topics {
				if isInternalTopic(name, false) {
					continue
				}
				topicCount++
				parts, err := client.Partitions(name)
				if err != nil {
					continue
				}
				partitionCount += len(parts)
				for _, p := range parts {
					if leader, err := client.Leader(name, p); err == nil && leader != nil {
						if n := nodesByID[leader.ID()]; n != nil {
							n.LeaderCount++
						}
					}
					if replicas, err := client.Replicas(name, p); err == nil {
						for _, rid := range replicas {
							if n := nodesByID[rid]; n != nil {
								n.ReplicaCount++
							}
						}
					}
				}
			}
		}
	}

	if controllerAddr == "" {
		if n := nodesByID[controllerID]; n != nil {
			controllerAddr = n.Addr
			n.IsController = true
		}
	}

	groupCount := 0
	admin, err := openAdmin(conn)
	if err == nil {
		if groups, gerr := admin.ListConsumerGroups(); gerr == nil {
			groupCount = len(groups)
		}
		ids := make([]int32, 0, len(nodesByID))
		for id := range nodesByID {
			ids = append(ids, id)
		}
		if dirs, derr := admin.DescribeLogDirs(ids); derr == nil {
			for id, entries := range dirs {
				n := nodesByID[id]
				if n == nil {
					continue
				}
				n.LogDirCount = len(entries)
				var size int64
				for _, d := range entries {
					for _, t := range d.Topics {
						for _, p := range t.Partitions {
							if p.Size > 0 {
								size += p.Size
							}
						}
					}
				}
				n.LogDirSize = size
			}
		}
		_ = admin.Close()
	}

	nodes := make([]BrokerNode, 0, len(nodesByID))
	for _, n := range nodesByID {
		nodes = append(nodes, *n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsController != nodes[j].IsController {
			return nodes[i].IsController
		}
		return nodes[i].ID < nodes[j].ID
	})

	if controllerID < 0 && len(nodes) == 0 {
		empty.Overview.Message = "no brokers in metadata"
		return empty, fmt.Errorf("no brokers in metadata")
	}

	return ClusterDetail{
		Overview: ClusterOverview{
			Available:      true,
			Message:        "ok",
			ClusterID:      clusterID,
			ControllerID:   controllerID,
			ControllerAddr: controllerAddr,
			BrokerCount:    len(nodes),
			TopicCount:     topicCount,
			PartitionCount: partitionCount,
			GroupCount:     groupCount,
		},
		Nodes: nodes,
	}, nil
}

func fetchClusterMetadata(client sarama.Client) (*sarama.MetadataResponse, error) {
	brokers := client.Brokers()
	if len(brokers) == 0 {
		return nil, fmt.Errorf("no brokers")
	}
	var lastErr error
	for _, b := range brokers {
		if err := b.Open(client.Config()); err != nil {
			lastErr = err
			continue
		}
		resp, err := b.GetMetadata(&sarama.MetadataRequest{Version: 4})
		if err != nil {
			lastErr = err
			continue
		}
		return resp, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no reachable broker")
	}
	return nil, lastErr
}

func isInternalTopic(name string, flagged bool) bool {
	if flagged {
		return true
	}
	return strings.HasPrefix(name, "_") || strings.HasPrefix(name, "__")
}

func splitBrokerAddr(addr string) (host, port string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", ""
	}
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, ""
	}
	return h, p
}

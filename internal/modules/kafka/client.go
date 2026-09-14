package kafka

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/hello-coder/hello-coder/internal/model"
)

// Preview consume must not block like a long-lived consumer.
const consumeWaitTimeout = 1500 * time.Millisecond

func buildConfig(conn *model.KafkaConn) (*sarama.Config, error) {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_6_0_0
	cfg.Admin.Timeout = 10 * time.Second
	cfg.Net.DialTimeout = 5 * time.Second
	cfg.Net.ReadTimeout = 10 * time.Second
	cfg.Net.WriteTimeout = 10 * time.Second
	cfg.Metadata.Timeout = 10 * time.Second
	cfg.Metadata.Retry.Max = 1
	cfg.Consumer.Return.Errors = true
	cfg.Producer.Return.Successes = true
	cfg.Producer.Return.Errors = true
	cfg.Producer.RequiredAcks = sarama.WaitForLocal

	protocol := strings.ToUpper(strings.TrimSpace(conn.SecurityProtocol))
	if protocol == "" {
		protocol = "PLAINTEXT"
	}

	switch protocol {
	case "PLAINTEXT":
		// default
	case "SASL_PLAINTEXT", "SASL_SSL":
		cfg.Net.SASL.Enable = true
		cfg.Net.SASL.User = conn.SASLUsername
		cfg.Net.SASL.Password = conn.SASLPassword
		mech := strings.ToUpper(strings.TrimSpace(conn.SASLMechanism))
		switch mech {
		case "", "PLAIN":
			cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		case "SCRAM-SHA-256":
			cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			cfg.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &XDGSCRAMClient{HashGeneratorFcn: SHA256}
			}
		case "SCRAM-SHA-512":
			cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			cfg.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &XDGSCRAMClient{HashGeneratorFcn: SHA512}
			}
		default:
			return nil, fmt.Errorf("unsupported SASL mechanism: %s", conn.SASLMechanism)
		}
		if protocol == "SASL_SSL" {
			cfg.Net.TLS.Enable = true
		}
	case "SSL":
		cfg.Net.TLS.Enable = true
	default:
		return nil, fmt.Errorf("unsupported security protocol: %s", conn.SecurityProtocol)
	}

	return cfg, nil
}

func brokerList(conn *model.KafkaConn) []string {
	parts := strings.Split(conn.Brokers, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func openAdmin(conn *model.KafkaConn) (sarama.ClusterAdmin, error) {
	brokers := brokerList(conn)
	if len(brokers) == 0 {
		return nil, fmt.Errorf("brokers empty")
	}
	cfg, err := buildConfig(conn)
	if err != nil {
		return nil, err
	}
	admin, err := sarama.NewClusterAdmin(brokers, cfg)
	if err != nil {
		return nil, err
	}
	return admin, nil
}

func openClient(conn *model.KafkaConn) (sarama.Client, error) {
	brokers := brokerList(conn)
	if len(brokers) == 0 {
		return nil, fmt.Errorf("brokers empty")
	}
	cfg, err := buildConfig(conn)
	if err != nil {
		return nil, err
	}
	return sarama.NewClient(brokers, cfg)
}

type PingResult struct {
	Available bool     `json:"available"`
	Brokers   []string `json:"brokers,omitempty"`
	Message   string   `json:"message,omitempty"`
}

func Ping(conn *model.KafkaConn) PingResult {
	client, err := openClient(conn)
	if err != nil {
		return PingResult{Available: false, Message: err.Error()}
	}
	defer client.Close()

	brokers := client.Brokers()
	addrs := make([]string, 0, len(brokers))
	ok := false
	for _, b := range brokers {
		addrs = append(addrs, b.Addr())
		if err := b.Open(client.Config()); err == nil {
			connected, _ := b.Connected()
			if connected {
				ok = true
			}
		}
	}
	if !ok && len(brokers) == 0 {
		return PingResult{Available: false, Message: "no brokers in metadata"}
	}
	// Controller presence is a stronger signal.
	if _, err := client.Controller(); err != nil {
		return PingResult{Available: false, Brokers: addrs, Message: err.Error()}
	}
	return PingResult{Available: true, Brokers: addrs, Message: "ok"}
}

type TopicInfo struct {
	Name              string `json:"name"`
	Partitions        int    `json:"partitions"`
	ReplicationFactor int    `json:"replicationFactor"`
	// Messages is the currently retained message count (sum of end-start per partition).
	Messages int64 `json:"messages"`
	// Total is the sum of log-end offsets across partitions (approx produced volume).
	Total int64 `json:"total"`
}

type TopicListQuery struct {
	Q        string
	Page     int
	PageSize int
}

type TopicListResult struct {
	Items    []TopicInfo `json:"items"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"pageSize"`
}

func ListTopics(conn *model.KafkaConn, query TopicListQuery) (*TopicListResult, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	q := strings.ToLower(strings.TrimSpace(query.Q))

	admin, err := openAdmin(conn)
	if err != nil {
		return nil, err
	}
	defer admin.Close()

	client, err := openClient(conn)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	meta, err := admin.ListTopics()
	if err != nil {
		return nil, err
	}

	type metaItem struct {
		name   string
		detail sarama.TopicDetail
	}
	filtered := make([]metaItem, 0, len(meta))
	for name, detail := range meta {
		if strings.HasPrefix(name, "_") {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(name), q) {
			continue
		}
		filtered = append(filtered, metaItem{name: name, detail: detail})
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].name < filtered[j].name
	})

	total := len(filtered)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	pageItems := filtered[start:end]

	items := make([]TopicInfo, 0, len(pageItems))
	for _, it := range pageItems {
		detail := it.detail
		rf := 0
		if len(detail.ReplicaAssignment) > 0 {
			for _, replicas := range detail.ReplicaAssignment {
				if len(replicas) > rf {
					rf = len(replicas)
				}
			}
		} else if detail.NumPartitions > 0 {
			rf = int(detail.ReplicationFactor)
		}
		if rf == 0 {
			rf = int(detail.ReplicationFactor)
		}
		messages, msgTotal := topicMessageStats(client, it.name)
		items = append(items, TopicInfo{
			Name:              it.name,
			Partitions:        int(detail.NumPartitions),
			ReplicationFactor: rf,
			Messages:          messages,
			Total:             msgTotal,
		})
	}

	return &TopicListResult{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func topicMessageStats(client sarama.Client, topic string) (messages, total int64) {
	parts, err := client.Partitions(topic)
	if err != nil {
		return 0, 0
	}
	for _, p := range parts {
		oldest, err1 := client.GetOffset(topic, p, sarama.OffsetOldest)
		newest, err2 := client.GetOffset(topic, p, sarama.OffsetNewest)
		if err1 != nil || err2 != nil {
			continue
		}
		if newest > 0 {
			total += newest
		}
		if newest > oldest {
			messages += newest - oldest
		}
	}
	return messages, total
}

func DeleteTopic(conn *model.KafkaConn, topic string) error {
	admin, err := openAdmin(conn)
	if err != nil {
		return err
	}
	defer admin.Close()
	return admin.DeleteTopic(topic)
}

type PartitionInfo struct {
	ID       int32   `json:"id"`
	Leader   int32   `json:"leader"`
	Replicas []int32 `json:"replicas"`
	ISR      []int32 `json:"isr"`
}

type BrokerInfo struct {
	ID   int32  `json:"id"`
	Addr string `json:"addr"`
	Rack string `json:"rack,omitempty"`
}

type GroupInfo struct {
	GroupID string   `json:"groupId"`
	State   string   `json:"state"`
	Members int      `json:"members"`
	Topics  []string `json:"topics,omitempty"`
}

type TopicDetail struct {
	Name              string          `json:"name"`
	Partitions        []PartitionInfo `json:"partitions"`
	Brokers           []BrokerInfo    `json:"brokers"`
	ConsumerGroups    []GroupInfo     `json:"consumerGroups"`
	ReplicationFactor int             `json:"replicationFactor"`
}

func DescribeTopic(conn *model.KafkaConn, topic string) (*TopicDetail, error) {
	admin, err := openAdmin(conn)
	if err != nil {
		return nil, err
	}
	defer admin.Close()

	client, err := openClient(conn)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	metas, err := admin.DescribeTopics([]string{topic})
	if err != nil {
		return nil, err
	}
	if len(metas) == 0 || metas[0].Err != sarama.ErrNoError {
		if len(metas) > 0 {
			return nil, metas[0].Err
		}
		return nil, fmt.Errorf("topic not found")
	}
	tm := metas[0]

	parts := make([]PartitionInfo, 0, len(tm.Partitions))
	rf := 0
	for _, p := range tm.Partitions {
		if len(p.Replicas) > rf {
			rf = len(p.Replicas)
		}
		parts = append(parts, PartitionInfo{
			ID:       p.ID,
			Leader:   p.Leader,
			Replicas: append([]int32(nil), p.Replicas...),
			ISR:      append([]int32(nil), p.Isr...),
		})
	}

	brokers := make([]BrokerInfo, 0)
	for _, b := range client.Brokers() {
		brokers = append(brokers, BrokerInfo{
			ID:   b.ID(),
			Addr: b.Addr(),
			Rack: b.Rack(),
		})
	}

	groups, err := groupsForTopic(admin, topic)
	if err != nil {
		// Non-fatal: still return topic/broker info.
		groups = []GroupInfo{}
	}

	return &TopicDetail{
		Name:              topic,
		Partitions:        parts,
		Brokers:           brokers,
		ConsumerGroups:    groups,
		ReplicationFactor: rf,
	}, nil
}

type ConsumeMessage struct {
	Topic     string `json:"topic"`
	Partition int32  `json:"partition"`
	Offset    int64  `json:"offset"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Timestamp string `json:"timestamp"`
}

type ConsumeQuery struct {
	Topic     string
	Partition int32
	From      string
	Offset    int64
	Limit     int
	Since     time.Time
	Until     time.Time
}

func parseConsumeTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q", s)
}

// ConsumeMessages pulls a limited number of messages (tool preview, not a long-lived consumer).
// partition < 0 means all partitions. from: earliest | latest | offset.
// Optional Since/Until use Kafka offset-for-time, then filter by message timestamp.
func ConsumeMessages(ctx context.Context, conn *model.KafkaConn, q ConsumeQuery) ([]ConsumeMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	topic := strings.TrimSpace(q.Topic)
	if topic == "" {
		return nil, fmt.Errorf("topic is required")
	}
	from := strings.ToLower(strings.TrimSpace(q.From))
	if from == "" {
		from = "latest"
	}
	if from != "earliest" && from != "latest" && from != "offset" {
		return nil, fmt.Errorf("from must be earliest, latest, or offset")
	}
	if !q.Since.IsZero() && !q.Until.IsZero() && !q.Since.Before(q.Until) {
		return nil, fmt.Errorf("since must be before until")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	q.Topic = topic
	q.From = from
	q.Limit = limit

	client, err := openClient(conn)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	parts, err := client.Partitions(topic)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return []ConsumeMessage{}, nil
	}

	targets := parts
	if q.Partition >= 0 {
		found := false
		for _, p := range parts {
			if p == q.Partition {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("partition %d not found", q.Partition)
		}
		targets = []int32{q.Partition}
	}

	cfg := client.Config()
	cfg.Consumer.MaxWaitTime = 200 * time.Millisecond
	consumer, err := sarama.NewConsumerFromClient(client)
	if err != nil {
		return nil, err
	}
	defer consumer.Close()

	perPart := limit
	if q.Partition < 0 && len(targets) > 0 {
		perPart = (limit + len(targets) - 1) / len(targets)
		if perPart < 1 {
			perPart = 1
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		out      = make([]ConsumeMessage, 0, limit)
		firstErr error
		errOnce  sync.Once
	)
	for _, p := range targets {
		wg.Add(1)
		go func(p int32) {
			defer wg.Done()
			if ctx.Err() != nil {
				return
			}
			partQuery := q
			partQuery.Limit = perPart
			msgs, err := consumePartition(ctx, client, consumer, p, partQuery)
			if err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			if len(msgs) == 0 {
				return
			}
			mu.Lock()
			out = append(out, msgs...)
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return trimConsumeResult(out, from, limit), nil
}

type consumeRange struct {
	oldest   int64
	newest   int64
	from     string
	offset   int64
	limit    int
	sinceOff int64 // inclusive; -1 unset
	untilOff int64 // exclusive; -1 unset
}

// consumeWindow returns the start offset and how many retained messages to read.
// ok is false when the partition has nothing in the requested window.
func consumeWindow(oldest, newest, offset int64, from string, limit int) (start int64, count int, ok bool) {
	return resolveConsumeWindow(consumeRange{
		oldest:   oldest,
		newest:   newest,
		from:     from,
		offset:   offset,
		limit:    limit,
		sinceOff: -1,
		untilOff: -1,
	})
}

func resolveConsumeWindow(r consumeRange) (start int64, count int, ok bool) {
	if r.limit <= 0 || r.newest <= r.oldest {
		return 0, 0, false
	}
	lo := r.oldest
	hi := r.newest
	if r.sinceOff >= 0 && r.sinceOff > lo {
		lo = r.sinceOff
	}
	if r.untilOff >= 0 && r.untilOff < hi {
		hi = r.untilOff
	}
	if lo >= hi {
		return 0, 0, false
	}
	switch r.from {
	case "earliest":
		start = lo
	case "offset":
		start = r.offset
		if start < lo {
			start = lo
		}
		if start >= hi {
			return 0, 0, false
		}
	default: // latest: last N messages in [lo, hi)
		start = hi - int64(r.limit)
		if start < lo {
			start = lo
		}
	}
	available := hi - start
	if available <= 0 {
		return start, 0, false
	}
	count = r.limit
	if available < int64(count) {
		count = int(available)
	}
	return start, count, true
}

func trimConsumeResult(out []ConsumeMessage, from string, limit int) []ConsumeMessage {
	if len(out) == 0 {
		if out == nil {
			return []ConsumeMessage{}
		}
		return out
	}
	switch from {
	case "latest":
		sort.Slice(out, func(i, j int) bool {
			if out[i].Timestamp != out[j].Timestamp {
				return out[i].Timestamp > out[j].Timestamp
			}
			if out[i].Partition != out[j].Partition {
				return out[i].Partition < out[j].Partition
			}
			return out[i].Offset > out[j].Offset
		})
	default:
		sort.Slice(out, func(i, j int) bool {
			if out[i].Partition != out[j].Partition {
				return out[i].Partition < out[j].Partition
			}
			return out[i].Offset < out[j].Offset
		})
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func consumePartition(
	ctx context.Context,
	client sarama.Client,
	consumer sarama.Consumer,
	partition int32,
	q ConsumeQuery,
) ([]ConsumeMessage, error) {
	oldest, err := client.GetOffset(q.Topic, partition, sarama.OffsetOldest)
	if err != nil {
		return nil, err
	}
	newest, err := client.GetOffset(q.Topic, partition, sarama.OffsetNewest)
	if err != nil {
		return nil, err
	}
	rng := consumeRange{
		oldest:   oldest,
		newest:   newest,
		from:     q.From,
		offset:   q.Offset,
		limit:    q.Limit,
		sinceOff: -1,
		untilOff: -1,
	}
	if !q.Since.IsZero() {
		off, err := client.GetOffset(q.Topic, partition, q.Since.UnixMilli())
		if err != nil {
			return nil, err
		}
		if off < 0 {
			return []ConsumeMessage{}, nil
		}
		rng.sinceOff = off
	}
	if !q.Until.IsZero() {
		off, err := client.GetOffset(q.Topic, partition, q.Until.UnixMilli())
		if err != nil {
			return nil, err
		}
		if off < 0 {
			rng.untilOff = newest
		} else {
			rng.untilOff = off
		}
	}
	start, n, ok := resolveConsumeWindow(rng)
	if !ok {
		return []ConsumeMessage{}, nil
	}

	pc, err := consumer.ConsumePartition(q.Topic, partition, start)
	if err != nil {
		return nil, err
	}
	defer pc.Close()

	timer := time.NewTimer(consumeWaitTimeout)
	defer timer.Stop()

	out := make([]ConsumeMessage, 0, n)
	read := 0
	for len(out) < n && read < n {
		select {
		case <-ctx.Done():
			return out, nil
		case msg, ok := <-pc.Messages():
			if !ok {
				return out, nil
			}
			read++
			if !inConsumeTimeRange(msg.Timestamp, q.Since, q.Until) {
				if !q.Until.IsZero() && !msg.Timestamp.IsZero() && !msg.Timestamp.Before(q.Until) {
					return out, nil
				}
				continue
			}
			out = append(out, ConsumeMessage{
				Topic:     msg.Topic,
				Partition: msg.Partition,
				Offset:    msg.Offset,
				Key:       string(msg.Key),
				Value:     string(msg.Value),
				Timestamp: msg.Timestamp.Local().Format("2006-01-02 15:04:05"),
			})
		case err, ok := <-pc.Errors():
			if ok && err != nil {
				return out, err.Err
			}
			return out, nil
		case <-timer.C:
			return out, nil
		}
	}
	return out, nil
}

func inConsumeTimeRange(ts, since, until time.Time) bool {
	if ts.IsZero() {
		return true
	}
	if !since.IsZero() && ts.Before(since) {
		return false
	}
	if !until.IsZero() && !ts.Before(until) {
		return false
	}
	return true
}

type ProduceResult struct {
	Topic     string `json:"topic"`
	Partition int32  `json:"partition"`
	Offset    int64  `json:"offset"`
}

// ProduceMessage sends one message to the topic. partition < 0 lets Kafka choose.
func ProduceMessage(conn *model.KafkaConn, topic, key, value string, partition int32) (*ProduceResult, error) {
	if topic == "" {
		return nil, fmt.Errorf("topic is required")
	}
	brokers := brokerList(conn)
	if len(brokers) == 0 {
		return nil, fmt.Errorf("brokers empty")
	}
	cfg, err := buildConfig(conn)
	if err != nil {
		return nil, err
	}
	if partition >= 0 {
		cfg.Producer.Partitioner = sarama.NewManualPartitioner
	}

	producer, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, err
	}
	defer producer.Close()

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.StringEncoder(value),
	}
	if key != "" {
		msg.Key = sarama.StringEncoder(key)
	}
	if partition >= 0 {
		msg.Partition = partition
	}

	part, off, err := producer.SendMessage(msg)
	if err != nil {
		return nil, err
	}
	return &ProduceResult{Topic: topic, Partition: part, Offset: off}, nil
}

func groupsForTopic(admin sarama.ClusterAdmin, topic string) ([]GroupInfo, error) {
	listed, err := admin.ListConsumerGroups()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(listed))
	for id := range listed {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []GroupInfo{}, nil
	}

	descs, err := admin.DescribeConsumerGroups(ids)
	if err != nil {
		return nil, err
	}

	out := make([]GroupInfo, 0)
	for _, d := range descs {
		topics := map[string]struct{}{}
		for _, m := range d.Members {
			meta, err := m.GetMemberMetadata()
			if err != nil {
				continue
			}
			for _, t := range meta.Topics {
				topics[t] = struct{}{}
			}
			assign, err := m.GetMemberAssignment()
			if err == nil && assign != nil {
				for t := range assign.Topics {
					topics[t] = struct{}{}
				}
			}
		}
		if _, ok := topics[topic]; !ok {
			continue
		}
		topicNames := make([]string, 0, len(topics))
		for t := range topics {
			topicNames = append(topicNames, t)
		}
		out = append(out, GroupInfo{
			GroupID: d.GroupId,
			State:   d.State,
			Members: len(d.Members),
			Topics:  topicNames,
		})
	}
	return out, nil
}

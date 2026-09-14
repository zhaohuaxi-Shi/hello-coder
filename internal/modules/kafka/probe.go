package kafka

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
	"github.com/hello-coder/hello-coder/internal/model"
)

const probeTimeout = 500 * time.Millisecond

type connStatusDTO struct {
	Total    int `json:"total"`
	Abnormal int `json:"abnormal"`
}

func buildProbeConfig(conn *model.KafkaConn) (*sarama.Config, error) {
	cfg, err := buildConfig(conn)
	if err != nil {
		return nil, err
	}
	cfg.Net.DialTimeout = probeTimeout
	cfg.Net.ReadTimeout = probeTimeout
	cfg.Net.WriteTimeout = probeTimeout
	cfg.Metadata.Timeout = probeTimeout
	cfg.Metadata.Retry.Max = 0
	cfg.Admin.Timeout = probeTimeout
	return cfg, nil
}

// Probe is the fastest protocol-level Kafka check: open the first reachable
// broker (TCP + optional TLS/SASL) without fetching cluster metadata.
func Probe(conn *model.KafkaConn) bool {
	brokers := brokerList(conn)
	if len(brokers) == 0 {
		return false
	}
	cfg, err := buildProbeConfig(conn)
	if err != nil {
		return false
	}

	done := make(chan struct{})
	var ok atomic.Bool
	var once sync.Once
	var left atomic.Int32
	left.Store(int32(len(brokers)))
	finish := func(success bool) {
		if success {
			ok.Store(true)
		}
		once.Do(func() { close(done) })
	}
	for _, addr := range brokers {
		go func(addr string) {
			defer func() {
				if left.Add(-1) == 0 {
					finish(false)
				}
			}()
			b := sarama.NewBroker(addr)
			if err := b.Open(cfg); err != nil {
				return
			}
			defer func() { _ = b.Close() }()
			connected, err := b.Connected()
			if err == nil && connected {
				finish(true)
			}
		}(addr)
	}

	timer := time.NewTimer(probeTimeout)
	defer timer.Stop()
	select {
	case <-done:
		return ok.Load()
	case <-timer.C:
		return false
	}
}

func summarizeConns(rows []model.KafkaConn, open func(*model.KafkaConn) (*model.KafkaConn, error)) connStatusDTO {
	var abnormal atomic.Int32
	var wg sync.WaitGroup
	for i := range rows {
		wg.Add(1)
		go func(row *model.KafkaConn) {
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

package es

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/hello-coder/hello-coder/internal/model"
)

const probeTimeout = 500 * time.Millisecond

type connStatusDTO struct {
	Total    int `json:"total"`
	Abnormal int `json:"abnormal"`
}

func newProbeClient(conn *model.EsConn) (*elasticsearch.Client, error) {
	addrs := addressList(conn)
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses configured")
	}
	cfg := elasticsearch.Config{
		Addresses: addrs,
		Transport: &http.Transport{
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // local ops tool
			DialContext:           (&net.Dialer{Timeout: probeTimeout}).DialContext,
			TLSHandshakeTimeout:   probeTimeout,
			ResponseHeaderTimeout: probeTimeout,
			DisableKeepAlives:     true,
		},
	}
	if conn.Username != "" {
		cfg.Username = conn.Username
		cfg.Password = conn.Password
	}
	return elasticsearch.NewClient(cfg)
}

// Probe uses Elasticsearch HEAD / — the cheapest cluster liveness check.
func Probe(conn *model.EsConn) bool {
	client, err := newProbeClient(conn)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	res, err := client.Ping(client.Ping.WithContext(ctx))
	if err != nil {
		return false
	}
	if res.Body != nil {
		defer res.Body.Close()
	}
	return !res.IsError()
}

func summarizeConns(rows []model.EsConn, open func(*model.EsConn) (*model.EsConn, error)) connStatusDTO {
	var abnormal atomic.Int32
	var wg sync.WaitGroup
	for i := range rows {
		wg.Add(1)
		go func(row *model.EsConn) {
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

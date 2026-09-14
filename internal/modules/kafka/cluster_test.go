package kafka

import "testing"

func TestSplitBrokerAddr(t *testing.T) {
	cases := []struct {
		addr, host, port string
	}{
		{"kafka.local:9092", "kafka.local", "9092"},
		{"127.0.0.1:9093", "127.0.0.1", "9093"},
		{"[::1]:9092", "::1", "9092"},
		{"plain-host", "plain-host", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		host, port := splitBrokerAddr(c.addr)
		if host != c.host || port != c.port {
			t.Fatalf("splitBrokerAddr(%q) = (%q, %q), want (%q, %q)", c.addr, host, port, c.host, c.port)
		}
	}
}

func TestIsInternalTopic(t *testing.T) {
	if !isInternalTopic("__consumer_offsets", false) {
		t.Fatal("expected __consumer_offsets to be internal")
	}
	if !isInternalTopic("user-events", true) {
		t.Fatal("expected flagged topic to be internal")
	}
	if isInternalTopic("orders", false) {
		t.Fatal("orders should be user-facing")
	}
}

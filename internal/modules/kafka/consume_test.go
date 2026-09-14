package kafka

import (
	"testing"
	"time"
)

func TestConsumeWindow(t *testing.T) {
	cases := []struct {
		name           string
		oldest, newest int64
		offset         int64
		from           string
		limit          int
		wantStart      int64
		wantCount      int
		wantOK         bool
	}{
		{name: "empty partition", oldest: 10, newest: 10, from: "latest", limit: 20, wantOK: false},
		{name: "latest fewer than limit", oldest: 0, newest: 3, from: "latest", limit: 20, wantStart: 0, wantCount: 3, wantOK: true},
		{name: "latest last N", oldest: 0, newest: 100, from: "latest", limit: 20, wantStart: 80, wantCount: 20, wantOK: true},
		{name: "earliest capped by available", oldest: 5, newest: 12, from: "earliest", limit: 20, wantStart: 5, wantCount: 7, wantOK: true},
		{name: "offset past end", oldest: 0, newest: 10, offset: 10, from: "offset", limit: 5, wantOK: false},
		{name: "offset before oldest", oldest: 8, newest: 20, offset: 1, from: "offset", limit: 5, wantStart: 8, wantCount: 5, wantOK: true},
		{name: "offset near end", oldest: 0, newest: 10, offset: 8, from: "offset", limit: 20, wantStart: 8, wantCount: 2, wantOK: true},
		{name: "zero limit", oldest: 0, newest: 10, from: "latest", limit: 0, wantOK: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, count, ok := consumeWindow(c.oldest, c.newest, c.offset, c.from, c.limit)
			if ok != c.wantOK || start != c.wantStart || count != c.wantCount {
				t.Fatalf("consumeWindow() = (%d, %d, %v), want (%d, %d, %v)", start, count, ok, c.wantStart, c.wantCount, c.wantOK)
			}
		})
	}
}

func TestTrimConsumeResult(t *testing.T) {
	in := []ConsumeMessage{
		{Partition: 1, Offset: 2, Timestamp: "2026-01-01 10:00:00"},
		{Partition: 0, Offset: 9, Timestamp: "2026-01-01 12:00:00"},
		{Partition: 0, Offset: 1, Timestamp: "2026-01-01 11:00:00"},
	}
	latest := trimConsumeResult(append([]ConsumeMessage(nil), in...), "latest", 2)
	if len(latest) != 2 || latest[0].Offset != 9 || latest[1].Offset != 1 {
		t.Fatalf("latest trim = %+v", latest)
	}
	earliest := trimConsumeResult(append([]ConsumeMessage(nil), in...), "earliest", 10)
	if len(earliest) != 3 || earliest[0].Partition != 0 || earliest[0].Offset != 1 || earliest[2].Partition != 1 {
		t.Fatalf("earliest sort = %+v", earliest)
	}
}

func TestResolveConsumeWindowWithTime(t *testing.T) {
	cases := []struct {
		name      string
		r         consumeRange
		wantStart int64
		wantCount int
		wantOK    bool
	}{
		{
			name:      "since lifts start for earliest",
			r:         consumeRange{oldest: 0, newest: 100, from: "earliest", limit: 20, sinceOff: 40, untilOff: -1},
			wantStart: 40, wantCount: 20, wantOK: true,
		},
		{
			name:      "until caps latest window",
			r:         consumeRange{oldest: 0, newest: 100, from: "latest", limit: 10, sinceOff: -1, untilOff: 50},
			wantStart: 40, wantCount: 10, wantOK: true,
		},
		{
			name:   "since after until empty",
			r:      consumeRange{oldest: 0, newest: 100, from: "latest", limit: 20, sinceOff: 80, untilOff: 60},
			wantOK: false,
		},
		{
			name:      "latest inside since-until",
			r:         consumeRange{oldest: 0, newest: 100, from: "latest", limit: 5, sinceOff: 10, untilOff: 30},
			wantStart: 25, wantCount: 5, wantOK: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, count, ok := resolveConsumeWindow(c.r)
			if ok != c.wantOK || start != c.wantStart || count != c.wantCount {
				t.Fatalf("resolveConsumeWindow() = (%d, %d, %v), want (%d, %d, %v)", start, count, ok, c.wantStart, c.wantCount, c.wantOK)
			}
		})
	}
}

func TestParseConsumeTime(t *testing.T) {
	got, err := parseConsumeTime("2026-09-10T08:27:00")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 10, 8, 27, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if _, err := parseConsumeTime(""); err != nil {
		t.Fatal(err)
	}
	if _, err := parseConsumeTime("not-a-time"); err == nil {
		t.Fatal("expected invalid time")
	}
}

func TestInConsumeTimeRange(t *testing.T) {
	since := time.Date(2026, 9, 10, 8, 0, 0, 0, time.Local)
	until := time.Date(2026, 9, 10, 9, 0, 0, 0, time.Local)
	inside := time.Date(2026, 9, 10, 8, 30, 0, 0, time.Local)
	if !inConsumeTimeRange(inside, since, until) {
		t.Fatal("expected inside")
	}
	if inConsumeTimeRange(until, since, until) {
		t.Fatal("until is exclusive")
	}
	if inConsumeTimeRange(since.Add(-time.Second), since, until) {
		t.Fatal("before since")
	}
	if !inConsumeTimeRange(time.Time{}, since, until) {
		t.Fatal("zero timestamp should pass")
	}
}

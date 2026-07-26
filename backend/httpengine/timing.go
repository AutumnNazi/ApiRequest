package httpengine

import (
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"

	"apirequest/backend/model"
)

// traceTimer 用 httptrace.ClientTrace 回调打点采集分阶段计时。
// 回调可能来自不同 goroutine，用互斥锁保护。
type traceTimer struct {
	mu sync.Mutex

	dnsStart, dnsDone         time.Time
	connectStart, connectDone time.Time
	tlsStart, tlsDone         time.Time
	firstByte                 time.Time
}

func newTraceTimer() *traceTimer { return &traceTimer{} }

func (t *traceTimer) clientTrace() *httptrace.ClientTrace {
	stamp := func(dst *time.Time) func() {
		return func() {
			t.mu.Lock()
			*dst = time.Now()
			t.mu.Unlock()
		}
	}
	return &httptrace.ClientTrace{
		DNSStart:     func(httptrace.DNSStartInfo) { stamp(&t.dnsStart)() },
		DNSDone:      func(httptrace.DNSDoneInfo) { stamp(&t.dnsDone)() },
		ConnectStart: func(string, string) { stamp(&t.connectStart)() },
		ConnectDone:  func(_, _ string, _ error) { stamp(&t.connectDone)() },
		TLSHandshakeStart: func() { stamp(&t.tlsStart)() },
		TLSHandshakeDone: func(tls.ConnectionState, error) { stamp(&t.tlsDone)() },
		GotFirstResponseByte: func() { stamp(&t.firstByte)() },
	}
}

// timing 汇总为 model.Timing。连接复用时 DNS/connect/TLS 各段为 0。
func (t *traceTimer) timing(start, end time.Time) model.Timing {
	t.mu.Lock()
	defer t.mu.Unlock()

	ms := func(a, b time.Time) float64 {
		if a.IsZero() || b.IsZero() || b.Before(a) {
			return 0
		}
		return float64(b.Sub(a).Microseconds()) / 1000
	}
	tm := model.Timing{
		DnsMs:     ms(t.dnsStart, t.dnsDone),
		ConnectMs: ms(t.connectStart, t.connectDone),
		TlsMs:     ms(t.tlsStart, t.tlsDone),
		TtfbMs:    ms(start, t.firstByte),
		TotalMs:   ms(start, end),
	}
	if !t.firstByte.IsZero() {
		tm.DownloadMs = ms(t.firstByte, end)
	}
	return tm
}

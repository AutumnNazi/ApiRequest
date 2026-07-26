package protocol

import (
	"bufio"
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"apirequest/backend/model"
)

func base64Encode(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

// sseSession SSE 会话（docs/protocols.md §3）：GET 长连接，
// text/event-stream 增量解析 event:/data:/id:/retry: 字段
type sseSession struct {
	cancel context.CancelFunc
}

func openSSE(id string, cfg SessionConfig, emit EmitFunc) (Session, error) {
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", cfg.Url, nil)
	if err != nil {
		cancel()
		return nil, model.WrapError(model.KindValidation, err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	for _, h := range cfg.Headers {
		if h.Enabled && h.Key != "" {
			req.Header.Set(h.Key, h.Value)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		return nil, model.WrapError(model.KindNetwork, err)
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		cancel()
		return nil, model.NewError(model.KindNetwork, "SSE endpoint returned "+resp.Status)
	}

	emit(InboundMsg{
		SessionId: id, Protocol: "sse", Direction: "system",
		Kind: "open", Data: cfg.Url, Ts: time.Now().UnixMilli(),
	})

	// 读循环：按 SSE 规范逐行解析，空行 = 事件结束
	go func() {
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 4<<20)
		var event string
		var dataLines []string
		flush := func() {
			if len(dataLines) == 0 {
				return
			}
			emit(InboundMsg{
				SessionId: id, Protocol: "sse", Direction: "in",
				Kind: "event", Event: event, Data: strings.Join(dataLines, "\n"),
				Ts: time.Now().UnixMilli(),
			})
			event = ""
			dataLines = nil
		}
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case line == "":
				flush()
			case strings.HasPrefix(line, "event:"):
				event = strings.TrimSpace(line[6:])
			case strings.HasPrefix(line, "data:"):
				dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			case strings.HasPrefix(line, ":"):
				// 注释行，忽略
			}
			// id:/retry: 暂不处理（自动续订留待后续）
		}
		flush()
		detail := "stream ended"
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			detail = err.Error()
		}
		emit(InboundMsg{
			SessionId: id, Protocol: "sse", Direction: "system",
			Kind: "close", Data: detail, Ts: time.Now().UnixMilli(),
		})
	}()

	return &sseSession{cancel: cancel}, nil
}

// Send SSE 是单向流，不支持发送
func (s *sseSession) Send(string) error {
	return model.NewError(model.KindValidation, "SSE is receive-only")
}

func (s *sseSession) Close() error {
	s.cancel()
	return nil
}

func init() { registerOpener("sse", openSSE) }

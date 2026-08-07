package protocol

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"apirequest/backend/model"
)

func base64Encode(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

const (
	defaultSSERetry = 3 * time.Second
	maxSSERetry     = 30 * time.Second
)

type sseSession struct {
	cancel context.CancelFunc
	done   chan struct{}
	client *http.Client
}

func openSSE(id string, cfg SessionConfig, emit EmitFunc, client *http.Client) (Session, error) {
	ctx, cancel := context.WithCancel(context.Background())
	response, err := connectSSE(ctx, cfg, "", client)
	if err != nil {
		cancel()
		return nil, err
	}
	session := &sseSession{cancel: cancel, done: make(chan struct{}), client: client}
	emit(systemSSEMessage(id, "open", cfg.Url))
	go session.run(ctx, id, cfg, emit, response)
	return session, nil
}

func connectSSE(ctx context.Context, cfg SessionConfig, lastEventID string, client *http.Client) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Url, nil)
	if err != nil {
		return nil, model.WrapError(model.KindValidation, err)
	}
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Cache-Control", "no-cache")
	for _, header := range cfg.Headers {
		if header.Enabled && header.Key != "" {
			request.Header.Set(header.Key, header.Value)
		}
	}
	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, model.NewError(model.KindNetwork, "SSE endpoint returned "+response.Status)
	}
	return response, nil
}

func (s *sseSession) run(
	ctx context.Context,
	id string,
	cfg SessionConfig,
	emit EmitFunc,
	response *http.Response,
) {
	defer close(s.done)
	lastEventID := ""
	retry := defaultSSERetry
	connectFailures := 0

	for {
		err := consumeSSE(ctx, response, id, emit, &lastEventID, &retry)
		response.Body.Close()
		if ctx.Err() != nil {
			emit(systemSSEMessage(id, "close", "closed by user"))
			return
		}
		detail := "stream ended"
		if err != nil {
			detail = err.Error()
		}
		emit(systemSSEMessage(id, "reconnect", fmt.Sprintf("%s; retrying in %s", detail, retry)))
		if !waitForSSE(ctx, retry) {
			emit(systemSSEMessage(id, "close", "closed by user"))
			return
		}

		for {
			next, connectErr := connectSSE(ctx, cfg, lastEventID, s.client)
			if connectErr == nil {
				response = next
				connectFailures = 0
				emit(systemSSEMessage(id, "reconnect", "reconnected"))
				break
			}
			if ctx.Err() != nil {
				emit(systemSSEMessage(id, "close", "closed by user"))
				return
			}
			connectFailures++
			delay := sseBackoff(retry, connectFailures)
			emit(systemSSEMessage(id, "reconnect", fmt.Sprintf("reconnect failed: %v; retrying in %s", connectErr, delay)))
			if !waitForSSE(ctx, delay) {
				emit(systemSSEMessage(id, "close", "closed by user"))
				return
			}
		}
	}
}

func consumeSSE(
	ctx context.Context,
	response *http.Response,
	sessionID string,
	emit EmitFunc,
	lastEventID *string,
	retry *time.Duration,
) error {
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	eventType := ""
	eventID := *lastEventID
	var dataLines []string
	flush := func() {
		if len(dataLines) == 0 {
			eventType = ""
			return
		}
		*lastEventID = eventID
		emit(InboundMsg{
			SessionId: sessionID,
			Protocol:  "sse",
			Direction: "in",
			Kind:      "event",
			Event:     eventType,
			EventId:   eventID,
			Data:      strings.Join(dataLines, "\n"),
			Ts:        time.Now().UnixMilli(),
		})
		eventType = ""
		dataLines = nil
	}
	firstLine := true
	for scanner.Scan() {
		line := scanner.Text()
		if firstLine {
			line = strings.TrimPrefix(line, "\ufeff")
			firstLine = false
		}
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			value = ""
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			eventType = value
		case "data":
			dataLines = append(dataLines, value)
		case "id":
			if !strings.ContainsRune(value, '\x00') {
				eventID = value
			}
		case "retry":
			milliseconds, err := strconv.ParseInt(value, 10, 64)
			if err == nil && milliseconds >= 0 {
				*retry = clampSSERetry(time.Duration(milliseconds) * time.Millisecond)
			}
		}
	}
	flush()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func clampSSERetry(retry time.Duration) time.Duration {
	if retry < 100*time.Millisecond {
		return 100 * time.Millisecond
	}
	if retry > maxSSERetry {
		return maxSSERetry
	}
	return retry
}

func sseBackoff(base time.Duration, failures int) time.Duration {
	delay := clampSSERetry(base)
	for i := 1; i < failures && delay < maxSSERetry; i++ {
		delay *= 2
		if delay > maxSSERetry {
			return maxSSERetry
		}
	}
	return delay
}

func waitForSSE(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func systemSSEMessage(sessionID, kind, data string) InboundMsg {
	return InboundMsg{
		SessionId: sessionID, Protocol: "sse", Direction: "system",
		Kind: kind, Data: data, Ts: time.Now().UnixMilli(),
	}
}

func (s *sseSession) Send(string) error {
	return model.NewError(model.KindValidation, "SSE is receive-only")
}

func (s *sseSession) Close() error {
	s.cancel()
	return nil
}

func init() { registerOpener("sse", openSSE) }

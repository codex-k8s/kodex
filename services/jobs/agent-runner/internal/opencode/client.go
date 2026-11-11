// Package opencode implements the pinned upstream HTTP boundary, not an agent loop or an authority source.
// Package opencode реализует закреплённую HTTP-границу upstream, а не агентный цикл или источник полномочий.
package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	// maximumResponseBytes bounds one paginated response, never the lifetime transcript.
	// maximumResponseBytes ограничивает один постраничный ответ, а не всю историю сессии.
	maximumResponseBytes = 16 << 20
	// maximumEventBytes bounds one event while a caller-owned context bounds stream lifetime.
	// maximumEventBytes ограничивает одно событие; срок потока задаёт контекст вызывающего.
	maximumEventBytes = 1 << 20
)

var (
	// ErrProtocol rejects malformed native data without exposing its content or credentials.
	// ErrProtocol отклоняет неверные native-данные без раскрытия содержимого или credentials.
	ErrProtocol = errors.New("OpenCode API protocol is invalid")
	// ErrUnavailable preserves an uncertain transport outcome without permitting write replay.
	// ErrUnavailable сохраняет неопределённый исход транспорта, не разрешая повтор записи.
	ErrUnavailable = errors.New("OpenCode API is unavailable; reconcile before repeating a write")
	// ErrAuthentication classifies an upstream credential rejection without retaining its raw body.
	// ErrAuthentication обозначает upstream credential rejection без сохранения raw body.
	ErrAuthentication = errors.New("OpenCode model provider authentication was rejected")
	// nativeSessionPattern accepts only bounded upstream session identifiers, never paths or URLs.
	// nativeSessionPattern принимает ограниченные upstream session ID, а не пути или URL.
	nativeSessionPattern = regexp.MustCompile(`^ses_[A-Za-z0-9]{1,100}$`)
	// nativeMessagePattern accepts the upstream message alphabet without deriving execution authority.
	// nativeMessagePattern принимает алфавит upstream message ID, не выводя из него полномочия.
	nativeMessagePattern = regexp.MustCompile(`^msg_[A-Za-z0-9]{1,100}$`)
	// nativePartPattern excludes path-like or ambiguous part identities in official checkpoints.
	// nativePartPattern исключает похожие на paths или неоднозначные part ID в официальных checkpoint.
	nativePartPattern = regexp.MustCompile(`^prt_[A-Za-z0-9]{1,100}$`)
)

// APIError exposes only the native HTTP status; response bodies are private and are never errors.
// APIError раскрывает только HTTP-статус upstream; приватное тело ответа в ошибку не попадает.
type APIError struct{ Status int }

// Error returns fixed diagnostic text; callers inspect Status without parsing provider text.
// Error возвращает фиксированный текст; вызывающий проверяет Status без разбора текста провайдера.
func (failure *APIError) Error() string { return "OpenCode API request rejected" }

// Client talks only to one private loopback server. Its methods do not authorize a Kodex session.
// Client обращается к одному private loopback server; его методы не авторизуют сессию Kodex.
type Client struct {
	origin   string
	username string
	password string
	http     *http.Client
}

// Session is the native identity projection; the owner separately binds it to exact Kodex scope.
// Session содержит native identity; владелец отдельно связывает её с точной областью Kodex.
type Session struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	Version   string `json:"version"`
}

// Model selects an already configured upstream provider and model without selecting credentials.
// Model выбирает настроенные provider и model upstream, но не назначает credentials.
type Model struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// TextPart carries approved new text. Arbitrary file URLs and native subtask parts are not exposed.
// TextPart переносит новый утверждённый текст; произвольные file URL и native subtasks не доступны.
type TextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Prompt contains only the new message and current approved system materialization, not replayed history.
// Prompt содержит новое сообщение и текущую system-материализацию, а не повтор прежней истории.
type Prompt struct {
	MessageID string     `json:"messageID,omitempty"`
	Model     Model      `json:"model"`
	System    string     `json:"system,omitempty"`
	Parts     []TextPart `json:"parts"`
	NoReply   bool       `json:"noReply,omitempty"`
}

// Message retains native fields for the qualified consumer without interpreting summaries as success.
// Message сохраняет native-поля для проверенного consumer, не трактуя summary как успех.
type Message struct {
	Info  json.RawMessage   `json:"info"`
	Parts []json.RawMessage `json:"parts"`
}

// Event is an upstream observation. Neither an event type nor its text grants tool authority.
// Event содержит наблюдение upstream; ни type, ни текст события не дают полномочий инструментам.
type Event struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

// NewClient closes redirects, environment proxies and non-loopback endpoints before retaining local auth.
// NewClient закрывает redirects, environment proxy и внешние endpoints до сохранения локальной авторизации.
func NewClient(origin, username, password string) (*Client, error) {
	endpoint, err := url.Parse(origin)
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" ||
		endpoint.User != nil || endpoint.Path != "" || endpoint.RawPath != "" || endpoint.RawQuery != "" ||
		endpoint.ForceQuery || endpoint.Fragment != "" || username == "" || strings.ContainsAny(username, ":\r\n") ||
		len(username) > 128 || len(password) < 24 || len(password) > 256 || strings.ContainsAny(password, "\r\n") {
		return nil, ErrProtocol
	}
	port, err := strconv.Atoi(endpoint.Port())
	if err != nil || port < 1024 || port > 65535 || endpoint.Host != net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) {
		return nil, ErrProtocol
	}
	transport := &http.Transport{
		DialContext:  (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		MaxIdleConns: 4, MaxIdleConnsPerHost: 4, MaxConnsPerHost: 4, IdleConnTimeout: 30 * time.Second,
	}
	return &Client{origin: origin, username: username, password: password, http: &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// Close releases idle connections; process/writer join remains the owning runtime's responsibility.
// Close освобождает idle connections; ожидание process/writer остаётся обязанностью владельца runtime.
func (client *Client) Close() { client.http.CloseIdleConnections() }

// Health requires the exact qualified executable/API version before session operations begin.
// Health требует точную проверенную версию executable/API до операций с сессией.
func (client *Client) Health(ctx context.Context) error {
	var result struct {
		Healthy bool   `json:"healthy"`
		Version string `json:"version"`
	}
	if err := client.call(ctx, http.MethodGet, "/global/health", nil, &result); err != nil {
		return err
	}
	if !result.Healthy || result.Version != runtimecontract.OpenCodeVersion {
		return ErrProtocol
	}
	return nil
}

// CreateSession creates native state once. An uncertain response requires discovery, not blind create retry.
// CreateSession создаёт native state один раз; при неизвестном ответе требуется сверка, а не повтор create.
func (client *Client) CreateSession(ctx context.Context, title string) (Session, error) {
	if strings.TrimSpace(title) == "" || len(title) > 512 || !utf8.ValidString(title) || strings.ContainsRune(title, 0) {
		return Session{}, ErrProtocol
	}
	var result Session
	if err := client.call(ctx, http.MethodPost, "/session", struct {
		Title string `json:"title"`
	}{title}, &result); err != nil {
		return Session{}, err
	}
	if !nativeSessionPattern.MatchString(result.ID) {
		return Session{}, ErrProtocol
	}
	return result, nil
}

// Session reads an exact native ID and rejects a mismatched response before owner binding is used.
// Session читает точный native ID и отклоняет чужой ответ до использования owner binding.
func (client *Client) Session(ctx context.Context, id string) (Session, error) {
	if !nativeSessionPattern.MatchString(id) {
		return Session{}, ErrProtocol
	}
	var result Session
	if err := client.call(ctx, http.MethodGet, "/session/"+id, nil, &result); err != nil {
		return Session{}, err
	}
	if result.ID != id {
		return Session{}, ErrProtocol
	}
	return result, nil
}

// SendPrompt invokes the native loop once. The owner must reconcile MessageID before any retry.
// SendPrompt однократно вызывает native loop; до повтора владелец обязан сверить MessageID.
func (client *Client) SendPrompt(ctx context.Context, id string, prompt Prompt) (Message, error) {
	if !nativeSessionPattern.MatchString(id) || !nativeMessagePattern.MatchString(prompt.MessageID) ||
		!runtimecontract.ValidModelProviderModelID(prompt.Model.ProviderID) ||
		!runtimecontract.ValidModelProviderModelID(prompt.Model.ModelID) || len(prompt.Parts) == 0 || len(prompt.Parts) > 128 ||
		len(prompt.System) > 1<<20 || !utf8.ValidString(prompt.System) || strings.ContainsRune(prompt.System, 0) {
		return Message{}, ErrProtocol
	}
	for _, part := range prompt.Parts {
		if part.Type != "text" || len(part.Text) > 1<<20 || !utf8.ValidString(part.Text) || strings.ContainsRune(part.Text, 0) {
			return Message{}, ErrProtocol
		}
	}
	var result Message
	if err := client.call(ctx, http.MethodPost, "/session/"+id+"/message", prompt, &result); err != nil {
		return Message{}, err
	}
	var info struct {
		SessionID string `json:"sessionID"`
		Role      string `json:"role"`
	}
	if json.Unmarshal(result.Info, &info) != nil || info.SessionID != id ||
		(prompt.NoReply && info.Role != "user") || (!prompt.NoReply && info.Role != "assistant") {
		return Message{}, ErrProtocol
	}
	return result, nil
}

// Abort requests native cancellation, not a durable checkpoint or proof that the process has stopped.
// Abort запрашивает native cancel, но не является checkpoint или доказательством остановки процесса.
func (client *Client) Abort(ctx context.Context, id string) error {
	if !nativeSessionPattern.MatchString(id) {
		return ErrProtocol
	}
	var acknowledged bool
	if err := client.call(ctx, http.MethodPost, "/session/"+id+"/abort", struct{}{}, &acknowledged); err != nil {
		return err
	}
	if !acknowledged {
		return ErrProtocol
	}
	return nil
}

// Messages reads one bounded page; callers keep pagination instead of accumulating the entire history.
// Messages читает ограниченную страницу; вызывающий использует pagination вместо накопления всей истории.
func (client *Client) Messages(ctx context.Context, id string, limit int, before string) ([]Message, error) {
	page, err := client.MessagePage(ctx, id, limit, before)
	return page.Messages, err
}

// MessagePage retains the opaque native pagination cursor, never substituting a message ID or following a Link URL.
// MessagePage сохраняет opaque native pagination cursor, не подставляя message ID и не следуя Link URL.
func (client *Client) MessagePage(ctx context.Context, id string, limit int, before string) (MessagePage, error) {
	if !nativeSessionPattern.MatchString(id) || limit < 1 || limit > 100 || before != "" && !nativeCursorPattern.MatchString(before) {
		return MessagePage{}, ErrProtocol
	}
	path := "/session/" + id + "/message?limit=" + strconv.Itoa(limit)
	if before != "" {
		path += "&before=" + url.QueryEscape(before)
	}
	var messages []Message
	headers, err := client.callHeaders(ctx, http.MethodGet, path, nil, &messages)
	if err != nil {
		return MessagePage{}, err
	}
	values := headers.Values("X-Next-Cursor")
	if len(messages) > limit || len(values) > 1 {
		return MessagePage{}, ErrProtocol
	}
	next := headers.Get("X-Next-Cursor")
	if next != "" && (!nativeCursorPattern.MatchString(next) || next == before || len(messages) == 0) {
		return MessagePage{}, ErrProtocol
	}
	for _, message := range messages {
		var info struct {
			SessionID string `json:"sessionID"`
		}
		if json.Unmarshal(message.Info, &info) != nil || info.SessionID != id {
			return MessagePage{}, ErrProtocol
		}
	}
	return MessagePage{Messages: messages, NextCursor: next}, nil
}

// Events applies bounded per-frame decoding and synchronous backpressure without a lifetime event ceiling.
// Events ограничивает каждый frame и применяет backpressure без предела количества событий за всю сессию.
func (client *Client) Events(ctx context.Context, consume func(Event) error) error {
	return client.events(ctx, consume, nil)
}

// events acknowledges the established SSE response before a caller submits the new native message.
// events подтверждает установленный SSE response до отправки нового native-сообщения вызывающим.
func (client *Client) events(ctx context.Context, consume func(Event) error, ready chan<- struct{}) error {
	if consume == nil {
		return ErrProtocol
	}
	response, err := client.request(ctx, http.MethodGet, "/event", nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if ready != nil {
		close(ready)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), maximumEventBytes)
	var frame bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if frame.Len() != 0 {
				var event Event
				if decodeNativeJSON(frame.Bytes(), &event) != nil || event.Type == "" {
					return ErrProtocol
				}
				if err := consume(event); err != nil {
					return err
				}
				frame.Reset()
			}
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			if frame.Len()+len(value)+1 > maximumEventBytes {
				return ErrProtocol
			}
			frame.WriteString(strings.TrimPrefix(value, " "))
			frame.WriteByte('\n')
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrUnavailable
}

// call decodes one duplicate-free native response and never includes raw errors in diagnostics.
// call разбирает один native-ответ без дубликатов и не включает сырые ошибки в диагностику.
func (client *Client) call(ctx context.Context, method, path string, input, output any) error {
	_, err := client.callHeaders(ctx, method, path, input, output)
	return err
}

// callHeaders decodes one bounded native response while retaining metadata required by native pagination.
// callHeaders разбирает один bounded native response и сохраняет metadata для native pagination.
func (client *Client) callHeaders(ctx context.Context, method, path string, input, output any) (http.Header, error) {
	response, err := client.request(ctx, method, path, input)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil || len(raw) > maximumResponseBytes || decodeNativeJSON(raw, output) != nil {
		return nil, ErrProtocol
	}
	return response.Header.Clone(), nil
}

// request performs exactly one local HTTP action; network failure never causes automatic mutation replay.
// request выполняет ровно одно локальное HTTP-действие; сбой сети не вызывает автоматический повтор записи.
func (client *Client) request(ctx context.Context, method, path string, input any) (*http.Response, error) {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil || len(body) > 2<<20 {
			return nil, ErrProtocol
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, client.origin+path, bytes.NewReader(body))
	if err != nil {
		return nil, ErrProtocol
	}
	request.SetBasicAuth(client.username, client.password)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := client.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, &APIError{Status: response.StatusCode}
	}
	return response, nil
}

// decodeNativeJSON reuses the shared duplicate/depth guard while accepting harmless upstream metadata.
// decodeNativeJSON использует общий guard дубликатов/глубины, допуская безвредные upstream metadata.
func decodeNativeJSON(raw []byte, target any) error {
	var shape any
	if !utf8.Valid(raw) || internalrpcauth.DecodeStrictJSON(raw, &shape) != nil || json.Unmarshal(raw, target) != nil {
		return ErrProtocol
	}
	return nil
}

// MessagePage carries a native page and its server-issued opaque continuation token without granting a new session scope.
// MessagePage содержит native page и server-issued opaque continuation token без выдачи новой session scope.
type MessagePage struct {
	Messages   []Message
	NextCursor string
}

// nativeCursorPattern bounds only the transport alphabet; the upstream server owns cursor semantics and session filtering.
// nativeCursorPattern ограничивает лишь transport alphabet; семантика cursor и session filtering принадлежат upstream server.
var nativeCursorPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`)

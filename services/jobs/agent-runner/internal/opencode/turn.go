package opencode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TurnResult contains bounded output and normalized measurements; it is not a checkpoint or owner receipt.
// TurnResult содержит ограниченный ответ и нормализованные измерения, но не checkpoint или owner receipt.
type TurnResult struct {
	FinalText   string
	Usage       runtimecontract.TokenUsage
	Compactions int
}

// TurnObservers holds optional synchronous sinks; a rejected observation stops the turn without inventing delivery.
// TurnObservers содержит необязательные синхронные sinks; отказ наблюдения останавливает turn без выдуманной доставки.
type TurnObservers struct {
	Progress    func(string) error
	Measurement func(runtimecontract.ModelMeasurement) error
}

// turnObserver projects upstream events into current-turn measurements and bounded commentary.
// Its maps retain unfinished messages and a bounded recent completion window, never the lifetime transcript or authority.
// turnObserver преобразует upstream events в измерения текущего turn и ограниченные commentary.
// Maps содержат незавершённые сообщения и ограниченное окно завершений, а не всю историю или authority.
type turnObserver struct {
	sessionID   string
	result      TurnResult
	active      map[string]*observedMessage
	allowTool   func(string) bool
	commentary  func(string, string) error
	completed   func(string) error
	progress    func(string) error
	measurement func(runtimecontract.ModelMeasurement) error
	retired     []string
}

// observedMessage keeps only current-message deduplication and summary classification.
// observedMessage хранит только дедупликацию текущего сообщения и признак summary.
type observedMessage struct {
	summary  bool
	complete bool
	measured bool
	parts    map[string]bool
}

// Execute submits exactly one new message to the native loop and observes its established SSE stream.
// It never drives tool steps, invokes a compactor, replays history or retries uncertain mutations.
// Execute отправляет ровно одно новое сообщение native loop и наблюдает установленный SSE stream.
// Не управляет tool steps, не вызывает compactor, не повторяет историю или uncertain mutations.
func (client *Client) Execute(ctx context.Context, sessionID string, prompt Prompt, allowTool func(string) bool, commentary func(string, string) error, observers ...TurnObservers) (TurnResult, error) {
	if allowTool == nil || commentary == nil || prompt.NoReply {
		return TurnResult{}, ErrProtocol
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	observer := turnObserver{sessionID: sessionID, active: map[string]*observedMessage{}, allowTool: allowTool, commentary: commentary}
	if len(observers) > 1 {
		return TurnResult{}, ErrProtocol
	}
	if len(observers) == 1 {
		observer.progress = observers[0].Progress
		observer.measurement = observers[0].Measurement
	}
	completed := make(chan string)
	observer.completed = func(id string) error {
		select {
		case completed <- id:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	ready := make(chan struct{})
	eventsDone := make(chan error, 1)
	go func() { eventsDone <- client.events(ctx, observer.consume, ready) }()
	select {
	case <-ready:
	case err := <-eventsDone:
		return TurnResult{}, err
	case <-ctx.Done():
		cancel()
		<-eventsDone
		return TurnResult{}, ctx.Err()
	}
	// The native POST returns the actual final assistant message, not an intermediate summary event.
	// Native POST возвращает настоящий final assistant message, а не промежуточное summary event.
	type response struct {
		message Message
		err     error
	}
	promptDone := make(chan response, 1)
	go func() {
		message, err := client.SendPrompt(ctx, sessionID, prompt)
		promptDone <- response{message, err}
	}()
	var received *response
	var failure error
	lastCompleted, finalID, text := "", "", ""
	streamJoined := false
	for received == nil || lastCompleted != finalID {
		select {
		case id := <-completed:
			lastCompleted = id
		case reply := <-promptDone:
			received = &reply
			if reply.err != nil {
				failure = reply.err
				break
			}
			text, failure = finalText(reply.message, sessionID)
			var info struct{ ID string }
			if json.Unmarshal(reply.message.Info, &info) != nil || !nativeMessagePattern.MatchString(info.ID) {
				failure = ErrProtocol
			}
			finalID = info.ID
		case err := <-eventsDone:
			failure = err
			streamJoined = true
		case <-ctx.Done():
			failure = ctx.Err()
		}
		if failure != nil {
			break
		}
	}
	cancel()
	if !streamJoined {
		<-eventsDone
	}
	if received == nil {
		<-promptDone
	}
	if failure != nil {
		return observer.result, failure
	}
	observer.result.FinalText = text
	return observer.result, nil
}

// consume ignores auxiliary global events and rejects native tool paths outside the current catalog.
// Summary text is never published as final output; completion is checked in the POST response.
// consume игнорирует auxiliary global events и отклоняет native tool paths вне текущего каталога.
// Summary не публикуется как final; завершение проверяется по POST response.
func (observer *turnObserver) consume(event Event) error {
	switch event.Type {
	case "session.error":
		var value struct {
			SessionID string `json:"sessionID"`
			Error     struct {
				Data struct {
					StatusCode int `json:"statusCode"`
				} `json:"data"`
			} `json:"error"`
		}
		if json.Unmarshal(event.Properties, &value) != nil {
			return ErrProtocol
		}
		if value.SessionID == observer.sessionID {
			if value.Error.Data.StatusCode == 401 || value.Error.Data.StatusCode == 403 {
				return ErrAuthentication
			}
			return ErrUnavailable
		}
	case "session.compacted":
		var value struct {
			SessionID string `json:"sessionID"`
		}
		if json.Unmarshal(event.Properties, &value) != nil {
			return ErrProtocol
		}
		if value.SessionID == observer.sessionID {
			observer.result.Compactions++
		}
	case "message.updated":
		var value struct {
			Info struct {
				ID, SessionID, Role string
				Summary             bool
				Time                struct {
					Completed int64 `json:"completed"`
				}
			} `json:"info"`
		}
		if json.Unmarshal(event.Properties, &value) != nil {
			return ErrProtocol
		}
		info := value.Info
		if info.SessionID != observer.sessionID || info.Role != "assistant" {
			return nil
		}
		for _, id := range observer.retired {
			if id == info.ID {
				return nil
			}
		}
		if observer.active[info.ID] == nil {
			if len(observer.active) >= 8 {
				return ErrProtocol
			}
			observer.active[info.ID] = &observedMessage{summary: info.Summary, parts: map[string]bool{}}
			if info.Summary && observer.progress != nil {
				if err := observer.progress("MODEL_CONTEXT_COMPACTING"); err != nil {
					return err
				}
			}
		}
		message := observer.active[info.ID]
		message.complete = message.complete || info.Time.Completed != 0
		return observer.finishMessage(info.ID, message)

	case "message.part.updated":
		var value struct {
			Part struct {
				ID, SessionID, MessageID, Type, Text, Tool string
				Time                                       struct {
					End int64 `json:"end"`
				} `json:"time"`
				Tokens *nativeTokens `json:"tokens"`
			} `json:"part"`
		}
		if json.Unmarshal(event.Properties, &value) != nil {
			return ErrProtocol
		}
		part := value.Part
		if part.SessionID != observer.sessionID {
			return nil
		}
		message := observer.active[part.MessageID]
		if message == nil {
			return nil
		}
		if part.Type == "tool" && !observer.allowTool(part.Tool) {
			return ErrProtocol
		}
		if message.parts[part.ID] {
			return nil
		}
		if part.Type == "step-finish" {
			if part.Tokens == nil {
				return ErrProtocol
			}
			usage, err := part.Tokens.usage()
			if err != nil {
				return err
			}
			observer.result.Usage.InputTokens += usage.InputTokens
			observer.result.Usage.OutputTokens += usage.OutputTokens
			observer.result.Usage.TotalTokens += usage.TotalTokens
			observer.result.Usage.CachedInputTokens += usage.CachedInputTokens
			observer.result.Usage.CacheWriteInputTokens += usage.CacheWriteInputTokens
			observer.result.Usage.ReasoningOutputTokens += usage.ReasoningOutputTokens
			if observer.result.Usage.Validate() != nil {
				return ErrProtocol
			}
			if observer.measurement != nil {
				phase := "NORMAL"
				if message.summary {
					phase = "COMPACTION"
				}
				usage.Completeness = runtimecontract.UsageUnknown
				completeness := "UNKNOWN"
				if usage.TotalTokens > 0 {
					usage.Completeness = runtimecontract.UsagePartial
					completeness = "PARTIAL"
				}
				digest := sha256.Sum256([]byte(observer.sessionID + "\x00" + part.ID))
				measurement := runtimecontract.ModelMeasurement{Ref: "step_" + hex.EncodeToString(digest[:]), Source: "NATIVE_STEP", Phase: phase, Outcome: "COMPLETED", Completeness: completeness, Usage: &usage}
				if measurement.Validate() != nil {
					return ErrProtocol
				}
				if err := observer.measurement(measurement); err != nil {
					return err
				}
			}
			message.parts[part.ID] = true
			message.measured = true
		}
		if part.Type == "text" && part.Time.End != 0 && !message.summary {
			if len(part.Text) > runtimecontract.MaximumRuntimeMessageBytes || !utf8.ValidString(part.Text) {
				return ErrProtocol
			}
			message.parts[part.ID] = true
			if strings.TrimSpace(part.Text) != "" {
				if err := observer.commentary(part.ID, part.Text); err != nil {
					return err
				}
			}
		}
		if len(message.parts) > 1024 {
			return ErrProtocol
		}
		return observer.finishMessage(part.MessageID, message)
	}
	return nil
}

// finishMessage joins completion with its measured step before releasing bounded observer state.
// Upstream may serialize the first message event after mutating its completed timestamp;
// that timestamp alone must neither discard following parts nor close the final stream.
// finishMessage объединяет завершение с измеренным шагом до освобождения состояния наблюдателя.
// Upstream может сериализовать первое событие уже с изменённым временем завершения;
// одной этой отметки недостаточно для отбрасывания частей или закрытия итогового потока.
func (observer *turnObserver) finishMessage(id string, message *observedMessage) error {
	if !message.complete || !message.measured {
		return nil
	}
	if message.summary && observer.progress != nil {
		if err := observer.progress("MODEL_REQUEST_RUNNING"); err != nil {
			return err
		}
	}
	delete(observer.active, id)
	if len(observer.retired) == 64 {
		copy(observer.retired, observer.retired[1:])
		observer.retired = observer.retired[:63]
	}
	observer.retired = append(observer.retired, id)
	if observer.completed != nil {
		return observer.completed(id)
	}
	return nil
}

// nativeTokens follows upstream's normalized counters: uncached input excludes cache reads/writes,
// and visible output excludes reasoning. This is accounting, not proof of physical cache reuse.
// nativeTokens соответствует нормализованным counters upstream: input исключает cache read/write,
// visible output исключает reasoning. Это accounting, не доказательство физического cache reuse.
type nativeTokens struct {
	Input, Output, Reasoning int64
	Cache                    struct{ Read, Write int64 } `json:"cache"`
}

// usage validates every upstream counter before combining the known disjoint normalized categories.
// usage проверяет каждый upstream counter до сложения известных непересекающихся категорий.
func (tokens nativeTokens) usage() (runtimecontract.TokenUsage, error) {
	for _, value := range []int64{tokens.Input, tokens.Output, tokens.Reasoning, tokens.Cache.Read, tokens.Cache.Write} {
		if value < 0 || value > 1_000_000_000_000 {
			return runtimecontract.TokenUsage{}, ErrProtocol
		}
	}
	usage := runtimecontract.TokenUsage{InputTokens: tokens.Input + tokens.Cache.Read + tokens.Cache.Write, OutputTokens: tokens.Output + tokens.Reasoning, ReasoningOutputTokens: tokens.Reasoning, CachedInputTokens: tokens.Cache.Read, CacheWriteInputTokens: tokens.Cache.Write}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	if usage.Validate() != nil {
		return runtimecontract.TokenUsage{}, ErrProtocol
	}
	return usage, nil
}

// finalText requires an ordinary completed stop message; a summary, length stop or error cannot pass.
// finalText требует обычное completed stop message; summary, length stop и error не проходят.
func finalText(message Message, sessionID string) (string, error) {
	var info struct {
		SessionID, Role, Finish string
		Summary                 bool
		Error                   json.RawMessage
		Time                    struct {
			Completed int64 `json:"completed"`
		}
	}
	if json.Unmarshal(message.Info, &info) != nil || info.SessionID != sessionID || info.Role != "assistant" {
		return "", ErrProtocol
	}
	if len(info.Error) != 0 && string(info.Error) != "null" {
		var failure struct {
			Data struct {
				StatusCode int `json:"statusCode"`
			} `json:"data"`
		}
		if json.Unmarshal(info.Error, &failure) != nil {
			return "", ErrProtocol
		}
		if failure.Data.StatusCode == 401 || failure.Data.StatusCode == 403 {
			return "", ErrAuthentication
		}
		return "", ErrUnavailable
	}
	if info.Summary || info.Finish != "stop" || info.Time.Completed == 0 {
		return "", ErrProtocol
	}
	var text strings.Builder
	for _, raw := range message.Parts {
		var part struct{ Type, Text string }
		if json.Unmarshal(raw, &part) != nil {
			return "", ErrProtocol
		}
		if part.Type == "text" {
			if text.Len()+len(part.Text) > runtimecontract.MaximumRuntimeMessageBytes || !utf8.ValidString(part.Text) {
				return "", ErrProtocol
			}
			text.WriteString(part.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", ErrProtocol
	}
	return text.String(), nil
}

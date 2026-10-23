package websockettransport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc"
)

// streamLocalizingRecorder adds deterministic locale selection to an in-memory HTTP recorder; it does not provide production translation or a socket.
// streamLocalizingRecorder добавляет детерминированный выбор языка к HTTP-рекордеру в памяти без промышленного перевода и сокета.
type streamLocalizingRecorder struct{ *httptest.ResponseRecorder }

// Localize prefixes the code with the request language header for assertions without translating or validating that header.
// Localize добавляет к коду префикс заголовка языка для проверок без перевода и проверки самого заголовка.
func (recorder *streamLocalizingRecorder) Localize(messageID string) string {
	return "accept-language:" + messageID
}

// LocalizeFor returns the selected locale and code as assertion text without loading a real translation catalog.
// LocalizeFor возвращает выбранный язык и код как проверочный текст без загрузки настоящего каталога переводов.
func (recorder *streamLocalizingRecorder) LocalizeFor(locale, messageID string) string {
	return locale + ":" + messageID
}

// TestStreamLocalizerUsesBoundedSelectedLocale checks supported Russian selection and fallback for an unknown locale, keeping arbitrary input out of the chosen locale.
// TestStreamLocalizerUsesBoundedSelectedLocale проверяет выбор русского и запасной вариант для неизвестного языка без переноса произвольного ввода в выбранный язык.
func TestStreamLocalizerUsesBoundedSelectedLocale(t *testing.T) {
	writer := &streamLocalizingRecorder{ResponseRecorder: httptest.NewRecorder()}
	request := httptest.NewRequest(http.MethodGet, "https://owner.example.test/api/v1/session/stream?locale=ru", nil)
	if title := streamLocalizer(writer, request)("STREAM_UNAVAILABLE"); title != "ru:STREAM_UNAVAILABLE" {
		t.Fatalf("selected locale was ignored: %q", title)
	}
	request = httptest.NewRequest(http.MethodGet, "https://owner.example.test/api/v1/session/stream?locale=unexpected", nil)
	if title := streamLocalizer(writer, request)("STREAM_UNAVAILABLE"); title != "accept-language:STREAM_UNAVAILABLE" {
		t.Fatalf("unsupported locale did not use safe fallback: %q", title)
	}
}

// TestRequestedProtocolsRequiresExactBaseAndSingleCSRF accepts the exact v2 protocol with one CSRF and ticket offer and rejects duplicate CSRF or unknown protocol offers; binding verification is outside this parser test.
// TestRequestedProtocolsRequiresExactBaseAndSingleCSRF принимает точный протокол v2 с одним CSRF и билетом и отклоняет повторный CSRF либо неизвестный протокол; проверка привязки находится вне этого теста parser.
func TestRequestedProtocolsRequiresExactBaseAndSingleCSRF(t *testing.T) {
	request := httptest.NewRequest("GET", "https://owner.example.test/api/v1/session/stream", nil)
	request.Header.Add("Sec-WebSocket-Protocol", "kodex.session.v2, csrf.token-value, ticket."+strings.Repeat("t", 43))
	selection, ok := requestedProtocols(request, sessionSubprotocol)
	if !ok || selection.csrf != "token-value" {
		t.Fatalf("valid protocol selection rejected: ok=%t csrf=%q", ok, selection.csrf)
	}
	request.Header.Add("Sec-WebSocket-Protocol", "csrf.second-token")
	if _, ok := requestedProtocols(request, sessionSubprotocol); ok {
		t.Fatal("duplicate CSRF subprotocol was accepted")
	}
	request = httptest.NewRequest("GET", "https://owner.example.test/api/v1/session/stream", nil)
	request.Header.Add("Sec-WebSocket-Protocol", "kodex.session.v2, csrf.token-value, legacy.protocol")
	if _, ok := requestedProtocols(request, sessionSubprotocol); ok {
		t.Fatal("unknown WebSocket subprotocol was accepted")
	}
}

// TestWebSocketProtocolTransitionHasAbsoluteRetirement uses a synthetic clock to restrict the existing v1 path to its explicit cutoff, keep it off by default, and require a ticket for v2 without downgrading.
// TestWebSocketProtocolTransitionHasAbsoluteRetirement по тестовым часам ограничивает существующий путь v1 явным сроком, оставляет его выключенным по умолчанию и требует билет v2 без понижения протокола.
func TestWebSocketProtocolTransitionHasAbsoluteRetirement(t *testing.T) {
	now := time.Now().UTC()
	server := &Server{legacyUntil: now.Add(time.Hour)}
	request := httptest.NewRequest("GET", "https://owner.example.test/api/v1/session/stream", nil)
	request.Header.Set("Sec-WebSocket-Protocol", "kodex.session.v1, csrf.token-value")
	if _, protocol, ok := server.selectProtocols(request, now); !ok || protocol != legacySessionSubprotocol {
		t.Fatal("legacy transition unavailable")
	}
	if _, _, ok := server.selectProtocols(request, server.legacyUntil); ok {
		t.Fatal("legacy cutoff was extended")
	}
	if _, _, ok := (&Server{}).selectProtocols(request, now); ok {
		t.Fatal("legacy enabled by default")
	}
	request.Header.Set("Sec-WebSocket-Protocol", "kodex.session.v2, csrf.token-value")
	if _, _, ok := server.selectProtocols(request, now); ok {
		t.Fatal("v2 silently fell back without ticket")
	}
	request.Header.Set("Sec-WebSocket-Protocol", "kodex.session.v2, csrf.token-value, ticket."+strings.Repeat("t", 43))
	if _, protocol, ok := server.selectProtocols(request, server.legacyUntil); !ok || protocol != sessionSubprotocol {
		t.Fatal("v2 disabled with legacy")
	}
}

// TestValidateSessionResumeRejectsDuplicatesAndBounds checks a valid resume, duplicate roots, excessive subscriptions and a negative platform cursor; it does not establish ownership of referenced resources.
// TestValidateSessionResumeRejectsDuplicatesAndBounds проверяет допустимое возобновление, повторные корни, лишние подписки и отрицательный курсор платформы без доказательства владения указанными ресурсами.
func TestValidateSessionResumeRejectsDuplicatesAndBounds(t *testing.T) {
	valid := generated.SessionResumeEnvelope{
		Type: "SESSION_RESUME", RequestRef: "request_0001", PlatformAfterSequence: 7,
		Runs: []generated.RunResumeCursor{{RunRef: "run_root0001", AfterSequence: 3}},
	}
	if err := validateSessionResume(valid); err != nil {
		t.Fatalf("valid session resume rejected: %v", err)
	}
	duplicated := valid
	duplicated.Runs = append(duplicated.Runs, duplicated.Runs[0])
	if err := validateSessionResume(duplicated); err == nil {
		t.Fatal("duplicated run cursor was accepted")
	}
	tooMany := valid
	tooMany.Runs = make([]generated.RunResumeCursor, maximumRunSubscriptions+1)
	for index := range tooMany.Runs {
		tooMany.Runs[index] = generated.RunResumeCursor{RunRef: "run_root_" + string(rune('A'+index)), AfterSequence: 0}
	}
	if err := validateSessionResume(tooMany); err == nil {
		t.Fatal("unbounded run subscription set was accepted")
	}
	invalidCursor := valid
	invalidCursor.PlatformAfterSequence = -1
	if err := validateSessionResume(invalidCursor); err == nil {
		t.Fatal("negative platform cursor was accepted")
	}
}

// TestNeedsPlatformBootstrap distinguishes an exact complete cursor from incomplete, behind, ahead and initial resume states that require bootstrap.
// TestNeedsPlatformBootstrap отличает точный полный курсор от неполного, отстающего, опережающего и начального состояний, которым нужен bootstrap.
func TestNeedsPlatformBootstrap(t *testing.T) {
	tests := []struct {
		name             string
		requestedAfter   int64
		current          int64
		snapshotRequired bool
		want             bool
	}{
		{name: "same cursor and complete cache", requestedAfter: 7, current: 7, want: false},
		{name: "same cursor and incomplete cache", requestedAfter: 7, current: 7, snapshotRequired: true, want: true},
		{name: "client cursor behind", requestedAfter: 6, current: 7, want: true},
		{name: "client cursor ahead", requestedAfter: 8, current: 7, want: true},
		{name: "initial bootstrap", requestedAfter: 0, current: 0, snapshotRequired: true, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := needsPlatformBootstrap(test.requestedAfter, test.current, test.snapshotRequired); got != test.want {
				t.Fatalf("needsPlatformBootstrap() = %t, want %t", got, test.want)
			}
		})
	}
}

// TestPlatformReadyAvailabilityPresenceDistinguishesBootstrapAndResume preserves the wire distinction between an explicitly empty availability list during bootstrap and an omitted list on resume, avoiding unintended cache replacement.
// TestPlatformReadyAvailabilityPresenceDistinguishesBootstrapAndResume сохраняет различие между явно пустым списком доступности при bootstrap и отсутствующим списком при возобновлении без нежелательной замены кэша.
func TestPlatformReadyAvailabilityPresenceDistinguishesBootstrapAndResume(t *testing.T) {
	emptyKinds := []generated.PlatformResourceKind{}
	fullBootstrap, err := json.Marshal(generated.PlatformReadyEnvelope{
		Type: "PLATFORM_READY", RequestRef: "request_0001", StreamKind: "PLATFORM",
		StreamRef: platformStreamRef, Cursor: 7, AvailableKinds: &emptyKinds,
	})
	if err != nil {
		t.Fatalf("marshal full bootstrap: %v", err)
	}
	if !strings.Contains(string(fullBootstrap), `"availableKinds":[]`) {
		t.Fatalf("full bootstrap omitted an explicit empty availability set: %s", fullBootstrap)
	}

	resume, err := json.Marshal(generated.PlatformReadyEnvelope{
		Type: "PLATFORM_READY", RequestRef: "request_0001", StreamKind: "PLATFORM",
		StreamRef: platformStreamRef, Cursor: 7,
	})
	if err != nil {
		t.Fatalf("marshal same-cursor resume: %v", err)
	}
	if strings.Contains(string(resume), "availableKinds") {
		t.Fatalf("same-cursor resume unexpectedly replaced availability: %s", resume)
	}
}

// TestDecodeSessionCommandIsClosedAndTyped checks typed subscribe decoding and rejection of unknown fields, trailing documents and unknown commands; duplicate JSON keys are not covered.
// TestDecodeSessionCommandIsClosedAndTyped проверяет типизированный разбор подписки и отказ при неизвестных полях, дополнительных документах и неизвестных командах; повторные JSON-ключи здесь не проверяются.
func TestDecodeSessionCommandIsClosedAndTyped(t *testing.T) {
	command, err := decodeSessionCommand([]byte(`{"type":"SUBSCRIBE_RUN","requestRef":"request_0001","runRef":"run_root0001","afterSequence":4}`))
	if err != nil {
		t.Fatalf("valid subscribe command rejected: %v", err)
	}
	value, ok := command.(subscribeRunCommand)
	if !ok || value.RunRef != "run_root0001" || value.AfterSequence != 4 {
		t.Fatalf("unexpected subscribe command: %#v", command)
	}
	invalidPayloads := [][]byte{
		[]byte(`{"type":"SUBSCRIBE_RUN","requestRef":"request_0001","runRef":"run_root0001","afterSequence":4,"foreign":true}`),
		[]byte(`{"type":"UNSUBSCRIBE_RUN","requestRef":"request_0001","runRef":"run_root0001"}{}`),
		[]byte(`{"type":"LEGACY_RESUME","requestRef":"request_0001"}`),
	}
	for _, payload := range invalidPayloads {
		if _, decodeErr := decodeSessionCommand(payload); decodeErr == nil {
			t.Fatalf("invalid command accepted: %s", payload)
		}
	}
}

// TestDecodePlatformSignalRedactsPayloadAndRejectsMismatch requires the expected organization and matching known kind, exposing a reduced wake instead of forwarding the source payload.
// TestDecodePlatformSignalRedactsPayloadAndRejectsMismatch требует ожидаемую организацию и соответствующий известный вид, выдавая сокращённый сигнал вместо исходного payload.
func TestDecodePlatformSignalRedactsPayloadAndRejectsMismatch(t *testing.T) {
	payload := []byte(`{"eventId":"d561fbb0-02c0-4be7-af7c-5998925632bd","eventName":"INTEGRATION_CONNECTION_CHANGED","eventVersion":1,"occurredAt":"2026-08-22T12:00:00Z","organizationRef":"org_example0001","projectRef":"prj_example0001","aggregateRef":"icon_example001","aggregateVersion":2,"sequence":8,"correlationRef":"d1713d76-566d-43c3-a0b2-0ca2307869d0","data":{"kind":"INTEGRATION_CONNECTION","safeSummary":"i18n:INTEGRATION_CONNECTION_TEST_COMPLETED"}}`)
	signal, ok := decodePlatformSignal(payload, "org_example0001")
	if !ok || signal.Sequence != 8 || signal.EventName != "INTEGRATION_CONNECTION_CHANGED" || signal.Kind != "INTEGRATION_CONNECTION" {
		t.Fatalf("valid signal rejected: ok=%t signal=%+v", ok, signal)
	}
	if _, ok := decodePlatformSignal(payload, "org_foreign0001"); ok {
		t.Fatal("foreign organization signal was accepted")
	}
	tampered := []byte(`{"eventId":"d561fbb0-02c0-4be7-af7c-5998925632bd","eventName":"AGENT_CHANGED","eventVersion":1,"occurredAt":"2026-08-22T12:00:00Z","organizationRef":"org_example0001","aggregateRef":"agt_example0001","aggregateVersion":2,"sequence":9,"correlationRef":"d1713d76-566d-43c3-a0b2-0ca2307869d0","data":{"kind":"PROJECT","safeSummary":"i18n:AGENT_UPDATED"}}`)
	if _, ok := decodePlatformSignal(tampered, "org_example0001"); ok {
		t.Fatal("event name and kind mismatch was accepted")
	}
}

// TestDecodePlatformSignalAcceptsRunInvalidationWithoutForwardingRefs accepts the known run invalidation and checks its reduced sequence, event and kind without exposing aggregate references or body.
// TestDecodePlatformSignalAcceptsRunInvalidationWithoutForwardingRefs принимает известную инвалидацию запуска и проверяет сокращённые номер, событие и вид без передачи ссылок агрегата и тела.
func TestDecodePlatformSignalAcceptsRunInvalidationWithoutForwardingRefs(t *testing.T) {
	payload := []byte(`{"eventId":"d561fbb0-02c0-4be7-af7c-5998925632bd","eventName":"RUN_CHANGED","eventVersion":1,"occurredAt":"2026-08-22T12:00:00Z","organizationRef":"org_example0001","projectRef":"prj_example0001","aggregateRef":"run_example0001","aggregateVersion":3,"sequence":9,"correlationRef":"d1713d76-566d-43c3-a0b2-0ca2307869d0","data":{"kind":"RUN","safeSummary":"i18n:RUN_UPDATED"}}`)
	signal, ok := decodePlatformSignal(payload, "org_example0001")
	if !ok || signal.Sequence != 9 || signal.EventName != "RUN_CHANGED" || signal.Kind != "RUN" {
		t.Fatalf("valid run invalidation rejected: ok=%t signal=%+v", ok, signal)
	}
}

// platformSignalBoundFixture marshals a synthetic platform envelope with a caller-selected summary and fixed owner, references and sequence; it is not an authenticated event producer.
// platformSignalBoundFixture сериализует тестовую оболочку платформы с заданной сводкой и фиксированными владельцем, ссылками и номером без аутентифицированного producer событий.
func platformSignalBoundFixture(t *testing.T, summary string) []byte {
	t.Helper()
	payload := platformBusEnvelope{
		EventID: "d561fbb0-02c0-4be7-af7c-5998925632bd", EventName: "RUN_CHANGED", EventVersion: 1,
		OccurredAt: time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC), OrganizationRef: "org_example0001",
		ProjectRef: "prj_example0001", AggregateRef: "run_example0001", AggregateVersion: 3,
		Sequence: 9, CorrelationRef: "corr_example001",
	}
	payload.Data.Kind, payload.Data.SafeSummary = "RUN", summary
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal platform fixture: %v", err)
	}
	return encoded
}

// TestDecodePlatformSignalUnicodeSummaryBound verifies the one-thousand-rune summary limit for ASCII, Cyrillic and emoji independently of UTF-8 byte length.
// TestDecodePlatformSignalUnicodeSummaryBound проверяет предел сводки в тысячу рун для ASCII, кириллицы и emoji независимо от длины UTF-8 в байтах.
func TestDecodePlatformSignalUnicodeSummaryBound(t *testing.T) {
	for name, character := range map[string]string{"ASCII": "a", "Cyrillic": "я", "emoji": "😀"} {
		for _, count := range []int{1000, 1001} {
			t.Run(fmt.Sprintf("%s/%d", name, count), func(t *testing.T) {
				payload := platformSignalBoundFixture(t, strings.Repeat(character, count))
				signal, ok := decodePlatformSignal(payload, "org_example0001")
				if ok != (count == 1000) || (ok && (signal.Sequence != 9 || signal.EventName != "RUN_CHANGED" || signal.Kind != "RUN" || signal.ProjectRef != "prj_example0001")) {
					t.Fatal("platform Unicode bound or safe wake projection changed")
				}
			})
		}
	}
}

// TestDecodePlatformSignalExactByteBoundAndMalformed checks the exact platform byte cap and malformed encoding, shape, ownership and event metadata while retaining the separate run cap; it does not test every JSON ambiguity.
// TestDecodePlatformSignalExactByteBoundAndMalformed проверяет точный байтовый предел платформы и неверные кодировку, структуру, владельца и metadata события, сохраняя отдельный предел запуска; все неоднозначности JSON не охватываются.
func TestDecodePlatformSignalExactByteBoundAndMalformed(t *testing.T) {
	base := platformSignalBoundFixture(t, "safe-summary")
	for _, size := range []int{65536, 65537} {
		t.Run(fmt.Sprintf("bytes/%d", size), func(t *testing.T) {
			payload := append(append([]byte(nil), base...), []byte(strings.Repeat(" ", size-len(base)))...)
			if _, ok := decodePlatformSignal(payload, "org_example0001"); ok != (size == 65536) {
				t.Fatal("platform frame did not enforce its independent byte bound")
			}
		})
	}
	for name, payload := range map[string][]byte{
		"invalid-UTF8":   []byte(strings.Replace(string(base), "safe-summary", string([]byte{0xff}), 1)),
		"unknown-field":  []byte(strings.Replace(string(base), `"data":`, `"unknown":true,"data":`, 1)),
		"extra-document": append(append([]byte(nil), base...), []byte(`{}`)...),
		"unknown-event":  []byte(strings.Replace(string(base), "RUN_CHANGED", "UNKNOWN_CHANGED", 1)),
		"wrong-kind":     []byte(strings.Replace(string(base), `"kind":"RUN"`, `"kind":"AGENT"`, 1)),
		"zero-sequence":  []byte(strings.Replace(string(base), `"sequence":9`, `"sequence":0`, 1)),
		"wrong-version":  []byte(strings.Replace(string(base), `"eventVersion":1`, `"eventVersion":2`, 1)),
		"foreign-owner":  []byte(strings.Replace(string(base), "org_example0001", "org_foreign0001", 1)),
		"malformed-JSON": []byte(`{`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := decodePlatformSignal(payload, "org_example0001"); ok {
				t.Fatal("malformed or mismatched platform signal was accepted")
			}
		})
	}
	if maximumFrameBytes != 1<<20 {
		t.Fatal("shared WS/RUN frame bound changed")
	}
}

// TestDecodePlatformSignalAcceptsRoleImageLifecycleEvents accepts only the fixture's known role-image lifecycle event names with the matching aggregate kind.
// TestDecodePlatformSignalAcceptsRoleImageLifecycleEvents проверяет известные в оснастке имена событий жизненного цикла образа роли с соответствующим видом агрегата.
func TestDecodePlatformSignalAcceptsRoleImageLifecycleEvents(t *testing.T) {
	for index, eventName := range []string{"ROLE_IMAGE_PROMOTION_REQUESTED", "ROLE_IMAGE_PROMOTED"} {
		payload := []byte(fmt.Sprintf(`{"eventId":"d561fbb0-02c0-4be7-af7c-5998925632bd","eventName":"%s","eventVersion":1,"occurredAt":"2026-08-22T12:00:00Z","organizationRef":"org_example0001","projectRef":"prj_example0001","aggregateRef":"rimg_example001","aggregateVersion":2,"sequence":%d,"correlationRef":"d1713d76-566d-43c3-a0b2-0ca2307869d0","data":{"kind":"ROLE_IMAGE_RECIPE","safeSummary":"i18n:ROLE_IMAGE_RECIPE_CHANGED"}}`, eventName, index+10))
		signal, ok := decodePlatformSignal(payload, "org_example0001")
		if !ok || signal.EventName != eventName || signal.Kind != "ROLE_IMAGE_RECIPE" {
			t.Fatalf("valid role image lifecycle signal rejected: ok=%t signal=%+v", ok, signal)
		}
	}
}

// TestPlatformSignalOutsideScope keeps global, organization and project-catalog wakes in scope while excluding unrelated project-specific noncatalog wakes.
// TestPlatformSignalOutsideScope сохраняет глобальные, организационные и каталожные проектные сигналы в области, исключая некаталожные сигналы другого проекта.
func TestPlatformSignalOutsideScope(t *testing.T) {
	t.Parallel()
	if platformSignalOutsideScope(platformSignal{Kind: "SYSTEM_ASSISTANT", ProjectRef: "prj_other0001"}, "") {
		t.Fatal("global scope skipped an accessible project signal")
	}
	if !platformSignalOutsideScope(platformSignal{Kind: "RUNTIME_SECRET", ProjectRef: "prj_other0001"}, "prj_selected01") {
		t.Fatal("foreign project signal entered the selected project snapshot")
	}
	if platformSignalOutsideScope(platformSignal{Kind: "PROJECT", ProjectRef: "prj_other0001"}, "prj_selected01") {
		t.Fatal("organization project catalog signal was skipped")
	}
	if platformSignalOutsideScope(platformSignal{Kind: "RUNTIME_SECRET", ProjectRef: "prj_selected01"}, "prj_selected01") {
		t.Fatal("selected project signal was skipped")
	}
}

// catchUpQueryClient embeds the unused generated methods and supplies an ordered synthetic event list for catch-up assertions without a real RPC client or event store.
// catchUpQueryClient встраивает неиспользуемые generated-методы и задаёт упорядоченные тестовые события для проверок догоняющего чтения без настоящего RPC-клиента и event store.
type catchUpQueryClient struct {
	controlplanev1.PlatformQueryServiceClient
	events []*controlplanev1.RunEvent
}

// ListRunEvents filters the fixture events after the requested sequence and reports their maximum as complete, ignoring context, root and limit; it does not emulate authorization or real pagination.
// ListRunEvents отбирает тестовые события после заданного номера и сообщает их максимум как полный результат, игнорируя контекст, корень и предел; авторизация и настоящая пагинация не моделируются.
func (client *catchUpQueryClient) ListRunEvents(_ context.Context, request *controlplanev1.ListRunEventsRequest, _ ...grpc.CallOption) (*controlplanev1.ListRunEventsResponse, error) {
	result := make([]*controlplanev1.RunEvent, 0, len(client.events))
	current := int64(0)
	for _, event := range client.events {
		if event.GetSequence() > current {
			current = event.GetSequence()
		}
		if event.GetSequence() > request.GetAfterSequence() {
			result = append(result, event)
		}
	}
	return &controlplanev1.ListRunEventsResponse{Events: result, CurrentSequence: current, Complete: true}, nil
}

// TestReadCatchUpRestoresMissingEventsInOrder consumes synthetic sequences two through four after cursor one and verifies exact order and the resulting cursor.
// TestReadCatchUpRestoresMissingEventsInOrder принимает тестовые номера со второго по четвёртый после курсора один и сверяет точный порядок и итоговый курсор.
func TestReadCatchUpRestoresMissingEventsInOrder(t *testing.T) {
	client := &catchUpQueryClient{events: []*controlplanev1.RunEvent{
		{Sequence: 1}, {Sequence: 2}, {Sequence: 3}, {Sequence: 4},
	}}
	var restored []int64
	latest, err := readCatchUp(context.Background(), client, "run_root0001", 1, func(event *controlplanev1.RunEvent) error {
		restored = append(restored, event.GetSequence())
		return nil
	})
	if err != nil {
		t.Fatalf("catch up missing events: %v", err)
	}
	if latest != 4 || len(restored) != 3 || restored[0] != 2 || restored[1] != 3 || restored[2] != 4 {
		t.Fatalf("unexpected catch-up result: latest=%d restored=%v", latest, restored)
	}
}

// TestReadCatchUpRejectsDurableGap rejects the synthetic gap from sequence one to three without advancing the cursor; despite its name it does not exercise a durable event store.
// TestReadCatchUpRejectsDurableGap отклоняет тестовый разрыв между первым и третьим номерами без продвижения курсора; несмотря на имя, устойчивый event store не проверяется.
func TestReadCatchUpRejectsDurableGap(t *testing.T) {
	client := &catchUpQueryClient{events: []*controlplanev1.RunEvent{{Sequence: 1}, {Sequence: 3}}}
	latest, err := readCatchUp(context.Background(), client, "run_root0001", 1, func(*controlplanev1.RunEvent) error { return nil })
	if err == nil || latest != 1 {
		t.Fatalf("durable gap was accepted: latest=%d err=%v", latest, err)
	}
}

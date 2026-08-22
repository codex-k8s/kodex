package websockettransport

import (
	"context"
	"net/http/httptest"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
)

func TestRequestedProtocolsRequiresExactBaseAndSingleCSRF(t *testing.T) {
	request := httptest.NewRequest("GET", "https://owner.example.test/api/v1/platform/stream", nil)
	request.Header.Add("Sec-WebSocket-Protocol", "mattercodex.platform.v1, csrf.token-value")
	selection, ok := requestedProtocols(request, platformSubprotocol)
	if !ok || selection.csrf != "token-value" {
		t.Fatalf("valid protocol selection rejected: ok=%t csrf=%q", ok, selection.csrf)
	}
	request.Header.Add("Sec-WebSocket-Protocol", "csrf.second-token")
	if _, ok := requestedProtocols(request, platformSubprotocol); ok {
		t.Fatal("duplicate CSRF subprotocol was accepted")
	}
}

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

type catchUpQueryClient struct {
	controlplanev1.PlatformQueryServiceClient
	events []*controlplanev1.RunEvent
}

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

func TestReadCatchUpRejectsDurableGap(t *testing.T) {
	client := &catchUpQueryClient{events: []*controlplanev1.RunEvent{{Sequence: 1}, {Sequence: 3}}}
	latest, err := readCatchUp(context.Background(), client, "run_root0001", 1, func(*controlplanev1.RunEvent) error { return nil })
	if err == nil || latest != 1 {
		t.Fatalf("durable gap was accepted: latest=%d err=%v", latest, err)
	}
}

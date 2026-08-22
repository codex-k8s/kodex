package websockettransport

import (
	"context"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
)

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

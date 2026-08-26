package natsjetstream

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

type fakeManagedStream struct {
	jetstream.Stream
	info    jetstream.StreamInfo
	infoErr error
}

func (stream *fakeManagedStream) Info(context.Context, ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error) {
	if stream.infoErr != nil {
		return nil, stream.infoErr
	}
	info := stream.info
	return &info, nil
}

type fakeJetStream struct {
	jetstream.JetStream
	stream        *fakeManagedStream
	streamErr     error
	createErr     error
	updateErr     error
	updateCalls   int
	updatedConfig jetstream.StreamConfig
}

func (manager *fakeJetStream) Stream(context.Context, string) (jetstream.Stream, error) {
	if manager.streamErr != nil {
		return nil, manager.streamErr
	}
	return manager.stream, nil
}

func (manager *fakeJetStream) CreateStream(context.Context, jetstream.StreamConfig) (jetstream.Stream, error) {
	return nil, manager.createErr
}

func (manager *fakeJetStream) UpdateStream(_ context.Context, config jetstream.StreamConfig) (jetstream.Stream, error) {
	manager.updateCalls++
	manager.updatedConfig = config
	if manager.updateErr != nil {
		return nil, manager.updateErr
	}
	manager.stream.info.Config = config
	return manager.stream, nil
}

func TestExpectedStreamContract(t *testing.T) {
	t.Parallel()
	config := Config{
		Stream:          "CONTROL_PLANE",
		Subjects:        []string{"control_plane.runtime_configuration_changed"},
		Replicas:        1,
		MaxMessageBytes: 256 << 10,
		MaxMessages:     10_000_000,
		MaxBytes:        4 << 30,
		MaxPerSubject:   5_000_000,
		MaxAge:          30 * 24 * time.Hour,
		DuplicateWindow: 2 * time.Minute,
	}
	actual := expectedStreamConfig(config)
	if !streamCompatible(actual, config) {
		t.Fatal("expected stream config must satisfy its exact contract")
	}
	actual.MaxBytes++
	if streamCompatible(actual, config) {
		t.Fatal("stream with another capacity must be rejected")
	}
	actual = expectedStreamConfig(config)
	actual.Storage = jetstream.MemoryStorage
	if streamCompatible(actual, config) {
		t.Fatal("stream with another storage must be rejected")
	}
}

func TestBoundedSubjectFilters(t *testing.T) {
	for _, value := range []string{"control_plane.run.*.*.events", "control_plane.platform.*.events"} {
		if !validSubjectFilter(value) {
			t.Fatalf("registered subject filter %q was rejected", value)
		}
	}
	for _, value := range []string{"control_plane.>", "control_plane.run.foo*", "control plane.run"} {
		if validSubjectFilter(value) {
			t.Fatalf("unsafe subject filter %q was accepted", value)
		}
	}
}

func TestEnsureStreamReconcilesOnlyEmptyStream(t *testing.T) {
	t.Parallel()
	config := Config{
		Stream: "CONTROL_PLANE", Subjects: []string{"control_plane.platform.*.events"},
		Replicas: 1, MaxMessageBytes: 64 << 10, MaxMessages: 1000, MaxBytes: 32 << 20,
		MaxPerSubject: 100, MaxAge: time.Hour, DuplicateWindow: time.Minute,
	}
	stream := &fakeManagedStream{info: jetstream.StreamInfo{Config: expectedStreamConfig(config)}}
	stream.info.Config.MaxBytes++
	manager := &fakeJetStream{stream: stream}
	publisher := &Publisher{jetstream: manager, config: config}
	if err := publisher.EnsureStream(context.Background()); err != nil {
		t.Fatalf("reconcile empty stream: %v", err)
	}
	if manager.updateCalls != 1 || !streamCompatible(manager.updatedConfig, config) {
		t.Fatalf("unexpected update: calls=%d config=%+v", manager.updateCalls, manager.updatedConfig)
	}
}

func TestEnsureStreamRejectsNonEmptyContractDrift(t *testing.T) {
	t.Parallel()
	config := Config{
		Stream: "CONTROL_PLANE", Subjects: []string{"control_plane.platform.*.events"},
		Replicas: 1, MaxMessageBytes: 64 << 10, MaxMessages: 1000, MaxBytes: 32 << 20,
		MaxPerSubject: 100, MaxAge: time.Hour, DuplicateWindow: time.Minute,
	}
	stream := &fakeManagedStream{info: jetstream.StreamInfo{
		Config: expectedStreamConfig(config), State: jetstream.StreamState{Msgs: 1, Bytes: 64},
	}}
	stream.info.Config.MaxBytes++
	manager := &fakeJetStream{stream: stream}
	publisher := &Publisher{jetstream: manager, config: config}
	err := publisher.EnsureStream(context.Background())
	if err == nil || !strings.Contains(err.Error(), "stream contract mismatch") {
		t.Fatalf("expected fail-closed contract mismatch, got %v", err)
	}
	if manager.updateCalls != 0 {
		t.Fatalf("non-empty stream was updated %d times", manager.updateCalls)
	}
}

func TestEnsureStreamPreservesCreateFailure(t *testing.T) {
	t.Parallel()
	createErr := errors.New("insufficient storage resources")
	manager := &fakeJetStream{streamErr: jetstream.ErrStreamNotFound, createErr: createErr}
	publisher := &Publisher{jetstream: manager, config: Config{
		Stream: "CONTROL_PLANE", Subjects: []string{"control_plane.platform.*.events"},
		Replicas: 1, MaxMessageBytes: 64 << 10, MaxMessages: 1000, MaxBytes: 32 << 20,
		MaxPerSubject: 100, MaxAge: time.Hour, DuplicateWindow: time.Minute,
	}}
	err := publisher.EnsureStream(context.Background())
	if !errors.Is(err, createErr) || !strings.Contains(err.Error(), createErr.Error()) {
		t.Fatalf("create failure was not preserved: %v", err)
	}
}

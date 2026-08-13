package natsjetstream

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

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

package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/emailprojection"
)

type projectionStoreFixture struct {
	config api.Configuration
	err    error
}

func (store projectionStoreFixture) EmailConfiguration(context.Context) (api.Configuration, error) {
	return store.config, store.err
}

type projectionPublisherFixture struct {
	mu        sync.Mutex
	published int
	checked   int
	fail      bool
}

func (publisher *projectionPublisherFixture) Publish(_ context.Context, config api.Configuration) (emailprojection.Receipt, error) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.published++
	if publisher.fail {
		return emailprojection.Receipt{}, emailprojection.ErrUnavailable
	}
	return emailprojection.Receipt{Revision: config.Revision, Digest: api.Digest(config)}, nil
}
func (publisher *projectionPublisherFixture) Check(_ context.Context, config api.Configuration) (emailprojection.Receipt, error) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	publisher.checked++
	if publisher.fail {
		return emailprojection.Receipt{}, emailprojection.ErrUnavailable
	}
	return emailprojection.Receipt{Revision: config.Revision, Digest: api.Digest(config)}, nil
}

func TestEmailProjectionReadinessDoesNotPublishOrAcceptMissingOwner(t *testing.T) {
	publisher := &projectionPublisherFixture{}
	projection := &emailProjection{store: projectionStoreFixture{err: errors.New("owner unavailable")}, publisher: publisher}
	if err := projection.Check(context.Background()); err == nil {
		t.Fatal("missing owner accepted")
	}
	if err := projection.reconcile(context.Background()); err == nil {
		t.Fatal("missing owner published")
	}
	if publisher.published != 0 || publisher.checked != 0 {
		t.Fatal("publisher used without owner snapshot")
	}
	projection.store = projectionStoreFixture{config: api.Configuration{Revision: 1}}
	if err := projection.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if publisher.published != 0 || publisher.checked != 1 {
		t.Fatal("readiness mutated projection")
	}
	publisher.fail = true
	if err := projection.Check(context.Background()); err == nil {
		t.Fatal("missing projection accepted")
	}
}

func TestEmailProjectionWorkerCancellationAndJoin(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		publisher := &projectionPublisherFixture{}
		projection := &emailProjection{store: projectionStoreFixture{}, publisher: publisher, interval: time.Millisecond, timeout: time.Second}
		if disabled {
			projection = nil
		}
		done := make(chan error, 1)
		go func() { done <- projection.Run(ctx) }()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("projection worker did not join")
		}
	}
}

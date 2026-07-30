package serviceruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWorkersCancelAndJoin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	group := StartWorkers(ctx, func(workerContext context.Context) error {
		<-workerContext.Done()
		return workerContext.Err()
	})
	cancel()
	waitContext, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := group.Wait(waitContext); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

func TestShutdownUsesIndependentContexts(t *testing.T) {
	first := errors.New("first")
	secondRan := false
	err := RunShutdown(
		context.Background(),
		ShutdownOperation{
			Name:    "first",
			Timeout: time.Second,
			Run: func(context.Context) error {
				return first
			},
		},
		ShutdownOperation{
			Name:    "second",
			Timeout: time.Second,
			Run: func(ctx context.Context) error {
				secondRan = ctx.Err() == nil
				return nil
			},
		},
	)
	if !errors.Is(err, first) || !secondRan {
		t.Fatalf("shutdown result = %v, second=%v", err, secondRan)
	}
}

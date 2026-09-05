package app

import (
	"context"
	"sync/atomic"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/configuration"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
)

type configurationRuntime struct {
	root    string
	accept  func(context.Context, api.Configuration, string) error
	build   func(*configuration.Snapshot) *mail.Service
	current atomic.Pointer[mail.Service]
}

// Refresh вызывается при startup, затем только единственным bounded monitor.
func (r *configurationRuntime) Refresh(ctx context.Context) error {
	snapshot, err := configuration.Load(ctx, r.root)
	if err == nil {
		err = r.accept(ctx, snapshot.Configuration, api.Digest(snapshot.Configuration))
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		r.current.Store(nil)
		return err
	}
	r.current.Store(r.build(snapshot))
	return nil
}

func (r *configurationRuntime) Service() *mail.Service { return r.current.Load() }

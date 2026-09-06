package app

import (
	"context"
	"errors"
	"sync/atomic"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/configuration"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
)

const (
	configurationBootstrap = "bootstrap"
	configurationManaged   = "managed"
	configurationPinError  = "email configuration deployment pin mismatch"
)

type configurationPins struct {
	mode     string
	revision int64
	digest   string
}

func (p configurationPins) valid() bool {
	return p.mode == configurationBootstrap && p.revision == 0 && p.digest == "" ||
		p.mode == configurationManaged && p.revision > 0 && mailtransport.ValidEgressDigest(p.digest)
}

func (p configurationPins) check(value api.Configuration, digest string) error {
	if !p.valid() {
		return errors.New(configurationPinError)
	}
	if p.mode == configurationManaged && value.Revision == p.revision && digest == p.digest {
		return nil
	}
	// Только пустой release seed допускается до первой owner-публикации.
	if p.mode == configurationBootstrap && value.Version == "email-bridge/v1" && value.Revision == 1 && value.ManagedBy == "git" && value.Source == "release-bootstrap" && len(value.Mailboxes) == 0 {
		return nil
	}
	return errors.New(configurationPinError)
}

type configurationRuntime struct {
	root    string
	check   func(api.Configuration, string) error
	accept  func(context.Context, api.Configuration, string) error
	report  func(context.Context, int64, string) error
	build   func(*configuration.Snapshot) *mail.Service
	current atomic.Pointer[mail.Service]
}

// Refresh вызывается при startup, затем только единственным bounded monitor.
func (r *configurationRuntime) Refresh(ctx context.Context) error {
	snapshot, err := configuration.Load(ctx, r.root)
	var digest string
	if err == nil {
		digest = api.Digest(snapshot.Configuration)
		if r.check != nil {
			err = r.check(snapshot.Configuration, digest)
		}
	}
	if err == nil {
		err = r.accept(ctx, snapshot.Configuration, digest)
	}
	var service *mail.Service
	if err == nil {
		service = r.build(snapshot)
		if service == nil {
			err = errors.New("email configuration service unavailable")
		}
	}
	if err == nil && r.report != nil {
		err = r.report(ctx, snapshot.Configuration.Revision, digest)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		r.current.Store(nil)
		return err
	}
	r.current.Store(service)
	return nil
}

func (r *configurationRuntime) Service() *mail.Service { return r.current.Load() }

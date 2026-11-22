package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// streamInputRepositoryFixture records typed forwarding and borrowed byte ownership without simulating stream authority or persistent staging.
// streamInputRepositoryFixture фиксирует typed forwarding и borrowed byte ownership без имитации stream authority либо persistent staging.
type streamInputRepositoryFixture struct {
	*materialRepositoryFixture
	seen, borrowed []byte
}

// ListKubernetesStreamInputs records an exact-node owner read without fabricating discovered streams.
// ListKubernetesStreamInputs фиксирует exact-node owner read без выдуманных discovered streams.
func (r *streamInputRepositoryFixture) ListKubernetesStreamInputs(context.Context, value.Principal, query.KubernetesStreamInputList) (entity.KubernetesStreamInputPage, error) {
	r.calls++
	return entity.KubernetesStreamInputPage{}, nil
}

// GetKubernetesStreamInput records a metadata-only owner read, never returning stored private input.
// GetKubernetesStreamInput фиксирует metadata-only owner read без выдачи stored private input.
func (r *streamInputRepositoryFixture) GetKubernetesStreamInput(context.Context, value.Principal, query.KubernetesStreamInputRead) (entity.KubernetesStreamInputReadback, error) {
	r.calls++
	return entity.KubernetesStreamInputReadback{}, nil
}

// SubmitKubernetesStreamInput observes the temporary owner-call copy so the test can verify clearing after synchronous return.
// SubmitKubernetesStreamInput наблюдает temporary owner-call copy для проверки очистки после synchronous return.
func (r *streamInputRepositoryFixture) SubmitKubernetesStreamInput(_ context.Context, _ value.Principal, _ value.Mutation, input command.KubernetesStreamInputSubmit) (entity.KubernetesStreamInputReadback, error) {
	r.calls++
	r.borrowed = input.Payload
	r.seen = bytes.Clone(input.Payload)
	return entity.KubernetesStreamInputReadback{}, nil
}

// StopKubernetesStream records only one tool-scoped owner closure without simulating a remote stop.
// StopKubernetesStream фиксирует лишь tool-scoped owner closure без имитации remote stop.
func (r *streamInputRepositoryFixture) StopKubernetesStream(context.Context, value.Principal, value.Mutation, command.KubernetesStreamInputStop) (entity.KubernetesStreamInputView, error) {
	r.calls++
	return entity.KubernetesStreamInputView{}, nil
}

// TestKubernetesStreamInputPublicBoundary preserves exact caller/method, closed absent persistence and owned input lifetime before any native authority exists.
// TestKubernetesStreamInputPublicBoundary сохраняет exact caller/method, closed absent persistence и owned input lifetime до возникновения native authority.
func TestKubernetesStreamInputPublicBoundary(t *testing.T) {
	repository := &streamInputRepositoryFixture{materialRepositoryFixture: &materialRepositoryFixture{}}
	service, err := New(repository)
	if err != nil {
		t.Fatal(err)
	}
	version := int64(3)
	mutation := value.Mutation{IdempotencyKey: "input-synthetic-key", ExpectedVersion: &version}
	payload := []byte("private-synthetic-input")
	sum := sha256.Sum256(payload)
	input := command.KubernetesStreamInputSubmit{InvocationRef: "inv_synthetic", StreamRef: "kst_synthetic", Channel: "STDIN", Payload: payload, PayloadDigest: hex.EncodeToString(sum[:])}
	for _, route := range []struct {
		permission string
		call       func(value.Principal) error
	}{
		{"platform.integrations.kubernetes.input.stop", func(p value.Principal) error {
			_, e := service.StopKubernetesStream(t.Context(), p, mutation, command.KubernetesStreamInputStop{InvocationRef: input.InvocationRef, StreamRef: input.StreamRef})
			return e
		}},
		{"platform.integrations.kubernetes.input.list", func(p value.Principal) error {
			_, e := service.ListKubernetesStreamInputs(t.Context(), p, query.KubernetesStreamInputList{RunRef: "run_synthetic", NodeRef: "nod_synthetic", Page: query.Page{Size: 20}})
			return e
		}},
		{"platform.integrations.kubernetes.input.get", func(p value.Principal) error {
			_, e := service.GetKubernetesStreamInput(t.Context(), p, query.KubernetesStreamInputRead{InvocationRef: input.InvocationRef, StreamRef: input.StreamRef})
			return e
		}},
		{"platform.integrations.kubernetes.input.submit", func(p value.Principal) error {
			_, e := service.SubmitKubernetesStreamInput(t.Context(), p, mutation, input)
			return e
		}},
	} {
		p := value.Principal{ActorID: "synthetic-actor", AuthorityTenant: "synthetic-tenant", CorrelationRef: "synthetic-trace", CredentialRevision: 1, CallerWorkload: "control-api-gateway", Permission: route.permission}
		before := repository.calls
		if err := route.call(p); err != nil || repository.calls != before+1 {
			t.Fatal("valid typed owner forwarding failed", err)
		}
		p.CallerWorkload = "integration-gateway"
		if err := route.call(p); !errors.Is(err, errs.ErrForbidden) || repository.calls != before+1 {
			t.Fatal("wrong public caller reached owner", err)
		}
		p.CallerWorkload = "control-api-gateway"
		p.Permission = "platform.integrations.kubernetes.input.foreign"
		if err := route.call(p); !errors.Is(err, errs.ErrForbidden) || repository.calls != before+1 {
			t.Fatal("wrong public operation reached owner", err)
		}
	}
	if !bytes.Equal(repository.seen, payload) || !bytes.Equal(repository.borrowed, make([]byte, len(payload))) {
		t.Fatal("private input copy lost or retained after owner call")
	}
	p := value.Principal{ActorID: "synthetic-actor", AuthorityTenant: "synthetic-tenant", CorrelationRef: "synthetic-trace", CredentialRevision: 1, CallerWorkload: "control-api-gateway", Permission: "platform.integrations.kubernetes.input.submit"}
	before := repository.calls
	input.Channel = "FILE_CONTENT"
	if _, err := service.SubmitKubernetesStreamInput(t.Context(), p, mutation, input); !errors.Is(err, errs.ErrInvalid) || repository.calls != before {
		t.Fatal("public submission replaced the pinned artifact source")
	}
	absent, err := New(&materialRepositoryFixture{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := absent.SubmitKubernetesStreamInput(t.Context(), p, mutation, input); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("absent private input owner became a generic queue", err)
	}
}

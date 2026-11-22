package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// streamOutputRepositoryFixture returns controlled owner results to exercise disclosure validation, not database authority.
// streamOutputRepositoryFixture возвращает контролируемые owner results для проверки выдачи, не database authority.
type streamOutputRepositoryFixture struct {
	*materialRepositoryFixture
	result entity.KubernetesStreamOutput
	err    error
}

// ReadKubernetesStreamOutput counts forwarding and transfers the result buffer to the service boundary.
// ReadKubernetesStreamOutput считает forwarding и передаёт result buffer в service boundary.
func (r *streamOutputRepositoryFixture) ReadKubernetesStreamOutput(context.Context, value.Principal, query.KubernetesStreamOutputRead) (entity.KubernetesStreamOutput, error) {
	r.calls++
	return r.result, r.err
}

// TestKubernetesStreamOutputPublicBoundary rejects unauthorized callers and malformed cursors before repository access.
// TestKubernetesStreamOutputPublicBoundary отклоняет чужой caller и malformed cursor до repository access.
func TestKubernetesStreamOutputPublicBoundary(t *testing.T) {
	r := &streamOutputRepositoryFixture{materialRepositoryFixture: &materialRepositoryFixture{}}
	s, err := New(r)
	if err != nil {
		t.Fatal(err)
	}
	p := value.Principal{ActorID: "synthetic-actor", AuthorityTenant: "synthetic-tenant", CorrelationRef: "synthetic-trace", CredentialRevision: 1, CallerWorkload: "control-api-gateway", Permission: "platform.integrations.kubernetes.output.read"}
	q := query.KubernetesStreamOutputRead{InvocationRef: "inv_synthetic", StreamRef: "kst_synthetic", Generation: 1}
	r.result = entity.KubernetesStreamOutput{InvocationRef: q.InvocationRef, StreamRef: q.StreamRef, Generation: 1, Status: "WAITING", AuthorizedUntil: time.Now().Add(time.Second)}
	if got, err := s.ReadKubernetesStreamOutput(t.Context(), p, q); err != nil || got.Status != "WAITING" || r.calls != 1 {
		t.Fatal("valid owner read failed", err)
	}
	for _, test := range []struct {
		name string
		p    value.Principal
		q    query.KubernetesStreamOutputRead
		want error
	}{
		{"caller", value.Principal{ActorID: p.ActorID, AuthorityTenant: p.AuthorityTenant, CorrelationRef: p.CorrelationRef, CredentialRevision: 1, CallerWorkload: "integration-gateway", Permission: p.Permission}, q, errs.ErrForbidden},
		{"permission", value.Principal{ActorID: p.ActorID, AuthorityTenant: p.AuthorityTenant, CorrelationRef: p.CorrelationRef, CredentialRevision: 1, CallerWorkload: p.CallerWorkload, Permission: "platform.runtime.kubernetes.output.read"}, q, errs.ErrForbidden},
		{"invocation", p, query.KubernetesStreamOutputRead{InvocationRef: "run_foreign", StreamRef: q.StreamRef, Generation: 1}, errs.ErrInvalid},
		{"stream", p, query.KubernetesStreamOutputRead{InvocationRef: q.InvocationRef, StreamRef: "inv_foreign", Generation: 1}, errs.ErrInvalid},
		{"generation", p, query.KubernetesStreamOutputRead{InvocationRef: q.InvocationRef, StreamRef: q.StreamRef}, errs.ErrInvalid},
		{"overflow", p, query.KubernetesStreamOutputRead{InvocationRef: q.InvocationRef, StreamRef: q.StreamRef, Generation: 1, AfterSequence: managedkubernetes.MaximumFrameSequence + 1}, errs.ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := s.ReadKubernetesStreamOutput(t.Context(), test.p, test.q); !errors.Is(err, test.want) || r.calls != 1 {
				t.Fatal("invalid read reached owner", err)
			}
		})
	}
	absent, err := New(&materialRepositoryFixture{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := absent.ReadKubernetesStreamOutput(t.Context(), p, q); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("missing reader did not fail closed", err)
	}
}

// TestKubernetesStreamOutputRejectsAndClearsInvalidOwnerData protects the exact cursor and clears rejected private bytes, including partial error results.
// TestKubernetesStreamOutputRejectsAndClearsInvalidOwnerData защищает exact cursor и очищает rejected private bytes, включая partial error results.
func TestKubernetesStreamOutputRejectsAndClearsInvalidOwnerData(t *testing.T) {
	for _, name := range []string{"valid", "invocation", "stream", "generation", "cursor", "digest", "oversize", "expired", "channel", "unknown", "invented_end", "partial_error"} {
		t.Run(name, func(t *testing.T) {
			body := []byte("private synthetic output")
			sum := sha256.Sum256(body)
			q := query.KubernetesStreamOutputRead{InvocationRef: "inv_synthetic", StreamRef: "kst_synthetic", Generation: 1}
			v := entity.KubernetesStreamOutput{InvocationRef: q.InvocationRef, StreamRef: q.StreamRef, Generation: 1, OutputWatermark: 1, Status: "FRAME", AuthorizedUntil: time.Now().Add(time.Second), Frame: &entity.KubernetesStreamOutputFrame{StageRef: "kos_synthetic", Sequence: 1, Channel: "STDOUT", Digest: hex.EncodeToString(sum[:]), Payload: body}}
			r := &streamOutputRepositoryFixture{materialRepositoryFixture: &materialRepositoryFixture{}}
			switch name {
			case "invocation":
				v.InvocationRef = "inv_other"
			case "stream":
				v.StreamRef = "kst_other"
			case "generation":
				v.Generation++
			case "cursor":
				v.AfterSequence = 1
				v.Frame.Sequence = 2
				v.OutputWatermark = 2
			case "digest":
				v.Frame.Digest = hex.EncodeToString(make([]byte, 32))
			case "oversize":
				body = bytes.Repeat([]byte("x"), managedkubernetes.MaximumFrameBytes+1)
				v.Frame.Payload = body
			case "expired":
				v.AuthorizedUntil = time.Now().Add(-time.Second)
			case "channel":
				v.Frame.Channel = "STDIN"
			case "unknown":
				v.Status = "SUCCESS"
			case "invented_end":
				v.Status = "ENDED"
			case "partial_error":
				r.err = errs.ErrNotFound
			}
			r.result = v
			s, err := New(r)
			if err != nil {
				t.Fatal(err)
			}
			p := value.Principal{ActorID: "synthetic-actor", AuthorityTenant: "synthetic-tenant", CorrelationRef: "synthetic-trace", CredentialRevision: 1, CallerWorkload: "control-api-gateway", Permission: "platform.integrations.kubernetes.output.read"}
			got, err := s.ReadKubernetesStreamOutput(t.Context(), p, q)
			if name == "valid" {
				if err != nil || got.Frame == nil || !bytes.Equal(got.Frame.Payload, body) {
					t.Fatal("valid output lost", err)
				}
				return
			}
			want := errs.ErrUnavailable
			if name == "partial_error" {
				want = errs.ErrNotFound
			}
			if !errors.Is(err, want) || got.Frame != nil || !bytes.Equal(body, make([]byte, len(body))) {
				t.Fatal("invalid owner output was disclosed or retained", err)
			}
		})
	}
}

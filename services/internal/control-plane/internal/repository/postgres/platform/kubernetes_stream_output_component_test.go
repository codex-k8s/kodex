package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// kubernetesDisclosureStorageFixture observes exact versioned reads and injects a synchronous owner transition during storage I/O.
// kubernetesDisclosureStorageFixture наблюдает exact versioned reads и вводит synchronous owner transition во время storage I/O.
type kubernetesDisclosureStorageFixture struct {
	objectstorage.Store
	gets    int
	corrupt bool
	onGet   func()
}

// Get preserves the real local storage bytes while optionally corrupting provenance or changing owner state before return.
// Get сохраняет реальные local storage bytes, при необходимости портит provenance либо меняет owner state перед возвратом.
func (s *kubernetesDisclosureStorageFixture) Get(ctx context.Context, key, version string) (objectstorage.Object, error) {
	s.gets++
	object, err := s.Store.Get(ctx, key, version)
	if s.corrupt {
		object.Digest = "sha256:invalid"
	}
	if s.onGet != nil {
		f := s.onGet
		s.onGet = nil
		f()
	}
	return object, err
}

// TestKubernetesStreamOutputOwnerComponent checks original-user disclosure of accepted ACKs, old native-credit expiry, exact cursors and revocation during object I/O.
// The real PostgreSQL owner and local versioned store are used; this does not prove live Kubernetes transport or deployed S3.
// TestKubernetesStreamOutputOwnerComponent проверяет выдачу original user только accepted ACKs, expiry старого native credit, exact cursor и отзыв во время object I/O.
// Используются реальные PostgreSQL owner и local versioned store; live Kubernetes transport и deployed S3 этим не доказаны.
func TestKubernetesStreamOutputOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 80*time.Second)
	defer cancel()
	r, s, broker, gateway, binding, _, admission, _ := newKubernetesInvocationStreamResultFixture(t, ctx)
	storage := &kubernetesDisclosureStorageFixture{Store: r.objects}
	r.objects = storage
	actor := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.integrations.kubernetes.output.read"}, "control-api-gateway")
	q := query.KubernetesStreamOutputRead{InvocationRef: binding.InvocationRef, StreamRef: admission.Stream.Ref, Generation: binding.Generation}
	if v, err := s.ReadKubernetesStreamOutput(ctx, actor, q); err != nil || v.Status != "WAITING" || storage.gets != 0 {
		t.Fatal("empty output was not waiting", err)
	}
	body := []byte("confirmed private output")
	sum := sha256.Sum256(body)
	deadline := time.Now().Add(4 * time.Second)
	frame := command.ManagedKubernetesBeginFrame{StreamRef: q.StreamRef, Direction: enum.KubernetesFrameOutput, Sequence: 1, Channel: "STDOUT", PayloadDigest: hex.EncodeToString(sum[:]), ByteCount: uint32(len(body)), NativeAuthorizedUntil: deadline}
	broker.Permission = "platform.runtime.kubernetes.frame.begin"
	credit, err := r.BeginManagedKubernetesFrame(ctx, broker, binding, frame)
	if err != nil {
		t.Fatal("begin output", err)
	}
	gateway.Permission = "platform.runtime.kubernetes.output.accept"
	stage, err := s.AcceptManagedKubernetesOutputFrame(ctx, gateway, binding, command.ManagedKubernetesAcceptOutputFrame{ManagedKubernetesOutputFrameLookup: command.ManagedKubernetesOutputFrameLookup{StreamRef: q.StreamRef, Sequence: 1, DeliveryOrdinal: credit.DeliveryOrdinal}, Channel: frame.Channel, PayloadDigest: frame.PayloadDigest, ByteCount: frame.ByteCount, Payload: body})
	if err != nil || stage.State != enum.KubernetesOutputAccepted {
		t.Fatal("stage output", err)
	}
	if v, err := s.ReadKubernetesStreamOutput(ctx, actor, q); err != nil || v.Status != "WAITING" || v.Frame != nil || storage.gets != 0 {
		t.Fatal("unacknowledged output was disclosed", err)
	}
	broker.Permission = "platform.runtime.kubernetes.frame.complete"
	if _, err := r.CompleteManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesCompleteFrame{StreamRef: q.StreamRef, Direction: enum.KubernetesFrameOutput, Sequence: 1, PayloadDigest: frame.PayloadDigest}); err != nil {
		t.Fatal("ack output", err)
	}
	// Expired native credit is not a data-retention deadline for an independently authorized stored read.
	// Истёкший native credit не является data-retention deadline для отдельно разрешённого stored read.
	timer := time.NewTimer(time.Until(deadline.Add(20 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	v, err := s.ReadKubernetesStreamOutput(ctx, actor, q)
	if err != nil || v.Status != "FRAME" || v.Frame == nil || string(v.Frame.Payload) != string(body) || v.Frame.StageRef != stage.Ref || v.Frame.Sequence != 1 || storage.gets != 1 {
		t.Fatal("confirmed output did not survive native credit expiry", err)
	}
	clear(v.Frame.Payload)
	for _, bad := range []query.KubernetesStreamOutputRead{
		{InvocationRef: q.InvocationRef, StreamRef: q.StreamRef, Generation: q.Generation + 1},
		{InvocationRef: q.InvocationRef, StreamRef: "kst_foreign_stream", Generation: q.Generation},
		{InvocationRef: "inv_foreign_invocation", StreamRef: q.StreamRef, Generation: q.Generation},
	} {
		if v, err := s.ReadKubernetesStreamOutput(ctx, actor, bad); !errors.Is(err, errs.ErrNotFound) || v.Frame != nil || storage.gets != 1 {
			t.Fatal("foreign output origin reached storage", err)
		}
	}
	wrongProject := actor
	wrongProject.ProjectRef = "prj_foreign_project"
	if v, err := s.ReadKubernetesStreamOutput(ctx, wrongProject, q); !errors.Is(err, errs.ErrNotFound) || v.Frame != nil || storage.gets != 1 {
		t.Fatal("foreign project reached output storage", err)
	}
	cursor := q
	cursor.AfterSequence = 1
	if v, err := s.ReadKubernetesStreamOutput(ctx, actor, cursor); err != nil || v.Status != "WAITING" || v.Frame != nil || storage.gets != 1 {
		t.Fatal("cursor replayed prior output", err)
	}
	cursor.AfterSequence = 2
	if _, err := s.ReadKubernetesStreamOutput(ctx, actor, cursor); !errors.Is(err, errs.ErrInvalid) || storage.gets != 1 {
		t.Fatal("future cursor skipped output", err)
	}
	storage.corrupt = true
	if v, err := s.ReadKubernetesStreamOutput(ctx, actor, q); !errors.Is(err, errs.ErrUnavailable) || v.Frame != nil {
		t.Fatal("corrupt storage receipt was disclosed", err)
	}
	storage.corrupt = false
	storage.onGet = func() {
		gateway.Permission = "platform.runtime.kubernetes.cancel"
		if _, err := s.CancelManagedKubernetesInvocation(ctx, gateway, binding); err != nil {
			t.Fatal("stop during object read", err)
		}
	}
	if v, err := s.ReadKubernetesStreamOutput(ctx, actor, q); !errors.Is(err, errs.ErrNotFound) || v.Frame != nil || storage.onGet != nil {
		t.Fatal("owner Stop during storage read leaked bytes", err)
	}
	gets := storage.gets
	if v, err := s.ReadKubernetesStreamOutput(ctx, actor, q); !errors.Is(err, errs.ErrNotFound) || v.Frame != nil || storage.gets != gets {
		t.Fatal("stopped live read reached storage", err)
	}
}

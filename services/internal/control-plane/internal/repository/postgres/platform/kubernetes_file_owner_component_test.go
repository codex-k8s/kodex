package platform

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryFileOwnerCleanupClaimFixture contains only the disposable FILE owner assertion; it is never loaded by production code.
// queryFileOwnerCleanupClaimFixture содержит только disposable FILE owner assertion; production code его не загружает.
//
//go:embed testdata/sql/kubernetes_file_owner_cleanup_claim.sql
var queryFileOwnerCleanupClaimFixture string

// queryFileOwnerCleanupCompleteFixture contains only the disposable FILE owner assertion; it is never loaded by production code.
// queryFileOwnerCleanupCompleteFixture содержит только disposable FILE owner assertion; production code его не загружает.
//
//go:embed testdata/sql/kubernetes_file_owner_cleanup_complete.sql
var queryFileOwnerCleanupCompleteFixture string

// queryFileOwnerSourceVersionFixture contains only the disposable FILE owner assertion; it is never loaded by production code.
// queryFileOwnerSourceVersionFixture содержит только disposable FILE owner assertion; production code его не загружает.
//
//go:embed testdata/sql/kubernetes_file_owner_source_version.sql
var queryFileOwnerSourceVersionFixture string

// queryFileOwnerMetadataPurgeFixture contains only the disposable FILE owner assertion; it is never loaded by production code.
// queryFileOwnerMetadataPurgeFixture содержит только disposable FILE owner assertion; production code его не загружает.
//
//go:embed testdata/sql/kubernetes_file_owner_metadata_purge.sql
var queryFileOwnerMetadataPurgeFixture string

// queryFileOwnerCleanupCountersFixture contains only the disposable FILE owner assertion; it is never loaded by production code.
// queryFileOwnerCleanupCountersFixture содержит только disposable FILE owner assertion; production code его не загружает.
//
//go:embed testdata/sql/kubernetes_file_owner_cleanup_counters.sql
var queryFileOwnerCleanupCountersFixture string

// queryFileOwnerOriginRetention observes only the exact source's retained revision edge.
// RU: queryFileOwnerOriginRetention читает лишь retained revision edge точного source.
//
//go:embed testdata/sql/kubernetes_file_owner_origin_retention.sql
var queryFileOwnerOriginRetention string

// queryFileOwnerReleaseDenied checks that public runtime cannot invoke the private release dispatcher.
// RU: queryFileOwnerReleaseDenied проверяет запрет private release dispatcher для public runtime.
//
//go:embed testdata/sql/kubernetes_file_owner_release_denied.sql
var queryFileOwnerReleaseDenied string

// fileOwnerCountStore counts actual object/range operations while retaining immutable fixture versions.
// It is an in-process versioned store, not an S3 durability or installed AV proof.
// fileOwnerCountStore считает actual object/range операции с immutable fixture versions.
// Это in-process versioned store, не S3 durability либо установленная AV проверка.
type fileOwnerCountStore struct {
	*objectstoragetest.VersionedStore
	puts, ranges, deletes atomic.Int64
}

// Put observes every physical object reservation without changing the fixture storage semantics.
// Put наблюдает каждую physical object запись, не меняет fixture storage semantics.
func (store *fileOwnerCountStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	store.puts.Add(1)
	return store.VersionedStore.Put(ctx, input)
}

// Delete records one simulated external call; a lost completion ACK must never dispatch another.
// Delete фиксирует один simulated external call; потеря completion ACK не разрешает следующий.
func (store *fileOwnerCountStore) Delete(ctx context.Context, key, version string) error {
	store.deletes.Add(1)
	return store.VersionedStore.Delete(ctx, key, version)
}

// DeleteOnce counts the typed local first attempt independently of an ordinary deletion fallback.
// RU: DeleteOnce считает typed local first attempt отдельно от обычного deletion fallback.
func (store *fileOwnerCountStore) DeleteOnce(ctx context.Context, key, version string) error {
	store.deletes.Add(1)
	return store.VersionedStore.DeleteOnce(ctx, key, version)
}

// ReadRange counts owner-pinned bounded slices without reading a substituted head.
// ReadRange считает owner-pinned bounded slices без подстановки нового head.
func (store *fileOwnerCountStore) ReadRange(ctx context.Context, input objectstorage.RangeRead) ([]byte, error) {
	store.ranges.Add(1)
	return store.VersionedStore.ReadRange(ctx, input)
}

// TestKubernetesFileOwnerComponent crosses real artifact/RR/Resolve/Prepare/INPUT and cleanup owner functions.
// It records a synthetic handshake and INPUT receipts, proving owner state, not native execution or remote copy.
// TestKubernetesFileOwnerComponent проходит actual artifact/RR/Resolve/Prepare/INPUT и cleanup owner functions.
// Handshake и INPUT receipts синтетические: проверяется owner state, не native execution либо remote copy.
func TestKubernetesFileOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	fixture, service, connection, bindingMetadata := kubernetesPersonalHealthFixture(t, ctx)
	authenticateKubernetesInvocationFixture(t, ctx, fixture, service, connection, bindingMetadata)
	repository := fixture.repository
	configureComponentArtifactScanner(t, repository)
	store := &fileOwnerCountStore{VersionedStore: objectstoragetest.NewVersioned()}
	repository.objects = store
	seedObservedCatalogFixture(t, ctx, repository)
	execute := newComponentCommandExecutor(t, ctx, service, "kube-file-")
	project := execute(command.CreateProject, fixture.transport, "project", nil, command.ProjectInput{Name: "Disposable private input", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, fixture.transport, project.Ref, "kube-file-agent", "Private input owner")
	agent = *execute(command.ChangeAgentCapability, fixture.transport, "files", &agent.Version, command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	original := strings.Repeat("reviewed immutable file\n", 1500)
	artifact, err := service.UploadArtifact(ctx, fixture.transport, value.Mutation{IdempotencyKey: "file-original"}, port.ArtifactUpload{ProjectRef: project.Ref, FileName: "review.txt", MediaType: "text/plain", SizeBytes: int64(len(original)), Reader: strings.NewReader(original)})
	if err != nil {
		t.Fatal("upload original FILE source", err)
	}
	execute(command.ChangeArtifactBinding, fixture.transport, "file-bind", &artifact.Version, command.ArtifactBindingInput{ArtifactRef: artifact.Ref, AgentRef: agent.Ref, Enabled: true})
	connection = *execute(command.ChangeIntegrationGrant, fixture.transport, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.cp.upload", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true, KubernetesNamespace: "review"}).Connection
	run := execute(command.LaunchRun, fixture.transport, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Write a reviewed private stdin frame"}).Run
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	runtime := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "kube-file-runtime", Limit: 1}).RuntimeItems
	if len(runtime) != 1 {
		t.Fatal("claim input runtime")
	}
	node := stringMap(runtime[0], "nodeRef")
	uid := "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8"
	payload := map[string]any{"group": "", "version": "v1", "resource": "pods", "namespace": "review", "name": "web", "uid": uid, "resourceVersion": "17", "options": map[string]any{"container": "main", "artifactRef": artifact.Ref, "artifactSHA256": strings.TrimPrefix(artifact.Digest, "sha256:"), "transferFormat": "FILE", "remotePath": "/tmp/review.txt", "maximumBytes": 1 << 20, "timeoutSeconds": 120}}
	resolved, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": node, "connection_ref": connection.Ref, "capability_key": "kubernetes.cp.upload", "idempotency_key": "kube-file-invocation"}, payload)
	if err != nil {
		t.Fatal("resolve input invocation", err)
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
		if method == cp.RuntimeWorkService_ClaimIntegrationInvocations_FullMethodName {
			gateway.Permission = operation
		}
	}
	claims, err := service.ClaimIntegrationInvocationsWithCapacity(ctx, gateway, "kube-file-gateway", 1, value.IntegrationInvocationDeliveryCapacity{Kubernetes: 1})
	if err != nil || len(claims) != 1 {
		t.Fatal("claim input invocation", err)
	}
	claim := claims[0]
	binding := value.ManagedKubernetesBinding{WorkloadInstance: "kube-file-gateway", InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
	if binding.InvocationRef != stringMap(resolved, "invocationRef") {
		t.Fatal("claim selected another input invocation")
	}
	broker := fixture.principal
	broker.CallerWorkload = "secret-broker"
	broker.Permission = "platform.runtime.kubernetes.resolve"
	snapshot, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding)
	if err != nil {
		t.Fatal(err)
	}
	candidate := managedkubernetes.ChildCandidate{ObservationState: managedkubernetes.ObservationPresent, Resource: managedkubernetes.ResourceAddress{Version: "v1", Resource: "pods", Namespace: "review", Name: "web", UID: uid, ResourceVersion: "17", Namespaced: true}, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64), PodWitness: &managedkubernetes.PodNodeWitness{NodeName: "synthetic-node", Container: "main"}}
	proposal, err := managedkubernetes.ChildProposalDigest(snapshot.Digest, []managedkubernetes.ChildCandidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	broker.Permission = "platform.runtime.kubernetes.children.prepare"
	plan, err := repository.PrepareManagedKubernetesChildren(ctx, broker, binding, command.ManagedKubernetesPrepareChildren{SnapshotDigest: snapshot.Digest, ProposalDigest: proposal, Candidates: []managedkubernetes.ChildCandidate{candidate}})
	if err != nil || len(plan.Children) != 1 {
		t.Fatal("prepare input child", err)
	}
	child := plan.Children[0]
	opening := command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest}
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	var waiting *platformservice.KubernetesApprovalRequiredError
	if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, opening); !errors.As(err, &waiting) {
		t.Fatal("input child did not require exact approval", err)
	}
	actor := fixture.transport
	actor.Permission = "platform.query.owner-gates.get"
	gate, err := service.GetOwnerGate(ctx, actor, waiting.GateRef)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.ResolveOwnerGate, fixture.transport, "approve", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
	if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, opening); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("FILE effect opened without prepared source", err)
	}
	// Fixture SQL advances one immutable head as in the existing revision component;
	// this checks pinned reads, not the public assistant-plan revision writer.
	// Fixture SQL продвигает immutable head как в существующем revision component;
	// проверяется pinned read, не public assistant-plan revision writer.
	advanceFileOwnerFixtureHead(t, ctx, repository, store, fixture.current, artifact, "a later artifact head")
	newer, err := service.GetArtifact(ctx, fixture.transport, artifact.Ref)
	if err != nil || newer.Ref != artifact.Ref || newer.CurrentRevisionRef == artifact.CurrentRevisionRef {
		t.Fatal("advance fixture artifact head", err)
	}
	// Reuse the existing disposable tracer; SQL, parameters and driver messages remain private.
	// Используется existing disposable tracer; SQL, параметры и driver messages остаются private.
	traceConfig := repository.pool.Config().Copy()
	traceConfig.ConnConfig.Tracer = componentQueryRejectionTracer{t: t}
	tracePool, err := pgxpool.NewWithConfig(ctx, traceConfig)
	if err != nil {
		t.Fatal("open FILE diagnostic pool")
	}
	originalPool := repository.pool
	repository.pool = tracePool
	defer func() {
		repository.pool = originalPool
		tracePool.Close()
	}()
	putsBefore := store.puts.Load()
	broker.Permission = "platform.runtime.kubernetes.file.prepare"
	prepared, err := repository.PrepareManagedKubernetesFileInput(ctx, broker, binding, child.Ref)
	if err != nil || prepared.View.State != "READY" || prepared.View.ArtifactRevisionRef != artifact.CurrentRevisionRef || prepared.View.ArtifactSHA256 != strings.TrimPrefix(artifact.Digest, "sha256:") || store.puts.Load() != putsBefore+1 {
		t.Fatalf("prepare one pinned FILE object: error=%v new_puts=%d context_done=%t", err, store.puts.Load()-putsBefore, ctx.Err() != nil)
	}
	opening.FileSourceRef, opening.FileSourceVersion = prepared.View.SourceRef, prepared.View.Version
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	foreignSource := opening
	foreignSource.FileSourceVersion++
	if _, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, foreignSource); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unprepared FILE version admitted", err)
	}
	admission, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, opening)
	if err != nil || admission.Stream == nil {
		t.Fatal("begin input stream", err)
	}
	stream := admission.Stream.Ref
	bindingDigest, _ := managedkubernetes.BindingDigest(binding)
	pin := child.NodeTransport
	if pin == nil {
		t.Fatal("missing owner-enriched node transport")
	}
	handshake := managedkubernetes.HandshakeReceipt{Origin: managedkubernetes.StreamReceiptOrigin{SnapshotDigest: snapshot.Digest, BindingDigest: bindingDigest, ChildInputDigest: child.InputDigest, EffectRef: admission.Ref, EffectGeneration: admission.Generation, StreamRef: stream, StreamGeneration: binding.Generation, Operation: child.Operation, TargetRef: snapshot.Target.Ref, TargetRevision: snapshot.Target.Revision}, TransportRef: pin.Ref, TransportGeneration: pin.Generation, Protocol: string(pin.Protocol), HTTPStatus: 101, NegotiatedProtocol: pin.Protocol.WireProtocol(), PeerSPKISHA256: pin.ServerPublicKeySHA256}
	hsDigest, err := handshake.Digest()
	if err != nil {
		t.Fatal(err)
	}
	broker.Permission = "platform.runtime.kubernetes.stream.open"
	if _, err := repository.OpenManagedKubernetesStream(ctx, broker, binding, command.ManagedKubernetesOpenStream{StreamRef: stream, EffectRef: admission.Ref, EffectGeneration: admission.Generation, HandshakeReceipt: handshake, HandshakeReceiptDigest: hsDigest}); err != nil {
		t.Fatal("record input handshake", err)
	}
	var normalized []byte
	defer func() { clear(normalized) }()
	claimSource := gateway
	claimSource.Permission = "platform.runtime.kubernetes.input.claim"
	for len(normalized) < int(prepared.View.NormalizedSizeBytes) {
		delivery, claimErr := service.ClaimManagedKubernetesInputFrame(ctx, claimSource, binding, stream)
		if claimErr != nil || delivery.Disposition != enum.KubernetesInputFrameDeliver || delivery.Frame == nil {
			t.Fatal("claim pinned FILE range", claimErr)
		}
		frame := delivery.Frame
		if frame.Channel != "FILE_CONTENT" || len(frame.Payload) == 0 || len(frame.Payload) > managedkubernetes.MaximumFrameBytes ||
			frame.EndOfInput != (len(normalized)+len(frame.Payload) == int(prepared.View.NormalizedSizeBytes)) {
			clear(frame.Payload)
			t.Fatal("FILE final DATA/EOF tuple changed")
		}
		normalized = append(normalized, frame.Payload...)
		clear(frame.Payload)
		if frame.Sequence == 1 {
			ranges := store.ranges.Load()
			again, againErr := service.ClaimManagedKubernetesInputFrame(ctx, claimSource, binding, stream)
			if againErr != nil || again.Disposition != enum.KubernetesInputFrameUnknown || again.Frame != nil || store.ranges.Load() != ranges {
				t.Fatal("uncertain FILE delivery replayed storage bytes", againErr)
			}
		}
		broker.Permission = "platform.runtime.kubernetes.frame.begin"
		admitted, beginErr := repository.BeginManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesBeginFrame{StreamRef: stream, Direction: enum.KubernetesFrameInput, Sequence: frame.Sequence, Channel: frame.Channel, PayloadDigest: frame.PayloadDigest, ByteCount: frame.ByteCount, EndOfInput: frame.EndOfInput})
		if beginErr != nil || admitted.Disposition != enum.KubernetesFrameSend || admitted.ExpiresAt.After(frame.DeliveryExpiresAt) {
			t.Fatal("admit exact FILE staged range", beginErr)
		}
		broker.Permission = "platform.runtime.kubernetes.frame.complete"
		if _, err := repository.CompleteManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesCompleteFrame{StreamRef: stream, Direction: enum.KubernetesFrameInput, Sequence: frame.Sequence, PayloadDigest: frame.PayloadDigest}); err != nil {
			t.Fatal("record FILE exact input receipt", err)
		}
	}
	if store.puts.Load() != putsBefore+1 || store.ranges.Load() < 2 {
		t.Fatal("FILE chunks created separate physical objects")
	}
	archive := tar.NewReader(bytes.NewReader(normalized))
	entry, err := archive.Next()
	if err != nil || entry.Name != "review.txt" || entry.Size != int64(len(original)) {
		t.Fatal("normalized FILE archive metadata", err)
	}
	recovered, err := io.ReadAll(io.LimitReader(archive, int64(len(original))+1))
	defer clear(recovered)
	if err != nil || string(recovered) != original {
		t.Fatal("artifact head replaced pinned bytes", err)
	}
	if _, err := archive.Next(); err != io.EOF {
		t.Fatal("FILE archive contained another entry", err)
	}
	actor.Permission = "platform.integrations.kubernetes.file.get"
	confirmed, err := service.GetKubernetesFileInput(ctx, actor, binding.InvocationRef, prepared.View.SourceRef)
	if err != nil || confirmed.State != "TRANSFERRED" || !confirmed.EOFAcknowledged || confirmed.AcknowledgedBytes != confirmed.NormalizedSizeBytes {
		t.Fatal("read confirmed FILE ACK prefix", err)
	}
	gateway.Permission = "platform.runtime.kubernetes.cancel"
	if _, err := service.CancelManagedKubernetesInvocation(ctx, gateway, binding); err != nil {
		t.Fatal("close FILE owner graph", err)
	}
	config := repository.pool.Config().Copy()
	config.ConnConfig.User = "artifact_retention_runtime_g1"
	config.ConnConfig.Password = ""
	consumer, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open existing retention consumer identity", err)
	}
	defer consumer.Close()
	exerciseKubernetesFileCleanup(t, ctx, repository, service, consumer, actor, project.Ref, binding.InvocationRef, prepared.View.SourceRef, store)
	if store.puts.Load() != putsBefore+1 {
		t.Fatal("FILE cleanup rematerialized source bytes")
	}
	assertFileProjectSSHFKBoundary(t, ctx, repository, fixture.current)
	exerciseFileProjectLifecycle(t, ctx, repository, service, fixture.current, actor, project.Ref, prepared.View.SourceRef, store)
}

// exerciseKubernetesFileCleanup exhausts the existing source budget, repairs with original-key readback and deletes one object.
// It crosses the common SQL dispatcher; the synthetic store does not prove S3 transport or native copy.
// exerciseKubernetesFileCleanup исчерпывает source budget, делает repair с original-key readback и удаляет один объект.
// Проходит common SQL dispatcher; synthetic store не доказывает S3 transport либо native copy.
func exerciseKubernetesFileCleanup(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, consumer *pgxpool.Pool, actor value.Principal, projectRef, invocationRef, sourceRef string, store *fileOwnerCountStore) {
	t.Helper()
	var kind, id, key, version, etag, digest string
	var size, generation int64
	var uncertain bool
	// assertOrigin observes metadata, never reading the original artifact bytes.
	// RU: assertOrigin читает metadata, исключая исходные artifact bytes.
	assertOrigin := func(expected bool) {
		t.Helper()
		var pinned, retained bool
		if err := repository.pool.QueryRow(ctx, queryFileOwnerOriginRetention, sourceRef).Scan(&pinned, &retained); err != nil || pinned != expected || retained != expected {
			t.Fatal("FILE origin retention boundary changed", err)
		}
	}
	assertOrigin(true)
	var released int
	err := repository.pool.QueryRow(ctx, queryFileOwnerReleaseDenied).Scan(&released)
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatal("public runtime could release FILE origin")
	}

	// claim waits only for the original input disclosure lease to expire; it never invents an owner clock.
	// claim ждёт лишь истечения original input disclosure lease; owner clock не подменяет.
	claim := func() {
		t.Helper()
		deadline := time.Now().Add(7 * time.Second)
		for {
			err := consumer.QueryRow(ctx, queryFileOwnerCleanupClaimFixture, "file-component-cleanup").Scan(&kind, &id, &key, &version, &etag, &digest, &size, &generation, &uncertain)
			if err == nil {
				break
			}
			if !errors.Is(err, pgx.ErrNoRows) || !time.Now().Before(deadline) {
				t.Fatal("claim FILE source cleanup", err)
			}
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				t.Fatal("cancel FILE cleanup wait")
			}
		}
		if kind != "KUBERNETES_FILE_SOURCE" || size < 1 || version == "" || etag == "" {
			t.Fatal("cleanup lost exact FILE receipt")
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		claim()
		if uncertain != (attempt > 0) {
			t.Fatal("FILE delete admission replayed across worker claims")
		}
		if attempt == 0 {
			if err := store.DeleteOnce(ctx, key, version); err != nil {
				t.Fatal("single FILE delete attempt", err)
			}
		}
		// The native Delete succeeded but its ACK is deliberately unknown to the owner.
		// Native Delete завершился, но его ACK намеренно неизвестен owner.
		var accepted bool
		if err := consumer.QueryRow(ctx, queryFileOwnerCleanupCompleteFixture, id, "file-component-cleanup", generation, version, etag, false).Scan(&accepted); err != nil || !accepted {
			t.Fatal("retain failed FILE deletion", err)
		}
	}
	assertFileProjectTrashBlocked(t, ctx, repository, service, actor, projectRef)
	actor.Permission = "platform.integrations.kubernetes.file.get"
	read := query.KubernetesFileCleanupRead{InvocationRef: invocationRef, SourceRef: sourceRef}
	before, err := service.GetKubernetesFileCleanup(ctx, actor, read)
	if err != nil || before.View.Cleanup.ExhaustedCount != 1 {
		t.Fatal("discover exhausted FILE cleanup", err)
	}
	var materialVersion int64
	if err := repository.pool.QueryRow(ctx, queryFileOwnerSourceVersionFixture, sourceRef).Scan(&materialVersion); err != nil {
		t.Fatal(err)
	}
	actor.Permission = kubernetesFileCleanupRepairOperation
	mutation := value.Mutation{IdempotencyKey: "file-repair-original", ExpectedVersion: &before.View.Cleanup.Version}
	repaired, err := service.RepairKubernetesFileCleanup(ctx, actor, mutation, command.KubernetesFileCleanupRepair{InvocationRef: invocationRef, SourceRef: sourceRef})
	if err != nil || repaired.Receipt == nil || repaired.Receipt.RepairedCount != 1 || repaired.Receipt.RepairGeneration != 1 {
		t.Fatal("repair exact FILE cleanup", err)
	}
	actor.Permission = "platform.integrations.kubernetes.file.get"
	read.CleanupIdempotencyKey = mutation.IdempotencyKey
	rejoined, err := service.GetKubernetesFileCleanup(ctx, actor, read)
	if err != nil || rejoined.Receipt == nil || *rejoined.Receipt != *repaired.Receipt {
		t.Fatal("recover FILE repair ACK without another POST", err)
	}
	claim()
	if !uncertain {
		t.Fatal("explicit repair reminted FILE DELETE credit")
	}
	if _, err := store.Head(ctx, key, version); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("exact FILE reconciliation did not observe absence", err)
	}
	var accepted bool
	if err := consumer.QueryRow(ctx, queryFileOwnerCleanupCompleteFixture, id, "file-component-cleanup", generation-1, version, etag, true).Scan(&accepted); err != nil || accepted {
		t.Fatal("stale FILE cleanup generation accepted", err)
	}
	if store.deletes.Load() != 1 {
		t.Fatal("FILE repair repeated uncertain external DELETE")
	}
	if err := consumer.QueryRow(ctx, queryFileOwnerCleanupCompleteFixture, id, "file-component-cleanup", generation, version, etag, true).Scan(&accepted); err != nil || !accepted {
		t.Fatal("close FILE deletion proof", err)
	}
	assertOrigin(true)
	// The next existing worker claim performs invocation->sources->origin metadata release.
	// RU: Следующий existing worker claim выполняет metadata release в порядке invocation->sources->origin.
	err = consumer.QueryRow(ctx, queryFileOwnerCleanupClaimFixture, "file-component-cleanup").Scan(&kind, &id, &key, &version, &etag, &digest, &size, &generation, &uncertain)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("terminal FILE release issued another object claim", err)
	}
	assertOrigin(false)
	var count int64
	if err := repository.pool.QueryRow(ctx, queryFileOwnerMetadataPurgeFixture, sourceRef).Scan(&count); err != nil || count != 0 {
		t.Fatal("FILE metadata purge was not atomic", err)
	}
	var currentVersion, attempts, total int64
	if err := repository.pool.QueryRow(ctx, queryFileOwnerCleanupCountersFixture, sourceRef).Scan(&currentVersion, &attempts, &total); err != nil || currentVersion != materialVersion || attempts != 1 || total != 4 {
		t.Fatal("FILE cleanup changed execution version or lost attempts", err)
	}
}

// advanceFileOwnerFixtureHead reuses real immutable revision/content/head SQL with one new local receipt.
// It changes disposable fixture state only and neither publishes an assistant plan nor claims that API proof.
// RU: advanceFileOwnerFixtureHead использует immutable revision/content/head SQL с новым local receipt.
// Меняет только disposable fixture state, не публикует assistant plan и не утверждает proof этого API.
func advanceFileOwnerFixtureHead(t *testing.T, ctx context.Context, repository *Repository, store *fileOwnerCountStore, current scope, artifact entity.Artifact, body string) {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	digest := fmt.Sprintf("sha256:%x", sum)
	scan, err := repository.scanArtifact(ctx, strings.NewReader(body), digest, int64(len(body)))
	if err != nil {
		t.Fatal("scan fixture revision", err)
	}
	scanJSON, err := artifactAntivirusJSON(scan)
	if err != nil {
		t.Fatal(err)
	}
	key := artifactObjectKey(current.organizationRef, current.actorRef, artifact.ProjectRef, artifact.Ref, digest)
	receipt, err := store.Put(ctx, objectstorage.PutInput{Key: key, MediaType: artifact.MediaType, Digest: digest, SizeBytes: int64(len(body)), Body: strings.NewReader(body)})
	if err != nil {
		t.Fatal("write exact fixture revision", err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var id string
	if err := tx.QueryRow(ctx, immutableNextRevision, artifact.Ref, int64(len(body)), digest).Scan(&id); err != nil {
		t.Fatal("insert fixture revision", err)
	}
	if _, err := tx.Exec(ctx, queryArtifactRevisionCreateContent, id, receipt.Key, receipt.VersionID, receipt.ETag, receipt.Digest, receipt.SizeBytes, scanJSON); err != nil {
		t.Fatal("bind fixture receipt", err)
	}
	tag, err := tx.Exec(ctx, immutableAdvance, artifact.Ref, id)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatal("advance exact fixture revision", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit fixture revision", err)
	}
}

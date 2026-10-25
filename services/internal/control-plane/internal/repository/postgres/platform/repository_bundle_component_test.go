package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryRepositoryBundleFixtureState reads only exact owner metadata and retention predicates; it never fabricates business state.
// queryRepositoryBundleFixtureState читает лишь exact owner metadata и retention predicates, не подготавливая бизнес-состояние SQL-записью.
//
//go:embed testdata/sql/repository_bundle_fixture_state.sql
var queryRepositoryBundleFixtureState string

// bundleFixtureObjects witnesses every object write/delete after source capture; publication must reuse immutable objects without cleanup effects.
// bundleFixtureObjects фиксирует каждый object write/delete после capture; publication переиспользует immutable objects без cleanup effects.
type bundleFixtureObjects struct {
	objectstorage.Store
	puts, deletes int
}

// Put forwards the native upload and records its external-effect count without retaining request bodies.
// Put передаёт native upload и учитывает external effects, не сохраняя request bodies.
func (s *bundleFixtureObjects) Put(ctx context.Context, in objectstorage.PutInput) (objectstorage.Receipt, error) {
	s.puts++
	return s.Store.Put(ctx, in)
}

// Delete records deletion attempts so unknown or replayed owner transactions cannot silently discard source material.
// Delete учитывает попытки удаления, чтобы unknown/replayed owner transactions не удаляли source material незаметно.
func (s *bundleFixtureObjects) Delete(ctx context.Context, key, version string) error {
	s.deletes++
	return s.Store.Delete(ctx, key, version)
}

// repositoryBundleFixture owns one isolated native producer graph and synthetic storage/AV; it never contacts a model, Kubernetes or GitHub.
// repositoryBundleFixture владеет одним isolated native producer graph и synthetic storage/AV; model, Kubernetes и GitHub не вызываются.
type repositoryBundleFixture struct {
	parentRun           entity.Run
	parentOrigin        map[string]any
	originResultSummary string
	t                   *testing.T
	ctx                 context.Context
	pool                *pgxpool.Pool
	r                   *Repository
	service             *serviceplatform.Service
	owner, worker       value.Principal
	connection          entity.IntegrationConnection
	agent               entity.Agent
	project             string
	run                 entity.Run
	origin              map[string]any
	input               port.RepositoryBundlePrepare
	proposal            entity.RepositoryBundlePublication
	archive             []byte
	store               *bundleFixtureObjects
	initialPuts         int
}

// newRepositoryBundleFixture builds sources through actual upload/capture and resolves prepare authority from a real immutable execution lease.
// newRepositoryBundleFixture создаёт sources настоящими upload/capture и разрешает prepare authority из actual immutable execution lease.
func newRepositoryBundleFixture(t *testing.T) *repositoryBundleFixture {
	return newRepositoryBundleFixtureMode(t, false)
}

// newRepositoryBundleFixtureMode additionally creates a native delegated child with a grantless manager when requested; only the child receives the publication grant and source bindings.
// newRepositoryBundleFixtureMode при запросе создаёт native delegated child с manager без Git grant; publication grant и source bindings получает лишь child.
func newRepositoryBundleFixtureMode(t *testing.T, delegated bool) *repositoryBundleFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 95*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	r := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	configureComponentArtifactScanner(t, r)
	store := &bundleFixtureObjects{Store: r.objects}
	r.objects = store
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, r)
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	f := &repositoryBundleFixture{t: t, ctx: ctx, pool: pool, r: r, service: service, store: store}
	f.owner = resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
	f.worker = f.principal("runtime-controller", "platform.runtime.execution.claim")
	f.connection = *f.execute(command.CreateConnection, "connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "Bundle fixture", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}).Connection
	if _, err = pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, f.connection.Ref); err != nil {
		t.Fatal("seed synthetic credential metadata", err)
	}
	f.project = f.execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Bundle fixture", Language: "en"}).Project.Ref
	f.agent = createLifecycleAgent(t, ctx, service, f.owner, f.project, "bundle-agent", "Bundle author")
	connection, err := service.GetIntegrationConnection(ctx, f.owner, f.connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	f.connection = *f.execute(command.ChangeIntegrationGrant, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: repositorybundle.Operation, AgentRef: f.agent.Ref, Enabled: true, ApprovalPolicy: "HUMAN_EACH_EFFECT"}).Connection
	files := make([]repositorybundle.File, 108)
	for i := range files {
		body := []byte("package fixture\n// " + strings.Repeat("source ", 382) + "\n")
		files[i] = repositorybundle.File{Entry: repositorybundle.Entry{Path: fmt.Sprintf("source/file-%03d.go", i), Mode: "100644"}, Bytes: body}
	}
	archive, manifest, err := repositorybundle.CanonicalContainer(files)
	if err != nil {
		t.Fatal(err)
	}
	f.archive = archive
	artifacts := make([]entity.Artifact, 2)
	for i, raw := range [][]byte{archive, manifest} {
		artifacts[i], err = service.UploadArtifact(ctx, f.owner, value.Mutation{IdempotencyKey: fmt.Sprintf("bundle-source-%d", i)}, port.ArtifactUpload{ProjectRef: f.project, FileName: fmt.Sprintf("bundle-source-%d.json", i), MediaType: "application/json", SizeBytes: int64(len(raw)), Reader: bytes.NewReader(raw)})
		if err != nil || artifacts[i].ScanState != "CLEAN" {
			t.Fatal("native CLEAN upload", err)
		}
	}

	target := f.agent
	if delegated {
		f.agent = *f.execute(command.ChangeAgentCapability, "writer-delegate", &f.agent.Version, command.AgentBindingInput{AgentRef: f.agent.Ref, BindingRef: "platform.run.delegate", Enabled: true}).Agent
		f.agent = *f.execute(command.ChangeAgentCapability, "writer-files", &f.agent.Version, command.AgentBindingInput{AgentRef: f.agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
		for i, a := range artifacts {
			f.execute(command.ChangeArtifactBinding, fmt.Sprintf("source-binding-%d", i), &a.Version, command.ArtifactBindingInput{ArtifactRef: a.Ref, AgentRef: f.agent.Ref, Enabled: true})
		}
		target = *f.execute(command.CreateAgent, "manager", nil, command.AgentInput{ProjectRef: f.project, Name: "Bundle manager", Purpose: "Receive exact native child outcome", RoleDescription: "Manager without Git grant", Instructions: "Delegate once and use owner receipts", InitialCapabilities: []string{"platform.run.delegate", runtimecontract.ArtifactCapability}}).Agent
	}
	set := finalizedAttachmentSetRef(t, ctx, service, f.owner, f.project, "RUN_INPUT", "bundle-input", artifacts[0].Ref, artifacts[1].Ref)
	f.run = *f.execute(command.LaunchRun, "launch", nil, command.LaunchRunInput{ProjectRef: f.project, Title: "Bundle producer", Task: "Prepare exact captured CLEAN sources", Target: entity.RunTarget{Type: "AGENT", Ref: target.Ref}, AttachmentSetRef: set}).Run
	result, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: f.worker, Mutation: value.Mutation{IdempotencyKey: "bundle-origin-claim"}, Payload: command.LeaseInput{WorkloadInstance: "bundle-origin", Limit: 1}})
	if err != nil || len(result.RuntimeItems) != 1 {
		t.Fatal("native origin claim", err)
	}
	f.origin = result.RuntimeItems[0]
	if delegated {
		f.parentRun, f.parentOrigin = f.run, f.origin
		child, err := service.Execute(ctx, command.Command{Kind: command.DelegateExecution, Principal: f.worker, Mutation: value.Mutation{IdempotencyKey: "bundle-delegate"}, Payload: command.DelegateInput{LeaseRef: stringMap(f.parentOrigin, "leaseRef"), Fence: stringMap(f.parentOrigin, "fence"), Generation: runtimeRevisionMapInt64(f.parentOrigin, "generation"), TargetAgentRef: f.agent.Ref, Task: "Publish the exact source bundle through the owner gate"}})
		if err != nil || child.Run == nil {
			t.Fatal("native child delegation", err)
		}
		f.run = *child.Run
		completeClaimedExecutionWithSummary(t, ctx, service, f.worker, f.parentOrigin, "bundle-manager-initial", false, "Await the child's authoritative publication outcome")
		result, err = service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: f.worker, Mutation: value.Mutation{IdempotencyKey: "bundle-child-claim"}, Payload: command.LeaseInput{WorkloadInstance: "bundle-child", Limit: 1}})
		if err != nil || len(result.RuntimeItems) != 1 || stringMap(result.RuntimeItems[0], "runRef") != f.run.Ref {
			t.Fatal("native delegated child claim", err)
		}
		f.origin = result.RuntimeItems[0]
	}
	f.input = port.RepositoryBundlePrepare{LeaseRef: stringMap(f.origin, "leaseRef"), Fence: stringMap(f.origin, "fence"), Generation: runtimeRevisionMapInt64(f.origin, "generation"), GrantRef: f.connection.Grants[0].Ref, BaseCommitSHA: strings.Repeat("a", 40), IdempotencyKey: "bundle-prepare-exact"}
	pins := []*entity.RepositoryBundleArtifactPin{&f.input.Archive, &f.input.Manifest}
	for i, a := range artifacts {
		download, err := service.ReadExecutionArtifact(ctx, f.principal("runtime-controller", "platform.runtime.execution.artifact.read"), f.input.LeaseRef, f.input.Fence, f.input.Generation, a.Ref)
		if err != nil {
			t.Fatal("captured source", err)
		}
		_ = download.Reader.Close()
		a = download.Artifact
		*pins[i] = entity.RepositoryBundleArtifactPin{Ref: a.Ref, Revision: a.Revision, Version: a.Version, Size: a.SizeBytes, SHA256: strings.TrimPrefix(a.Digest, "sha256:")}
	}
	f.initialPuts = store.puts
	return f
}

// principal derives an exact service-method proof using only the existing synthetic installation identity.
// principal выводит exact service-method proof лишь через существующую synthetic installation identity.
func (f *repositoryBundleFixture) principal(workload, permission string) value.Principal {
	f.t.Helper()
	return resolvedTestPrincipal(f.t, f.ctx, f.r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: workload, Operation: permission}, workload)
}

// execute uses fresh owner command authority and stable per-case mutation keys; callers explicitly supply OCC versions.
// execute использует owner command authority и stable per-case mutation keys; caller явно передаёт OCC versions.
func (f *repositoryBundleFixture) execute(kind command.Kind, key string, version *int64, payload any) command.Result {
	f.t.Helper()
	r, err := f.service.Execute(f.ctx, command.Command{Kind: kind, Principal: f.owner, Mutation: value.Mutation{IdempotencyKey: "bundle-" + key, ExpectedVersion: version}, Payload: payload})
	if err != nil {
		f.t.Fatalf("native %s/%s: %v", kind, key, err)
	}
	return r
}

// prepare creates the actual native invocation/gate and proves exact replay without a second source upload.
// prepare создаёт actual native invocation/Gate и проверяет exact replay без повторного source upload.
func (f *repositoryBundleFixture) prepare() {
	f.t.Helper()
	p := f.principal("runtime-controller", "platform.repository-bundles.prepare")
	var err error
	f.proposal, err = f.service.PrepareRepositoryBundle(f.ctx, p, f.input)
	if err != nil {
		f.t.Fatal("native prepare", err)
	}
	replayed, err := f.service.PrepareRepositoryBundle(f.ctx, p, f.input)
	if err != nil || replayed.Ref != f.proposal.Ref || replayed.Version != f.proposal.Version {
		f.t.Fatal("prepare replay changed owner graph", err)
	}
	if f.proposal.OriginRunRef != f.run.Ref || f.proposal.OriginRuntimeRevisionRef != stringMap(f.origin, "runtimeRevisionRef") || f.proposal.FileCount != 108 {
		f.t.Fatal("prepare lost immutable origin/full manifest")
	}
	f.noObjectEffects()
}

// decide resolves the current native gate once through owner OCC, retaining an exact rejection instruction for continuation checks.
// decide разрешает текущий native Gate через owner OCC, сохраняя exact rejection instruction для проверки continuation.
func (f *repositoryBundleFixture) decide(decision string) {
	f.t.Helper()
	gate, err := f.service.GetOwnerGate(f.ctx, f.owner, f.proposal.GateRef)
	if err != nil {
		f.t.Fatal(err)
	}
	f.execute(command.ResolveOwnerGate, "decision", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: decision, Comment: "Preserve exact source pins. Do not repeat WRITE."})
}

// completeOrigin finishes only the pinned original lease; callbacks must never substitute another run, turn, attempt or revision.
// completeOrigin завершает лишь pinned original lease; callback не подменяет другой run, turn, attempt либо revision.
func (f *repositoryBundleFixture) completeOrigin() {
	f.t.Helper()
	_, err := f.service.Execute(f.ctx, command.Command{Kind: command.CompleteExecution, Principal: f.worker, Mutation: value.Mutation{IdempotencyKey: "bundle-origin-complete"}, Payload: command.CompleteExecutionInput{LeaseRef: f.input.LeaseRef, Fence: f.input.Fence, Generation: f.input.Generation, Success: true, ResultSummary: nonemptyBundleFixtureSummary(f.originResultSummary), Usage: turnUsageFixture()}})
	if err != nil {
		f.t.Fatal("complete origin", err)
	}
}

// claim takes the specialized work, excluding the ordinary integration worker, and returns synthetic candidate metadata without asserting a live Git fact.
// claim получает specialized work, исключая ordinary integration worker, и возвращает synthetic candidate metadata без утверждения live Git fact.
func (f *repositoryBundleFixture) claim() port.RepositoryBundleEffect {
	f.t.Helper()
	generic, err := f.service.ClaimIntegrationInvocations(f.ctx, f.principal("integration-gateway", "platform.runtime.integrations.claim"), "bundle-generic", 1)
	if err != nil || len(generic) != 0 {
		f.t.Fatal("generic claim selected specialized publication", err)
	}
	items, err := f.service.ClaimRepositoryBundles(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.claim"), "bundle-private", 1)
	if err != nil || len(items) != 1 || items[0].Mode != "EXECUTE" || items[0].Proposal.Ref != f.proposal.Ref {
		f.t.Fatal("specialized native claim", err)
	}
	w := items[0]
	download, err := f.service.ReadRepositoryBundle(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.content.read"), w.Lease)
	if err != nil {
		f.t.Fatal("exact private read", err)
	}
	body, err := io.ReadAll(download.Reader)
	_ = download.Reader.Close()
	if err != nil || !bytes.Equal(body, f.archive) {
		f.t.Fatal("private read substituted bytes", err)
	}
	return port.RepositoryBundleEffect{Lease: w.Lease, CandidateCommitSHA: strings.Repeat("b", 40), CandidateTreeSHA: strings.Repeat("c", 40), BaseCommitSHA: f.input.BaseCommitSHA, ManifestSHA256: f.input.Manifest.SHA256, ArchiveSHA256: f.input.Archive.SHA256}
}

// begin proves one new intent ACK and a read-only duplicate ACK; neither call performs external Git work.
// begin доказывает один new intent ACK и read-only duplicate ACK; ни один вызов не выполняет external Git work.
func (f *repositoryBundleFixture) begin(effect port.RepositoryBundleEffect) {
	f.t.Helper()
	p := f.principal("integration-gateway", "platform.repository-bundles.work.begin")
	_, replay, err := f.service.BeginRepositoryBundle(f.ctx, p, effect)
	if err != nil || replay {
		f.t.Fatal("begin intent", err)
	}
	_, replay, err = f.service.BeginRepositoryBundle(f.ctx, p, effect)
	if err != nil || !replay {
		f.t.Fatal("begin ACK replay authorized another write", err)
	}
}

// complete proves immutable candidate receipt replay; a changed candidate can never reuse the successful acknowledgement.
// complete проверяет immutable candidate receipt replay; изменённый candidate не переиспользует successful acknowledgement.
func (f *repositoryBundleFixture) complete(effect port.RepositoryBundleEffect) {
	f.t.Helper()
	p := f.principal("integration-gateway", "platform.repository-bundles.work.complete")
	a, err := f.service.CompleteRepositoryBundle(f.ctx, p, effect)
	if err != nil || a.State != "SUCCEEDED" {
		f.t.Fatal("complete publication", err)
	}
	b, err := f.service.CompleteRepositoryBundle(f.ctx, p, effect)
	if err != nil || b.Version != a.Version {
		f.t.Fatal("completion replay", err)
	}
	wrong := effect
	wrong.CandidateCommitSHA = strings.Repeat("d", 40)
	if _, err = f.service.CompleteRepositoryBundle(f.ctx, p, wrong); !errors.Is(err, errs.ErrConflict) {
		f.t.Fatal("changed candidate receipt accepted", err)
	}
	f.noObjectEffects()
}

// noObjectEffects establishes that owner prepare/replay/UNKNOWN never puts or cleans up existing immutable source bodies.
// noObjectEffects устанавливает, что owner prepare/replay/UNKNOWN не выполняет Put либо cleanup existing immutable source bodies.
func (f *repositoryBundleFixture) noObjectEffects() {
	f.t.Helper()
	if f.store.puts != f.initialPuts || f.store.deletes != 0 {
		f.t.Fatal("publication mutated source object custody")
	}
}

// graph requires exact continuation cardinality and prevents success before the dependent publication has settled.
// graph требует exact continuation cardinality и запрещает success до завершения зависимой publication.
func (f *repositoryBundleFixture) graph(continuations int) {
	f.t.Helper()
	run, graph, err := f.service.GetRunGraph(f.ctx, f.owner, f.run.Ref)
	if err != nil {
		f.t.Fatal(err)
	}
	n := 0
	for _, edge := range graph.Edges {
		if edge.Type == "CONTINUES" {
			n++
		}
	}
	if n != continuations || run.State == "SUCCEEDED" {
		f.t.Fatalf("premature terminal or continuation count: %s/%d expected %d", run.State, n, continuations)
	}
}

// TestRepositoryBundlePublicationComponent exercises only the new operation's native owner graph; synthetic AV/storage/provider receipts do not constitute live Git or antivirus acceptance.
// TestRepositoryBundlePublicationComponent проверяет лишь native owner graph новой operation; synthetic AV/storage/provider receipts не являются live Git либо antivirus acceptance.
func TestRepositoryBundlePublicationComponent(t *testing.T) {
	t.Run("delegated-nested/oversize-presentation", testRepositoryBundleOversizeCallback)
	t.Run("delegated/late-disable", testRepositoryBundleLateDisable)
	for _, order := range []string{"nested-child-first", "effect-first"} {
		t.Run("delegated-nested/"+order, func(t *testing.T) { testRepositoryBundleNestedOrder(t, order) })
	}
	for _, order := range []string{"origin-first", "effect-first", "settled-cancelled-agent-disabled", "settled-failed"} {
		t.Run("delegated/"+order, func(t *testing.T) { testRepositoryBundleDelegatedOrder(t, order) })
	}
	for _, order := range []string{"origin-first", "effect-first", "reject-before-origin"} {
		t.Run(order, func(t *testing.T) {
			f := newRepositoryBundleFixture(t)
			f.prepare()
			f.graph(0)
			if order == "origin-first" {
				f.completeOrigin()
				f.graph(0)
			}
			if order == "reject-before-origin" {
				f.decide("REJECT")
				f.graph(0)
				f.completeOrigin()
				f.graph(1)
				return
			}
			f.decide("APPROVE")
			effect := f.claim()
			f.begin(effect)
			f.complete(effect)
			if order == "effect-first" {
				f.graph(0)
				f.completeOrigin()
			}
			f.graph(1)
			f.complete(effect)
			f.graph(1)
			continuation, err := f.service.Execute(f.ctx, command.Command{Kind: command.ClaimExecution, Principal: f.worker, Mutation: value.Mutation{IdempotencyKey: "bundle-continuation"}, Payload: command.LeaseInput{WorkloadInstance: "bundle-continuation", Limit: 1}})
			if err != nil || len(continuation.RuntimeItems) != 1 {
				t.Fatal("continuation claim", err)
			}
			next := continuation.RuntimeItems[0]
			if stringMap(next, "runRef") != f.run.Ref || stringMap(next, "sessionRef") != stringMap(f.origin, "sessionRef") || stringMap(next, "turnRef") == stringMap(f.origin, "turnRef") || stringMap(next, "runtimeRevisionRef") == stringMap(f.origin, "runtimeRevisionRef") || runtimeRevisionMapInt64(next, "attempt") != runtimeRevisionMapInt64(f.origin, "attempt")+1 {
				t.Fatal("continuation changed exact run/session/turn/attempt lineage")
			}
		})
	}

	t.Run("admission-and-complete-manifest", func(t *testing.T) {
		f := newRepositoryBundleFixture(t)
		prepare := f.principal("runtime-controller", "platform.repository-bundles.prepare")
		invalid := f.input
		invalid.Archive.Revision++
		if _, err := f.service.PrepareRepositoryBundle(f.ctx, prepare, invalid); err == nil {
			t.Fatal("uncaptured source revision admitted")
		}
		invalid = f.input
		invalid.Generation++
		if _, err := f.service.PrepareRepositoryBundle(f.ctx, prepare, invalid); err == nil {
			t.Fatal("foreign execution epoch admitted")
		}
		invalid = f.input
		invalid.GrantRef = "grt_foreignfixture000"
		if _, err := f.service.PrepareRepositoryBundle(f.ctx, prepare, invalid); err == nil {
			t.Fatal("absent exact grant admitted")
		}
		f.prepare()
		invalid = f.input
		invalid.BaseCommitSHA = strings.Repeat("f", 40)
		if _, err := f.service.PrepareRepositoryBundle(f.ctx, prepare, invalid); !errors.Is(err, errs.ErrIdempotencyReuse) {
			t.Fatal("semantic reuse accepted", err)
		}
		owner := resolvedTestPrincipal(t, f.ctx, f.r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.repository-bundles.get"}, "control-api-gateway")
		input := port.RepositoryBundleRead{GateRef: f.proposal.GateRef, Limit: 32}
		var entries []repositorybundle.Entry
		for pages := 0; pages < 8; pages++ {
			page, err := f.service.GetRepositoryBundle(f.ctx, owner, input)
			if err != nil {
				t.Fatal("native manifest page", err)
			}
			if page.Proposal.Ref != f.proposal.Ref || page.Offset != input.Offset || page.NextOffset != input.Offset+int32(len(page.Files)) {
				t.Fatal("mixed/truncated manifest")
			}
			entries = append(entries, page.Files...)
			if page.Complete {
				break
			}
			input.Offset = page.NextOffset
			input.ExpectedVersion = page.Proposal.Version
			input.ExpectedManifestSHA256 = page.Proposal.Manifest.SHA256
		}
		raw, err := marshalBundleFixtureManifest(entries)
		if err != nil || len(entries) != 108 || repositorybundle.Digest(raw) != f.input.Manifest.SHA256 {
			t.Fatal("full immutable manifest differs", err)
		}
		input.Offset = 1
		input.ExpectedVersion = f.proposal.Version + 1
		if _, err := f.service.GetRepositoryBundle(f.ctx, owner, input); !errors.Is(err, errs.ErrVersionMismatch) {
			t.Fatal("stale page accepted", err)
		}
		other := f.execute(command.CreateProject, "foreign-project", nil, command.ProjectInput{Name: "Other bundle owner", Language: "en"}).Project
		foreign := owner
		foreign.ProjectRef = gateTestProjectID(t, f.ctx, f.r, f.owner, other.Ref)
		if _, err := f.service.GetRepositoryBundle(f.ctx, foreign, port.RepositoryBundleRead{GateRef: f.proposal.GateRef, Limit: 32}); !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("valid foreign project read admitted", err)
		}
		f.noObjectEffects()
	})
	t.Run("unknown-retains-source-readonly-recovery-fences", testRepositoryBundleUnknownRecovery)

	for _, source := range []string{"grant", "connection", "agent", "artifact", "root"} {
		for _, begun := range []bool{false, true} {
			t.Run(fmt.Sprintf("close-%s/begun-%t", source, begun), func(t *testing.T) {
				f := newRepositoryBundleFixture(t)
				f.prepare()
				f.decide("APPROVE")
				effect := f.claim()
				if begun {
					f.begin(effect)
				}
				switch source {
				case "grant", "connection":
					c, err := f.service.GetIntegrationConnection(f.ctx, f.owner, f.connection.Ref)
					if err != nil {
						t.Fatal(err)
					}
					if source == "grant" {
						f.execute(command.ChangeIntegrationGrant, "disable-grant", &c.Version, command.IntegrationGrantInput{ConnectionRef: c.Ref, CapabilityKey: repositorybundle.Operation, AgentRef: f.agent.Ref, Enabled: false, ApprovalPolicy: "HUMAN_EACH_EFFECT"})
					} else {
						f.execute(command.SetConnectionEnabled, "disable-connection", &c.Version, command.ConnectionInput{Ref: c.Ref, Enabled: false})
					}
				case "agent":
					a, err := f.service.GetAgent(f.ctx, f.owner, f.agent.Ref)
					if err != nil {
						t.Fatal(err)
					}
					_, archiveErr := f.service.Execute(f.ctx, command.Command{Kind: command.ArchiveAgent, Principal: f.owner, Mutation: value.Mutation{IdempotencyKey: "bundle-active-archive", ExpectedVersion: &a.Version}, Payload: command.AgentInput{Ref: a.Ref}})
					if !errors.Is(archiveErr, errs.ErrConflict) {
						t.Fatal("active-run archive guard changed", archiveErr)
					}
					fresh, readErr := f.service.GetAgent(f.ctx, f.owner, a.Ref)
					if readErr != nil || fresh.Version != a.Version || fresh.State == "ARCHIVED" {
						t.Fatal("rejected archive changed agent", readErr)
					}
					f.execute(command.SetAgentEnabled, "disable-agent", &fresh.Version, command.AgentInput{Ref: fresh.Ref, Enabled: false})
				case "artifact":
					a, err := f.service.GetArtifact(f.ctx, f.owner, f.input.Archive.Ref)
					if err != nil {
						t.Fatal(err)
					}
					impact, err := f.service.GetArtifactImpact(f.ctx, f.owner, a.Ref, "DELETE")
					if err != nil {
						t.Fatal(err)
					}
					f.execute(command.DeleteArtifact, "delete-source", &a.Version, command.ArtifactLifecycleInput{ArtifactRef: a.Ref, ImpactDigest: impact.Digest})
				case "root":
					run, err := f.service.GetRun(f.ctx, f.owner, f.run.Ref)
					if err != nil {
						t.Fatal(err)
					}
					f.execute(command.CancelRun, "cancel-root", &run.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Fixture terminal boundary"})
				}
				var state, invocation string
				var retained bool
				var activeLeases int
				if err := f.pool.QueryRow(f.ctx, queryRepositoryBundleFixtureState, f.proposal.Ref).Scan(&state, &invocation, &retained, &activeLeases); err != nil {
					t.Fatal("read closure/retention metadata", err)
				}
				want := "CANCELLED"
				if begun {
					want = "UNKNOWN_OUTCOME"
				}
				wantInvocation := "FAILED"
				if source == "root" {
					wantInvocation = "CANCELLED"
				}
				if begun {
					wantInvocation = "UNKNOWN_OUTCOME"
				}
				if invocation != wantInvocation {
					t.Fatalf("native invocation closure mismatched publication: %s/%s expected %s", state, invocation, wantInvocation)
				}
				if state != want || activeLeases != 0 {
					t.Fatalf("closed dependency left stale authority: %s/%s/%d", state, invocation, activeLeases)
				}
				if begun && !retained {
					t.Fatal("UNKNOWN source pins became purgeable")
				}
				if _, _, err := f.service.BeginRepositoryBundle(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.begin"), effect); err == nil {
					t.Fatal("closed dependency reused stale Begin authority")
				}
				if _, err := f.service.CompleteRepositoryBundle(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.complete"), effect); err == nil {
					t.Fatal("closed dependency reused stale Complete authority")
				}
				f.noObjectEffects()
			})
		}
	}
}

// marshalBundleFixtureManifest reconstructs only approved metadata for end-to-end pagination proof; no source bytes are invented.
// marshalBundleFixtureManifest восстанавливает лишь approved metadata для pagination proof, не выдумывая source bytes.
func marshalBundleFixtureManifest(entries []repositorybundle.Entry) ([]byte, error) {
	var total int64
	for _, entry := range entries {
		total += entry.Bytes
	}
	return json.Marshal(repositorybundle.Manifest{Version: 1, TotalBytes: total, Files: entries})
}

// nonemptyBundleFixtureSummary selects controlled synthetic model text without changing the owner's publication fact.
// nonemptyBundleFixtureSummary выбирает synthetic model text, не меняя owner publication fact.
func nonemptyBundleFixtureSummary(summary string) string {
	if summary != "" {
		return summary
	}
	return "Waiting for publication"
}

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	broker "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/stagingcrypto"
	store "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/observability"
	transport "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/transport/grpc"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Этот consumer запускается только публичной disposable-оснасткой. CP работает
// отдельным процессом с настоящими PostgreSQL owner-транзакциями и casters;
// Kubernetes здесь fake, транспорт loopback без production mTLS/SSO acceptance.
func TestSecretOwnerCompositionConsumer(t *testing.T) {
	path := os.Getenv("KODEX_SECRET_COMPOSITION_READY_FILE")
	if path == "" {
		t.Skip("disposable owner composition is not configured")
	}
	if os.Getenv("KODEX_SECRET_COMPOSITION_SYNTHETIC") != "1" || os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN") != "" {
		t.Fatal("broker composition must not inherit database credentials")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatal("invalid private composition rendezvous")
	}
	var ready struct{ Address, ProjectRef string }
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &ready) != nil {
		t.Fatal("invalid composition owner receipt")
	}
	host, port, err := net.SplitHostPort(ready.Address)
	number, numberErr := strconv.Atoi(port)
	if err != nil || host != "127.0.0.1" || numberErr != nil || number < 1024 || number > 65535 || ready.ProjectRef == "" {
		t.Fatal("composition owner must be isolated loopback")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	var loseCleanupACK atomic.Bool
	connection, err := grpcgo.NewClient(ready.Address, grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithUnaryInterceptor(func(ctx context.Context, method string, request, response any, connection *grpcgo.ClientConn, invoker grpcgo.UnaryInvoker, options ...grpcgo.CallOption) error {
		if method == cp.RuntimeSecretDraftWorkService_CompleteRuntimeSecretDraftCleanup_FullMethodName && loseCleanupACK.CompareAndSwap(true, false) {
			return status.Error(codes.Unavailable, "synthetic lost cleanup acknowledgement")
		}
		return invoker(ctx, method, request, response, connection, options...)
	}))
	if err != nil {
		t.Fatal("create isolated owner connection")
	}
	defer connection.Close()
	commands := cp.NewPlatformCommandServiceClient(connection)
	workClient := cp.NewRuntimeSecretDraftWorkServiceClient(connection)
	native := cp.NewRuntimeSecretWorkServiceClient(connection)
	const stagedNamespace, runtimeNamespace, guardName = "kodex-secret-drafts", "kodex-runtime", "secret-broker-draft-key-guard"
	client := composedDraftKubernetes(t, stagedNamespace, guardName)
	runtimeStore, err := store.New(client, runtimeNamespace)
	if err != nil {
		t.Fatal(err)
	}
	keyDirectory := t.TempDir()
	if err := os.Chmod(keyDirectory, 0o700); err != nil {
		t.Fatal("protect synthetic keyring directory")
	}
	keyring := filepath.Join(keyDirectory, "keyring.json")
	if err := stagingcrypto.GenerateFile(keyring); err != nil {
		t.Fatal(err)
	}
	service, err := composeSecretDrafts(Config{ClaimantID: "composition-broker", MaximumSecretBytes: 1024, RuntimeNamespace: runtimeNamespace, DraftNamespace: stagedNamespace, DraftKeyGuardName: guardName, DraftKeyringFile: keyring}, workClient, runtimeStore, observability.NewSecretDrafts(), client)
	if err != nil {
		t.Fatal(err)
	}
	server := &transport.Server{}
	transport.WithDraftCommands(service)(server)
	if err := service.ReconcileOnce(ctx); err != nil {
		t.Fatal("initial owner recovery failed", err)
	}
	for _, organization := range []bool{false, true} {
		t.Run(map[bool]string{false: "PROJECT", true: "ORGANIZATION"}[organization], func(t *testing.T) {
			label := map[bool]string{false: "project", true: "organization"}[organization]
			fixture := []byte("synthetic owner wire value " + label)
			digest := sha256.Sum256(fixture)
			mutation := func(step string, version *int64) *cp.MutationContext {
				return &cp.MutationContext{IdempotencyKey: "composition-" + label + "-" + step, ExpectedVersion: version}
			}
			var operation *cp.RuntimeSecretDraftOperationReceipt
			if organization {
				result, err := commands.PrepareOrganizationRuntimeSecretDraft(ctx, &cp.PrepareOrganizationRuntimeSecretDraftRequest{Mutation: mutation("save", nil), Name: "OWNER_WIRE_ORGANIZATION", ValueType: cp.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING, ExpectedContentSha256: hex.EncodeToString(digest[:])})
				if err != nil {
					t.Fatal("actual organization owner prepare", err)
				}
				operation = result.Operation
			} else {
				result, err := commands.PrepareSaveRuntimeSecretDraft(ctx, &cp.PrepareSaveRuntimeSecretDraftRequest{Mutation: mutation("save", nil), ProjectRef: ready.ProjectRef, Name: "OWNER_WIRE_PROJECT", ValueType: cp.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING, ExpectedContentSha256: hex.EncodeToString(digest[:])})
				if err != nil {
					t.Fatal("actual project owner prepare", err)
				}
				operation = result.Operation
			}
			request := &broker.SaveSecretDraftRequest{OperationGrant: operation.OperationGrant, Value: bytes.Clone(fixture)}
			saved, err := server.SaveSecretDraft(ctx, request)
			if err != nil {
				t.Fatal("owner consume, AEAD, staged effect and committed receipt", err)
			}
			if !bytes.Equal(request.Value, make([]byte, len(request.Value))) {
				t.Fatal("secure form retained plaintext")
			}
			draft := saved.Draft
			expectedScope := "RUNTIME_RESOURCE_SCOPE_KIND_PROJECT"
			if organization {
				expectedScope = "RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION"
			}
			if draft.ScopeKind.String() != expectedScope || draft.OrganizationRef == "" || organization && draft.ProjectRef != "" || !organization && draft.ProjectRef != ready.ProjectRef {
				t.Fatal("owner tuple changed across actual wire")
			}
			validation, err := commands.PrepareValidateRuntimeSecretDraft(ctx, &cp.PrepareValidateRuntimeSecretDraftRequest{Mutation: mutation("validate", &draft.Version), DraftRef: draft.Ref})
			if err != nil {
				t.Fatal(err)
			}
			validated, err := server.ValidateSecretDraft(ctx, &broker.ValidateSecretDraftRequest{OperationGrant: validation.Operation.OperationGrant})
			if err != nil {
				t.Fatal("actual owner validation", err)
			}
			draft = validated.Draft
			impact, err := commands.PrepareRuntimeSecretDraftImpact(ctx, &cp.PrepareRuntimeSecretDraftImpactRequest{Mutation: mutation("impact", &draft.Version), DraftRef: draft.Ref})
			if err != nil || impact.Plan.Total != 0 {
				t.Fatal("actual authoritative empty impact", err)
			}
			publication, err := commands.PreparePublishRuntimeSecretDraft(ctx, &cp.PreparePublishRuntimeSecretDraftRequest{Mutation: mutation("publish", &draft.Version), DraftRef: draft.Ref, ExpectedSecretVersion: draft.SecretVersion, ImpactPlanRef: impact.Plan.Ref})
			if err != nil {
				t.Fatal(err)
			}
			published, err := server.PublishSecretDraft(ctx, &broker.PublishSecretDraftRequest{OperationGrant: publication.Operation.OperationGrant})
			if err != nil || published.Secret == nil {
				t.Fatal("actual owner immutable publication", err)
			}
			items, err := runtimeStore.ListManaged(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var actual store.Materialization
			for _, item := range items {
				if item.SecretRef == published.Secret.SecretRef {
					actual = item
				}
			}
			if actual.WorkKind != store.WorkKindDraft || actual.OperationRef != publication.Operation.OperationRef {
				t.Fatal("trusted producer lost DRAFT owner source")
			}
			source, err := client.CoreV1().Secrets(runtimeNamespace).Get(ctx, actual.Name, metav1.GetOptions{})
			if err != nil || !bytes.Equal(source.Data["value"], fixture) {
				t.Fatal("published immutable effect lost exact bytes")
			}
			clear(source.Data["value"])
			materialized := &cp.RuntimeSecretMaterialization{Namespace: actual.Namespace, SecretName: actual.Name, SecretKey: actual.Key, SecretUid: actual.UID, SecretResourceVersion: actual.ResourceVersion, ContentSha256: actual.ContentSHA256}
			if _, err := native.RecoverRuntimeSecretMaterialization(ctx, &cp.RecoverRuntimeSecretMaterializationRequest{OperationRef: actual.OperationRef, Materialization: materialized}); status.Code(err) != codes.NotFound {
				t.Fatal("draft crossed native recovery owner route", err)
			}
			for range 3 {
				if err := service.ReconcileOnce(ctx); err != nil {
					t.Fatal("retained owner sweep", err)
				}
			}
			if _, err := runtimeStore.ReadbackExact(ctx, actual); err != nil {
				t.Fatal("KEEP sweeps deleted published immutable revision")
			}
			version := published.Secret.Version
			revoke, err := commands.PrepareRevokeRuntimeSecret(ctx, &cp.PrepareRevokeRuntimeSecretRequest{Mutation: mutation("revoke", &version), SecretRef: published.Secret.SecretRef})
			if err != nil {
				t.Fatal(err)
			}
			claim, err := native.ConsumeRuntimeSecretOperation(ctx, &cp.ConsumeRuntimeSecretOperationRequest{OperationGrant: revoke.Operation.OperationGrant, ClaimantId: "composition-revoker"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := native.CompleteRuntimeSecretOperation(ctx, &cp.CompleteRuntimeSecretOperationRequest{OperationRef: claim.OperationRef, ClaimantId: "composition-revoker", ClaimGeneration: claim.ClaimGeneration}); err != nil {
				t.Fatal(err)
			}
			loseCleanupACK.Store(true)
			if err := service.ReconcileOnce(ctx); err == nil {
				t.Fatal("lost cleanup ACK was not visible as a recovery failure")
			}
			if _, err := client.CoreV1().Secrets(runtimeNamespace).Get(ctx, actual.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Fatal("revoked draft immutable effect survived exact deletion")
			}
			pending, err := workClient.ListRuntimeSecretDraftRecoveryWork(ctx, &cp.ListRuntimeSecretDraftRecoveryWorkRequest{Page: &cp.PageRequest{PageSize: 100}})
			if err != nil {
				t.Fatal("read durable owner cleanup intent", err)
			}
			var cleanup *cp.RuntimeSecretDraftWork
			for _, candidate := range pending.Operations {
				if candidate.OperationRef == actual.OperationRef {
					cleanup = candidate
				}
			}
			if cleanup == nil || cleanup.RecoveryMaterialization == nil || cleanup.RecoveryMaterialization.SecretUid != actual.UID || cleanup.RecoveryMaterialization.SecretResourceVersion != actual.ResourceVersion || cleanup.ClaimantId != "composition-broker" || cleanup.ClaimGeneration != actual.ClaimGeneration {
				t.Fatal("lost ACK dropped exact fenced retirement intent")
			}
			if err := service.ReconcileOnce(ctx); err != nil {
				t.Fatal("actual owner exact retirement ACK after missing Kubernetes object", err)
			}
			settled, err := workClient.ListRuntimeSecretDraftRecoveryWork(ctx, &cp.ListRuntimeSecretDraftRecoveryWorkRequest{Page: &cp.PageRequest{PageSize: 100}})
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range settled.Operations {
				if candidate.OperationRef == actual.OperationRef {
					t.Fatal("ACKed retirement remained in durable recovery queue")
				}
			}
			if err := service.ReconcileOnce(ctx); err != nil {
				t.Fatal("retirement replay", err)
			}
		})
	}
}

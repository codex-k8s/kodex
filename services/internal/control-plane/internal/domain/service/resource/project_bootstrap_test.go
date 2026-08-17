package resource

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/event"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

type projectBootstrapRepository struct {
	domainrepo.Repository
	tx *projectBootstrapTransaction
}

func (repository *projectBootstrapRepository) Transact(
	ctx context.Context,
	scope domainrepo.Scope,
	callback func(domainrepo.Transaction) error,
) error {
	repository.tx.scope = scope.ProjectID
	repository.tx.steps = append(repository.tx.steps, "begin:organization")
	return callback(repository.tx)
}

type projectBootstrapTransaction struct {
	domainrepo.Transaction
	scope     string
	projectID string
	steps     []string
	policy    domainrepo.ResourceRetentionPolicy
	receipt   domainrepo.Receipt
}

func (tx *projectBootstrapTransaction) GetReceipt(
	context.Context,
	string,
	string,
	string,
) (domainrepo.Receipt, error) {
	tx.steps = append(tx.steps, "receipt:get")
	return domainrepo.Receipt{}, errs.ErrNotFound
}

func (tx *projectBootstrapTransaction) Insert(_ context.Context, resource entity.Resource) error {
	if tx.scope != "" || resource.Kind != enum.KindProject || resource.ID != resource.ProjectID {
		return errors.New("project insert scope mismatch")
	}
	tx.projectID = resource.ID
	tx.steps = append(tx.steps, "project:insert")
	return nil
}

func (tx *projectBootstrapTransaction) SwitchWorkspaceProject(_ context.Context, projectID string) error {
	if projectID != "" && projectID != tx.projectID {
		return errors.New("unexpected project scope")
	}
	tx.scope = projectID
	if projectID == "" {
		tx.steps = append(tx.steps, "scope:organization")
	} else {
		tx.steps = append(tx.steps, "scope:project")
	}
	return nil
}

func (tx *projectBootstrapTransaction) InsertResourceRetentionPolicy(
	_ context.Context,
	policy domainrepo.ResourceRetentionPolicy,
	actorID, reasonCode, idempotencyKeySHA256, requestSHA256 string,
) error {
	if tx.scope != tx.projectID || actorID == "" || reasonCode != projectBootstrapReasonCode ||
		len(idempotencyKeySHA256) != 64 || len(requestSHA256) != 64 {
		return errors.New("retention bootstrap scope mismatch")
	}
	tx.policy = policy
	tx.steps = append(tx.steps, "retention:insert")
	return nil
}

func (tx *projectBootstrapTransaction) AppendAudit(_ context.Context, audit domainrepo.Audit) error {
	if tx.scope != tx.projectID || audit.ProjectID != tx.projectID ||
		audit.ResourceID != tx.projectID || audit.Action != "create_project" {
		return errors.New("audit bootstrap scope mismatch")
	}
	tx.steps = append(tx.steps, "audit:append")
	return nil
}

func (tx *projectBootstrapTransaction) AppendEvent(_ context.Context, change event.Change) error {
	if tx.scope != tx.projectID || change.ProjectID != tx.projectID ||
		change.ResourceID != tx.projectID || change.EventName != event.RuntimeConfigurationChanged {
		return errors.New("outbox bootstrap scope mismatch")
	}
	tx.steps = append(tx.steps, "outbox:append")
	return nil
}

func (tx *projectBootstrapTransaction) SaveReceipt(_ context.Context, receipt domainrepo.Receipt) error {
	if tx.scope != "" || receipt.ProjectID != "" || receipt.Scope != "create_project" {
		return errors.New("receipt bootstrap scope mismatch")
	}
	tx.receipt = receipt
	tx.steps = append(tx.steps, "receipt:save")
	return nil
}

type projectBootstrapObserver struct{}

func (projectBootstrapObserver) ObserveMutation(enum.Kind, string) {}
func (projectBootstrapObserver) ObserveScheduleMaintenance(string) {}

func TestCreateProjectSwitchesScopeForOwnedBootstrapRows(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	tx := &projectBootstrapTransaction{}
	service := &Service{
		repository: &projectBootstrapRepository{tx: tx},
		observer:   projectBootstrapObserver{},
		now:        func() time.Time { return now },
	}
	principal := value.Principal{
		ActorID: uuid.NewString(), OrganizationID: uuid.NewString(),
		Permission: permissionProjectCreate, CorrelationID: uuid.NewString(),
		PolicyRevision: 1, AuthorityGeneration: 1,
		CallerWorkload: controlAPIGatewayWorkload, CallerSPIFFEID: controlAPIGatewaySPIFFEID,
		AuthoritySource: "OWNER_SESSION", AuthorityReference: uuid.NewString(),
		AuthorityRevision: 1, AuthorityDigest: strings.Repeat("a", 64),
	}
	created, err := service.CreateProject(context.Background(), CreateProjectInput{
		Principal: principal, IdempotencyKey: "project-bootstrap-test",
		Name: "Bootstrap project",
		Spec: entity.ProjectSpec{
			Slug: "bootstrap-project", Locale: "ru",
			Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"},
		},
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if created.ID == "" || created.ID != tx.projectID || tx.receipt.Result.ID != created.ID {
		t.Fatalf("created project was not persisted in receipt: created=%q receipt=%q", created.ID, tx.receipt.Result.ID)
	}
	if tx.policy.ID != runtimeRetentionPolicyID || tx.policy.Version != 1 ||
		tx.policy.PVCRetentionSeconds != 604800 || tx.policy.ArchiveRetentionSeconds != 7776000 {
		t.Fatalf("unexpected bootstrap retention policy: %#v", tx.policy)
	}
	want := []string{
		"begin:organization", "receipt:get", "project:insert", "scope:project",
		"retention:insert", "audit:append", "outbox:append", "scope:organization", "receipt:save",
	}
	if strings.Join(tx.steps, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected bootstrap sequence: got %v want %v", tx.steps, want)
	}
}

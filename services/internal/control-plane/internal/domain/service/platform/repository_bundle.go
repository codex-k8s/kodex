package platform

import (
	"context"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// repositoryBundleOwner resolves an admitted exact-method principal and the specialized owner; neither user project headers nor a proposal locator grant work authority.
// repositoryBundleOwner разрешает admitted exact-method principal и специализированного owner; user project headers и proposal locator не дают work authority.
func (service *Service) repositoryBundleOwner(ctx context.Context, p value.Principal, action string) (value.Principal, port.RepositoryBundleOwner, error) {
	workload := "integration-gateway"
	if action == "prepare" {
		workload = "runtime-controller"
	} else if action == "get" {
		workload = "control-api-gateway"
	}
	if p.CallerWorkload != workload || p.Permission != "platform.repository-bundles."+action || workload != "control-api-gateway" && p.ProjectRef != "" {
		return value.Principal{}, nil, errs.ErrForbidden
	}
	p, err := service.principal(ctx, p)
	if err != nil {
		return value.Principal{}, nil, err
	}
	owner, found := service.repository.(port.RepositoryBundleOwner)
	if !found {
		return value.Principal{}, nil, errs.ErrUnavailable
	}
	return p, owner, nil
}

// PrepareRepositoryBundle accepts a compact native producer request and delegates exact execution/artifact/grant admission and atomic gate creation to the owner.
// PrepareRepositoryBundle принимает компактный native producer request и передаёт owner exact execution/artifact/grant admission и atomic Gate creation.
func (service *Service) PrepareRepositoryBundle(ctx context.Context, p value.Principal, input port.RepositoryBundlePrepare) (entity.RepositoryBundlePublication, error) {
	p, owner, err := service.repositoryBundleOwner(ctx, p, "prepare")
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	return owner.PrepareRepositoryBundle(ctx, p, input)
}

// GetRepositoryBundle validates bounded manifest page pins; a later page must retain the first version and digest, while tenant/resource eligibility stays with the authoritative owner.
// GetRepositoryBundle проверяет bounded manifest page pins; последующая страница сохраняет version/digest первой, а tenant/resource eligibility остаётся у authoritative owner.
func (service *Service) GetRepositoryBundle(ctx context.Context, p value.Principal, input port.RepositoryBundleRead) (entity.RepositoryBundlePublicationView, error) {
	p, owner, err := service.repositoryBundleOwner(ctx, p, "get")
	if err != nil {
		return entity.RepositoryBundlePublicationView{}, err
	}
	if (input.ProposalRef == "") == (input.GateRef == "") || len(input.ProposalRef) > 96 || len(input.GateRef) > 96 || input.Offset < 0 || input.Offset > 256 || input.Limit < 1 || input.Limit > 32 || input.Offset > 0 && (input.ExpectedVersion < 1 || len(input.ExpectedManifestSHA256) != 64) {
		return entity.RepositoryBundlePublicationView{}, errs.ErrInvalid
	}
	return owner.GetRepositoryBundle(ctx, p, input)
}

// ClaimRepositoryBundles permits one bounded private work item per cycle and binds it to an explicit claimant; snapshots never grant tenant-wide artifact access.
// ClaimRepositoryBundles допускает одну bounded private work item за цикл с явным claimant; snapshots не дают tenant-wide artifact access.
func (service *Service) ClaimRepositoryBundles(ctx context.Context, p value.Principal, claimant string, limit int32) ([]entity.RepositoryBundlePublicationWork, error) {
	p, owner, err := service.repositoryBundleOwner(ctx, p, "work.claim")
	if err != nil {
		return nil, err
	}
	if claimant == "" || len(claimant) > 128 || strings.TrimSpace(claimant) != claimant || strings.ContainsAny(claimant, "\x00\r\n") || limit != 1 {
		return nil, errs.ErrInvalid
	}
	return owner.ClaimRepositoryBundles(ctx, p, claimant, limit)
}

// validRepositoryBundleWorkLease requires the full durable tuple before any private work read or mutation; expiry and owner state are checked under the repository lock.
// validRepositoryBundleWorkLease требует полный durable tuple до private work read/mutation; expiry и owner state проверяются под repository lock.
func validRepositoryBundleWorkLease(lease entity.RepositoryBundleLease) bool {
	return strings.HasPrefix(lease.ProposalRef, "rbp_") && len(lease.ProposalRef) <= 96 && lease.Attempt > 0 && lease.Generation > 0 && lease.Claimant != "" && len(lease.Claimant) <= 128 && lease.Fence != "" && len(lease.Fence) <= 128 && !lease.ExpiresAt.IsZero()
}

// RenewRepositoryBundle refreshes only the exact owned work lease and never changes content, candidate identity or execution/recovery mode.
// RenewRepositoryBundle обновляет лишь exact owned work lease, не меняя content, candidate identity либо execution/recovery mode.
func (service *Service) RenewRepositoryBundle(ctx context.Context, p value.Principal, lease entity.RepositoryBundleLease) (entity.RepositoryBundleLease, error) {
	p, owner, err := service.repositoryBundleOwner(ctx, p, "work.renew")
	if err != nil {
		return entity.RepositoryBundleLease{}, err
	}
	if !validRepositoryBundleWorkLease(lease) {
		return entity.RepositoryBundleLease{}, errs.ErrInvalid
	}
	return owner.RenewRepositoryBundle(ctx, p, lease)
}

// BeginRepositoryBundle records an exact single-candidate intent before Git and returns AlreadyStarted on the same durable receipt; that replay never authorizes another push.
// BeginRepositoryBundle фиксирует exact single-candidate intent до Git и возвращает AlreadyStarted при прежней durable receipt; replay не разрешает повторный push.
func (service *Service) BeginRepositoryBundle(ctx context.Context, p value.Principal, input port.RepositoryBundleEffect) (entity.RepositoryBundlePublication, bool, error) {
	p, owner, err := service.repositoryBundleOwner(ctx, p, "work.begin")
	if err != nil {
		return entity.RepositoryBundlePublication{}, false, err
	}
	if !validRepositoryBundleWorkLease(input.Lease) {
		return entity.RepositoryBundlePublication{}, false, errs.ErrInvalid
	}
	return owner.BeginRepositoryBundle(ctx, p, input)
}

// CompleteRepositoryBundle persists only exact readback of the already begun candidate; missing/ambiguous remote state must remain UNKNOWN rather than being resent.
// CompleteRepositoryBundle сохраняет лишь exact readback уже начатого candidate; missing/ambiguous remote state остаётся UNKNOWN без повторной отправки.
func (service *Service) CompleteRepositoryBundle(ctx context.Context, p value.Principal, input port.RepositoryBundleEffect) (entity.RepositoryBundlePublication, error) {
	p, owner, err := service.repositoryBundleOwner(ctx, p, "work.complete")
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	if !validRepositoryBundleWorkLease(input.Lease) {
		return entity.RepositoryBundlePublication{}, errs.ErrInvalid
	}
	return owner.CompleteRepositoryBundle(ctx, p, input)
}

// FailRepositoryBundle reports a closed failure class under the exact lease; failure after Begin cannot claim the external effect was undone.
// FailRepositoryBundle сообщает закрытый failure class в exact lease; failure после Begin не означает отмену внешнего эффекта.
func (service *Service) FailRepositoryBundle(ctx context.Context, p value.Principal, lease entity.RepositoryBundleLease, code string) (entity.RepositoryBundlePublication, error) {
	p, owner, err := service.repositoryBundleOwner(ctx, p, "work.fail")
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	if !validRepositoryBundleWorkLease(lease) || !containsBundleFailure(code) {
		return entity.RepositoryBundlePublication{}, errs.ErrInvalid
	}
	return owner.FailRepositoryBundle(ctx, p, lease, code)
}

// containsBundleFailure admits only supported bounded metadata classes, never provider bodies or arbitrary error text.
// containsBundleFailure допускает лишь поддержанные bounded metadata classes, не provider body и не произвольный error text.
func containsBundleFailure(code string) bool {
	switch code {
	case "UNAVAILABLE", "AUTHORITY_CHANGED", "CONTENT_INVALID", "BASE_INVALID", "DEADLINE_EXCEEDED", "BRANCH_CONFLICT", "OUTCOME_UNCONFIRMED":
		return true
	default:
		return false
	}
}

// ReadRepositoryBundle exposes the frozen archive only to an exact private work lease; arbitrary artifact refs, object keys and runtime lease substitution are unsupported.
// ReadRepositoryBundle раскрывает frozen archive лишь exact private work lease; arbitrary artifact refs, object keys и runtime lease substitution не поддерживаются.
func (service *Service) ReadRepositoryBundle(ctx context.Context, p value.Principal, lease entity.RepositoryBundleLease) (port.ArtifactDownload, error) {
	p, owner, err := service.repositoryBundleOwner(ctx, p, "work.content.read")
	if err != nil {
		return port.ArtifactDownload{}, err
	}
	if !validRepositoryBundleWorkLease(lease) {
		return port.ArtifactDownload{}, errs.ErrInvalid
	}
	return owner.ReadRepositoryBundle(ctx, p, lease)
}

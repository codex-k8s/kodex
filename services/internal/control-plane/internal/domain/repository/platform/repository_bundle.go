package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// RepositoryBundlePrepare carries only exact execution/grant/artifact locators; owner and destination branch are resolved by CP.
// RepositoryBundlePrepare переносит лишь exact execution/grant/artifact locators; owner и destination branch разрешает CP.
type RepositoryBundlePrepare struct {
	LeaseRef, Fence, GrantRef, BaseCommitSHA, IdempotencyKey string
	Generation                                               int64
	Archive, Manifest                                        entity.RepositoryBundleArtifactPin
}

// RepositoryBundleRead selects one proposal or native gate and optionally pins a later page to the first version/digest.
// RepositoryBundleRead выбирает одну proposal либо native gate и закрепляет последующую страницу за version/digest первой.
type RepositoryBundleRead struct {
	ProposalRef, GateRef, ExpectedManifestSHA256 string
	ExpectedVersion                              int64
	Offset, Limit                                int32
}

// RepositoryBundleEffect binds a single candidate/readback to the durable lease and all approved content/base pins.
// RepositoryBundleEffect связывает единственный candidate/readback с durable lease и всеми approved content/base pins.
type RepositoryBundleEffect struct {
	Lease                                               entity.RepositoryBundleLease
	CandidateCommitSHA, CandidateTreeSHA, BaseCommitSHA string
	ManifestSHA256, ArchiveSHA256                       string
}

// RepositoryBundleOwner owns proposal, manifest visibility, work and effect receipts atomically; no method grants arbitrary Git or artifact access.
// RepositoryBundleOwner атомарно владеет proposal, manifest visibility, work и effect receipts; методы не дают произвольный Git/artifact access.
type RepositoryBundleOwner interface {
	// PrepareRepositoryBundle creates the native gate and frozen proposal atomically after exact execution/artifact/grant admission.
	// PrepareRepositoryBundle атомарно создаёт native Gate и frozen proposal после exact execution/artifact/grant admission.
	PrepareRepositoryBundle(context.Context, value.Principal, RepositoryBundlePrepare) (entity.RepositoryBundlePublication, error)
	// GetRepositoryBundle reads an authorized bounded manifest page without authorizing publication or exposing private archive bytes.
	// GetRepositoryBundle читает authorized bounded manifest page, не разрешая публикацию и не раскрывая private archive bytes.
	GetRepositoryBundle(context.Context, value.Principal, RepositoryBundleRead) (entity.RepositoryBundlePublicationView, error)
	// ClaimRepositoryBundles assigns exact fenced work or read-only recovery after owner eligibility checks.
	// ClaimRepositoryBundles назначает exact fenced work либо read-only recovery после owner eligibility checks.
	ClaimRepositoryBundles(context.Context, value.Principal, string, int32) ([]entity.RepositoryBundlePublicationWork, error)
	// RenewRepositoryBundle retains the same work identity and never reissues authority for a closed effect.
	// RenewRepositoryBundle сохраняет work identity и не выдаёт authority для закрытого эффекта повторно.
	RenewRepositoryBundle(context.Context, value.Principal, entity.RepositoryBundleLease) (entity.RepositoryBundleLease, error)
	// BeginRepositoryBundle persists candidate identity before the sole external push and distinguishes receipt replay from new permission.
	// BeginRepositoryBundle сохраняет candidate identity до единственного external push, отличая receipt replay от нового разрешения.
	BeginRepositoryBundle(context.Context, value.Principal, RepositoryBundleEffect) (entity.RepositoryBundlePublication, bool, error)
	// CompleteRepositoryBundle stores exact readback and the durable receipt in the owner transaction.
	// CompleteRepositoryBundle сохраняет exact readback и durable receipt в owner transaction.
	CompleteRepositoryBundle(context.Context, value.Principal, RepositoryBundleEffect) (entity.RepositoryBundlePublication, error)
	// FailRepositoryBundle closes a known pre-effect failure or preserves UNKNOWN after a begun effect.
	// FailRepositoryBundle закрывает известный pre-effect failure либо сохраняет UNKNOWN после начатого эффекта.
	FailRepositoryBundle(context.Context, value.Principal, entity.RepositoryBundleLease, string) (entity.RepositoryBundlePublication, error)
	// ReadRepositoryBundle returns only frozen bytes bound to the full live private work lease.
	// ReadRepositoryBundle возвращает лишь frozen bytes, привязанные к full live private work lease.
	ReadRepositoryBundle(context.Context, value.Principal, entity.RepositoryBundleLease) (ArtifactDownload, error)
}

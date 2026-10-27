package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5"
)

// queryModelCatalogResolveProof resolves the system owner identity and organization version only
// for a live claimed task digest with exact account/version/current-credential pins, enabled
// account/definition and active system subject/OWNER binding. It grants no authority by the digest
// alone; the method additionally checks the authenticated proof identity and closed
// operation/workload scope.
// queryModelCatalogResolveProof разрешает system owner identity и версию организации только для
// живого CLAIMED task digest с точными account, version и current credential, включёнными account
// и definition, активными system subject и OWNER binding. Один digest не выдаёт полномочий; метод
// дополнительно проверяет proof identity и закрытые operation, workload и scope.
//
//go:embed sql/model_catalog__resolve_proof.sql
var queryModelCatalogResolveProof string

// ResolveProviderModelCatalogProof requires control-plane workload, exact catalog operation, no
// project and valid digest before a five-second proof-identity resolution and live task lookup.
// NoRows or actor/organization mismatch is Forbidden, other read failure Unavailable; success pins
// ActorVersion to one and returns stored organization version. It neither claims work nor exposes
// credentials or treats the read timestamp as authority.
// ResolveProviderModelCatalogProof требует control-plane workload, точную catalog operation,
// отсутствие проекта и верный digest до пятиисекундного разрешения proof identity и поиска живого
// task. NoRows и actor/org mismatch — Forbidden, прочий сбой чтения — Unavailable; успех задаёт
// ActorVersion один и возвращает версию организации. Не захватывает работу, не раскрывает
// credentials и не использует timestamp как authority.
func (repository *Repository) ResolveProviderModelCatalogProof(ctx context.Context, principal platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	if principal.CallerWorkload != "control-plane" || principal.Operation != platformrepo.ProviderModelCatalogOperation || principal.ProjectRef != "" || !validModelCatalogDigest(principal.RequestDigestSHA256) {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	authority, err := repository.resolveProofIdentity(ctx, principal)
	if err != nil {
		return platformrepo.ProofAuthority{}, err
	}
	var resolved platformrepo.ProofAuthority
	var updatedAt time.Time
	err = repository.pool.QueryRow(ctx, queryModelCatalogResolveProof, principal.RequestDigestSHA256).Scan(&resolved.ActorID, &resolved.OrganizationID, &updatedAt, &resolved.OrganizationVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	if err != nil {
		return platformrepo.ProofAuthority{}, errs.ErrUnavailable
	}
	if resolved.ActorID != authority.ActorID || resolved.OrganizationID != authority.OrganizationID {
		return platformrepo.ProofAuthority{}, errs.ErrForbidden
	}
	resolved.ActorVersion = 1
	return resolved, nil
}

// validModelCatalogDigest delegates exact 64-byte unprefixed lowercase SHA-256 text grammar to the
// existing shared validator. It preserves zero digests and rejection of malformed
// case/length/Unicode without hashing, task lookup, credential access or authority validation.
// validModelCatalogDigest делегирует существующему общему валидатору точные 64 строчных SHA-256
// hex-байта без префикса. Сохраняет нулевой digest и отказ при неверных case, length и Unicode без
// хеширования, task lookup, credential access и authority validation.
func validModelCatalogDigest(digest string) bool {
	return exactSHA256(digest)
}

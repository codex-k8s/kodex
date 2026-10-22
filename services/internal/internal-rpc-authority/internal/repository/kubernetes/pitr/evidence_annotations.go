package pitr

import (
	"strconv"

	model "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// evidenceAnnotations returns a fresh map of the twelve served evidence pins
// shared by publication and validation, preserving decimal uint64 encoding and
// exact supplied strings. It does not verify claims, signatures or provenance.
// evidenceAnnotations возвращает новую карту двенадцати served evidence pins
// для публикации и проверки с прежним десятичным uint64-кодированием и точными
// строками. Claims, подписи и происхождение здесь не проверяются.
func evidenceAnnotations(claims model.RestoreFenceEvidenceClaims, digest string) map[string]string {
	return map[string]string{
		"kodex.dev/restore-anchor-revision":              strconv.FormatUint(claims.AnchorRevision, 10),
		"kodex.dev/restore-epoch":                        strconv.FormatUint(claims.RestoreEpoch, 10),
		"kodex.dev/restore-evidence-digest-sha256":       digest,
		"kodex.dev/restore-predecessor-revision":         strconv.FormatUint(claims.Predecessor.Revision, 10),
		"kodex.dev/restore-predecessor-digest-sha256":    claims.Predecessor.DigestSHA256,
		"kodex.dev/restored-cluster-uid":                 claims.RestoredClusterUID,
		"kodex.dev/restored-timeline-id":                 strconv.FormatUint(claims.RestoredTimelineID, 10),
		"kodex.dev/cnpg-backup-uid":                      claims.BackupResourceUID,
		"kodex.dev/cnpg-backup-resource-version":         claims.BackupResourceVersion,
		"kodex.dev/cnpg-backup-id":                       claims.ProviderBackupID,
		"kodex.dev/cnpg-source-cluster-uid":              claims.SourceClusterUID,
		"kodex.dev/cnpg-source-cluster-resource-version": claims.SourceClusterResourceVersion,
	}
}

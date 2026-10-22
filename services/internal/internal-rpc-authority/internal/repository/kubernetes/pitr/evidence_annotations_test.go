package pitr

import (
	"testing"

	model "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// TestEvidenceAnnotationsPreservePinsAndStorage checks all twelve keys and exact
// strings for zero and maximum uint64 fixtures, then proves each call owns a
// fresh map. No signing, Kubernetes operation or restore is performed.
// TestEvidenceAnnotationsPreservePinsAndStorage проверяет все двенадцать ключей
// и точные строки для нулевого и максимального uint64-эталонов и отдельное
// владение картой каждого вызова. Подпись, Kubernetes и restore не выполняются.
func TestEvidenceAnnotationsPreservePinsAndStorage(t *testing.T) {
	keys := []string{
		"kodex.dev/restore-anchor-revision",
		"kodex.dev/restore-epoch",
		"kodex.dev/restore-evidence-digest-sha256",
		"kodex.dev/restore-predecessor-revision",
		"kodex.dev/restore-predecessor-digest-sha256",
		"kodex.dev/restored-cluster-uid",
		"kodex.dev/restored-timeline-id",
		"kodex.dev/cnpg-backup-uid",
		"kodex.dev/cnpg-backup-resource-version",
		"kodex.dev/cnpg-backup-id",
		"kodex.dev/cnpg-source-cluster-uid",
		"kodex.dev/cnpg-source-cluster-resource-version",
	}
	tests := []struct {
		name   string
		claims model.RestoreFenceEvidenceClaims
		digest string
		want   [12]string
	}{
		{name: "zero values", want: [12]string{"0", "0", "", "0", "", "", "0", "", "", "", "", ""}},
		{
			name: "exact nonzero pins",
			claims: model.RestoreFenceEvidenceClaims{
				AnchorRevision: ^uint64(0), RestoreEpoch: 17,
				Predecessor:        model.RestoreEvidencePointer{Revision: 29, DigestSHA256: "predecessor"},
				RestoredClusterUID: "restored-uid", RestoredTimelineID: 31,
				BackupResourceUID: "backup-uid", BackupResourceVersion: "0042",
				ProviderBackupID: "provider-backup", SourceClusterUID: "source-uid",
				SourceClusterResourceVersion: "0007",
			},
			digest: "exact compact digest",
			want: [12]string{
				"18446744073709551615", "17", "exact compact digest", "29", "predecessor",
				"restored-uid", "31", "backup-uid", "0042", "provider-backup", "source-uid", "0007",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := evidenceAnnotations(test.claims, test.digest)
			if len(got) != len(keys) {
				t.Fatalf("unexpected annotation count: got %d, want %d", len(got), len(keys))
			}
			for index, key := range keys {
				if value, ok := got[key]; !ok || value != test.want[index] {
					t.Fatalf("annotation %s: got %q present=%t, want %q", key, value, ok, test.want[index])
				}
			}
			fresh := evidenceAnnotations(test.claims, test.digest)
			got[keys[0]] = "mutated"
			delete(got, keys[1])
			if fresh[keys[0]] != test.want[0] || fresh[keys[1]] != test.want[1] || len(fresh) != len(keys) {
				t.Fatal("annotation maps share mutable storage")
			}
		})
	}
}

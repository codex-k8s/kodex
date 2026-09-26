package platform

import (
	"testing"

	repoport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestSecretDraftRecoverySettled(t *testing.T) {
	retained := &entity.RuntimeSecretMaterialization{}
	tests := []struct {
		name   string
		draft  secretDraftRow
		work   secretDraftOperationRow
		input  repoport.RuntimeSecretDraftWorkInput
		result entity.RuntimeSecretDraftResult
		want   bool
	}{
		{
			name:  "published retained materialization",
			draft: secretDraftRow{public: entity.RuntimeSecretDraft{State: "PUBLISHED"}},
			work:  secretDraftOperationRow{state: "COMPLETED"},
			input: repoport.RuntimeSecretDraftWorkInput{Materialization: retained},
			result: entity.RuntimeSecretDraftResult{
				EncryptedAction:       "KEEP",
				MaterializationAction: "KEEP",
			},
			want: true,
		},
		{
			name:  "published missing materialization",
			draft: secretDraftRow{public: entity.RuntimeSecretDraft{State: "PUBLISHED"}},
			work:  secretDraftOperationRow{state: "COMPLETED"},
			result: entity.RuntimeSecretDraftResult{
				EncryptedAction:       "KEEP",
				MaterializationAction: "KEEP",
			},
		},
		{
			name:  "published materialization scheduled for delete",
			draft: secretDraftRow{public: entity.RuntimeSecretDraft{State: "PUBLISHED"}},
			work:  secretDraftOperationRow{state: "COMPLETED"},
			input: repoport.RuntimeSecretDraftWorkInput{Materialization: retained},
			result: entity.RuntimeSecretDraftResult{
				EncryptedAction:       "KEEP",
				MaterializationAction: "DELETE",
			},
		},
		{
			name: "failed without external effects",
			work: secretDraftOperationRow{state: "FAILED"},
			want: true,
		},
		{
			name: "cleanup intent still pending",
			work: secretDraftOperationRow{
				state:            "FAILED",
				encryptedCleanup: []byte(`{"namespace":"runtime"}`),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := secretDraftRecoverySettled(test.draft, test.work, test.input, test.result); got != test.want {
				t.Fatalf("secretDraftRecoverySettled() = %v, want %v", got, test.want)
			}
		})
	}
}

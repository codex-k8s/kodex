package platform

import (
	"testing"

	repoport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestSecretDraftRecoverySettled distinguishes retained published materialization or failed work
// without effects from missing/deleting materialization and pending encrypted cleanup.
// TestSecretDraftRecoverySettled отличает сохранённую опубликованную материализацию и ошибочную
// работу без эффектов от отсутствующей либо удаляемой материализации и ожидающей очистки
// encrypted-данных.
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

// TestSecretDraftPublishedRecoveryAlreadySettled requires published/completed work, completed
// cleanup and retained materialization with KEEP actions before declaring recovery already
// settled.
// TestSecretDraftPublishedRecoveryAlreadySettled требует опубликованную и завершённую работу,
// завершённую очистку и сохранённую материализацию с действиями KEEP перед признанием
// восстановления уже завершённым.
func TestSecretDraftPublishedRecoveryAlreadySettled(t *testing.T) {
	retained := &entity.RuntimeSecretMaterialization{}
	baseDraft := secretDraftRow{public: entity.RuntimeSecretDraft{State: "PUBLISHED"}}
	baseWork := secretDraftOperationRow{state: "COMPLETED", cleanupCompleted: true, encryptedCleanup: []byte(`{"namespace":"runtime"}`)}
	baseInput := repoport.RuntimeSecretDraftWorkInput{Encrypted: &entity.RuntimeSecretDraftEncryptedDescriptor{}, Materialization: retained}
	baseResult := entity.RuntimeSecretDraftResult{EncryptedAction: "KEEP", MaterializationAction: "KEEP"}
	tests := []struct {
		name   string
		draft  secretDraftRow
		work   secretDraftOperationRow
		input  repoport.RuntimeSecretDraftWorkInput
		result entity.RuntimeSecretDraftResult
		want   bool
	}{
		{name: "completed published retained", draft: baseDraft, work: baseWork, input: baseInput, result: baseResult, want: true},
		{name: "cleanup not completed", draft: baseDraft, work: secretDraftOperationRow{state: "COMPLETED"}, input: baseInput, result: baseResult},
		{name: "draft not published", draft: secretDraftRow{public: entity.RuntimeSecretDraft{State: "FAILED"}}, work: baseWork, input: baseInput, result: baseResult},
		{name: "operation not completed", draft: baseDraft, work: secretDraftOperationRow{state: "FAILED", cleanupCompleted: true}, input: baseInput, result: baseResult},
		{name: "materialization absent", draft: baseDraft, work: baseWork, result: baseResult},
		{name: "materialization scheduled for deletion", draft: baseDraft, work: baseWork, input: baseInput, result: entity.RuntimeSecretDraftResult{EncryptedAction: "KEEP", MaterializationAction: "DELETE"}},
		{name: "encrypted cleanup pending", draft: baseDraft, work: secretDraftOperationRow{state: "COMPLETED", encryptedCleanup: []byte(`{"namespace":"runtime"}`)}, input: baseInput, result: baseResult},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := secretDraftPublishedRecoveryAlreadySettled(test.draft, test.work, test.input, test.result); got != test.want {
				t.Fatalf("secretDraftPublishedRecoveryAlreadySettled() = %v, want %v", got, test.want)
			}
		})
	}
}

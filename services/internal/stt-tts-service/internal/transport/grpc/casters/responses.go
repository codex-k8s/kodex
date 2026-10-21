package casters

import (
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

// TranscriptionResponse copies text and exact domain receipt pins into the RPC response and marks the fixed completed-provider stage without including credential bytes.
// RU: TranscriptionResponse копирует текст и точные координаты доменной квитанции в RPC ответ и отмечает фиксированный завершённый этап провайдера без байтов реквизитов.
func TranscriptionResponse(result value.TranscriptionResult) *sttv1.TranscribeResponse {
	receipt := result.Receipt
	return &sttv1.TranscribeResponse{Text: result.Text, Receipt: &sttv1.TranscriptionReceipt{
		RequestId: receipt.RequestID, CorrelationId: receipt.CorrelationID,
		ActorId: receipt.ActorID, TenantId: receipt.TenantID, ProjectId: receipt.ProjectID,
		AuthoritySourceRevision:     receipt.AuthoritySourceRevision,
		AuthoritySourceDigestSha256: receipt.AuthoritySourceDigestSHA256,
		ConfigRevision:              receipt.ConfigRevision, ConfigDigestSha256: receipt.ConfigDigestSHA256,
		Model: receipt.Model, Language: receipt.Language, ProviderAccountRef: receipt.ProviderAccountRef,
		ProviderCredentialGeneration: receipt.ProviderCredentialGeneration,
		CompletedStage:               sttv1.TranscriptionStage_TRANSCRIPTION_STAGE_PROVIDER_COMPLETED,
	}}
}

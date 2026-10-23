package command

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

// EmailEffectReportInput correlates an email execution binding with an external receipt, semantic
// input digest, and reported outcome. The owner verifies the exact execution and receipt; the report
// cannot approve sending or authorize a second effect.
// EmailEffectReportInput связывает привязку исполнения почты с внешней квитанцией, семантическим
// дайджестом входа и сообщённым исходом. Владелец проверяет точное исполнение и квитанцию; отчёт не
// подтверждает отправку и не разрешает повторный эффект.
type EmailEffectReportInput struct {
	Binding                                                                 entity.EmailExecutionBinding
	ExternalReceiptRef, ExternalReceiptDigest, SemanticInputDigest, Outcome string
}

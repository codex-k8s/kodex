package command

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

type EmailEffectReportInput struct {
	Binding                                                                 entity.EmailExecutionBinding
	ExternalReceiptRef, ExternalReceiptDigest, SemanticInputDigest, Outcome string
}

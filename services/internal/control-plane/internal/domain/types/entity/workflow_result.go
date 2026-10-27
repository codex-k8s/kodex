package entity

// WorkflowResultRequirement declares one bounded mandatory output predicate; its references select
// owner-verified evidence and never grant authority supplied by an agent.
// WorkflowResultRequirement задаёт ограниченное обязательное условие; ссылки выбирают проверенные
// владельцем доказательства и не предоставляют полномочий, заявленных сотрудником.
type WorkflowResultRequirement struct {
	// SubjectResultKey pins one published ARTIFACT output; omission preserves historical spec bytes.
	// SubjectResultKey закрепляет published ARTIFACT output; отсутствие сохраняет historical spec bytes.
	SubjectResultKey                                                                 string `json:",omitempty"`
	Key, Kind, FileName, MediaType, Repository, SubjectStepKey, Operation, TargetRef string
}

// WorkflowResultDeclaration is untrusted model output associated with an owner-assigned attempt.
// WorkflowResultDeclaration — недоверенный ответ модели, связанный с назначенной сервером попыткой.
type WorkflowResultDeclaration struct {
	Schema  string                   `json:"schema"`
	Results []WorkflowDeclaredResult `json:"results"`
}

// WorkflowCaptureProgressDeclaration requests capture and a fresh technical turn using only
// published artifact keys. It carries no evidence, authority or business-acceptance verdict.
// WorkflowCaptureProgressDeclaration запрашивает capture и новый технический ход только по
// опубликованным ключам файлов. Декларация не содержит evidence, полномочий или бизнес-приёмки.
type WorkflowCaptureProgressDeclaration struct {
	Schema       string   `json:"schema"`
	Phase        string   `json:"phase"`
	Cause        string   `json:"cause"`
	ArtifactKeys []string `json:"artifactKeys"`
}

// WorkflowDeclaredResult names candidate evidence and a verdict; the owner resolves every reference
// inside the exact project, run, step and attempt before accepting it.
// WorkflowDeclaredResult называет доказательство и заключение; владелец проверяет каждую ссылку
// внутри точных проекта, запуска, шага и попытки перед принятием.
type WorkflowDeclaredResult struct {
	Key        string                  `json:"key"`
	Source     *WorkflowArtifactSource `json:"source,omitempty"`
	ReceiptRef string                  `json:"receiptRef,omitempty"`
	HeadSHA    string                  `json:"headSha,omitempty"`
	Verdict    string                  `json:"verdict,omitempty"`
}

// WorkflowArtifactSource selects either this completion's exact prepared outputs or an immutable
// previously captured revision. These untrusted selectors never attest ownership, lineage or CLEAN.
// WorkflowArtifactSource выбирает точные подготовленные файлы текущего завершения либо ранее
// сохранённую неизменяемую ревизию. Селектор не подтверждает владение, происхождение или CLEAN.
type WorkflowArtifactSource struct {
	Kind        string `json:"kind"`
	ArtifactRef string `json:"artifactRef,omitempty"`
	Revision    int64  `json:"revision,omitempty"`
	Digest      string `json:"digest,omitempty"`
}

// WorkflowAcceptedResult pins immutable evidence accepted by the owner; it is not constructed
// directly from the model's declaration or a URL.
// WorkflowAcceptedResult закрепляет принятое владельцем доказательство; оно не создаётся
// непосредственно из заявления модели или URL.
type WorkflowAcceptedResult struct {
	TargetRef        string `json:"targetRef,omitempty"`
	BundleSHA256     string `json:"bundleSha256,omitempty"`
	ImageDigest      string `json:"imageDigest,omitempty"`
	HealthOK         bool   `json:"healthOk,omitempty"`
	Key              string `json:"key"`
	Kind             string `json:"kind"`
	ArtifactRef      string `json:"artifactRef,omitempty"`
	ArtifactRevision string `json:"artifactRevision,omitempty"`
	Digest           string `json:"digest"`
	ReceiptRef       string `json:"receiptRef,omitempty"`
	Repository       string `json:"repository,omitempty"`
	Number           int64  `json:"number,omitempty"`
	HeadSHA          string `json:"headSha,omitempty"`
	Verdict          string `json:"verdict,omitempty"`
}

// WorkflowReviewSubject is the closed published cause of a rejected review, not a retry command.
// It remains private owner journal data and contains no caller-selected node/run/agent authority.
// WorkflowReviewSubject — закрытая published cause отказа review, не retry command.
// Это private owner journal data без caller-selected node/run/agent authority.
type WorkflowReviewSubject struct {
	RequirementKey   string `json:"requirementKey"`
	SubjectStepKey   string `json:"subjectStepKey"`
	SubjectResultKey string `json:"subjectResultKey,omitempty"`
}

// WorkflowAttemptResult exposes the business verdict and immutable evidence of one completed attempt;
// RuntimeRevisionID and Provenance remain internal and are excluded from public transport projection.
// WorkflowAttemptResult передаёт бизнес-решение и immutable доказательства завершённой попытки;
// RuntimeRevisionID и Provenance хранятся внутри и не включаются в публичную transport projection.
type WorkflowAttemptResult struct {
	// EffectiveState describes current owner freshness without rewriting historical Accepted.
	// EffectiveState описывает current owner freshness без переписывания historical Accepted.
	EffectiveState string `json:"effectiveState,omitempty"`
	// ReworkSubjects persist typed rejection before short progress summaries can lose context.
	// They are excluded by explicit public casters and omitted for all historical receipts.
	// ReworkSubjects сохраняют typed rejection до усечения краткого progress summary.
	// Public casters их исключают; historical receipts не получают это поле.
	ReworkSubjects    []WorkflowReviewSubject       `json:"reworkSubjects,omitempty"`
	Attempt           int32                         `json:"attempt"`
	RunRef            string                        `json:"runRef"`
	RuntimeRevisionID string                        `json:"runtimeRevisionId"`
	Accepted          bool                          `json:"accepted"`
	Reason            string                        `json:"reason"`
	Results           []WorkflowAcceptedResult      `json:"results"`
	Provenance        *WorkflowCompletionProvenance `json:"provenance,omitempty"`
}

// WorkflowCompletionProvenance binds a business verdict to the exact completed physical execution.
// The database owner constructs these private pins; transport projections must never accept them
// from a model or expose them as permissions to dispatch another step.
// WorkflowCompletionProvenance связывает бизнес-решение с точным завершённым физическим исполнением.
// Владелец БД строит приватные pins; transport не принимает их от модели и не выдаёт как полномочия
// на запуск следующего шага.
type WorkflowCompletionProvenance struct {
	RootRunID      string `json:"rootRunId"`
	OriginNodeID   string `json:"originNodeId"`
	NodeID         string `json:"nodeId"`
	LeaseID        string `json:"leaseId"`
	Generation     int64  `json:"generation"`
	TurnID         string `json:"turnId"`
	RuntimeAttempt int32  `json:"runtimeAttempt"`
}

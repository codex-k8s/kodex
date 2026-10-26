package entity

// WorkflowResultRequirement declares one bounded mandatory output predicate; its references select
// owner-verified evidence and never grant authority supplied by an agent.
// WorkflowResultRequirement задаёт ограниченное обязательное условие; ссылки выбирают проверенные
// владельцем доказательства и не предоставляют полномочий, заявленных сотрудником.
type WorkflowResultRequirement struct {
	Key, Kind, FileName, MediaType, Repository, SubjectStepKey, Operation, TargetRef string
}

// WorkflowResultDeclaration is untrusted model output associated with an owner-assigned attempt.
// WorkflowResultDeclaration — недоверенный ответ модели, связанный с назначенной сервером попыткой.
type WorkflowResultDeclaration struct {
	Schema  string                   `json:"schema"`
	Results []WorkflowDeclaredResult `json:"results"`
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

// WorkflowAttemptResult exposes the business verdict and immutable evidence of one completed attempt;
// RuntimeRevisionID is retained internally and excluded from public transport projection.
// WorkflowAttemptResult передаёт бизнес-решение и immutable доказательства завершённой попытки;
// RuntimeRevisionID хранится внутри и не включается в публичную transport projection.
type WorkflowAttemptResult struct {
	Attempt           int32                    `json:"attempt"`
	RunRef            string                   `json:"runRef"`
	RuntimeRevisionID string                   `json:"runtimeRevisionId"`
	Accepted          bool                     `json:"accepted"`
	Reason            string                   `json:"reason"`
	Results           []WorkflowAcceptedResult `json:"results"`
}

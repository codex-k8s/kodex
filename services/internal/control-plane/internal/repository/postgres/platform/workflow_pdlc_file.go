package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowPDLCReadOrder verifies receipt timestamps only after all three proofs have passed
// the canonical owner lineage reader. It grants no access and must not replace that reader.
// queryWorkflowPDLCReadOrder проверяет timestamps receipts после канонического owner lineage
// reader для всех трёх proofs. Запрос не выдаёт доступ и не заменяет этот reader.
//
//go:embed sql/workflow_pdlc__read_order.sql
var queryWorkflowPDLCReadOrder string

const (
	// workflowRepositoryFileRequired rejects incomplete native file evidence without accepting artifacts as a substitute.
	// workflowRepositoryFileRequired отклоняет неполное native файловое доказательство без подмены артефактом.
	workflowRepositoryFileRequired = "REPOSITORY_FILE_REQUIRED"
	// workflowRepositoryFileMismatch denotes bytes or immutable PR/path pins that disagree with the published file intent.
	// workflowRepositoryFileMismatch обозначает несовпадение bytes либо immutable PR/path pins с опубликованным намерением.
	workflowRepositoryFileMismatch = "REPOSITORY_FILE_CONTENT_MISMATCH"
	// workflowPRCommentRequired rejects an absent or unrelated native pull-request comment effect.
	// workflowPRCommentRequired отклоняет отсутствующий либо несвязанный native effect комментария PR.
	workflowPRCommentRequired = "PR_COMMENT_REQUIRED"
)

// workflowPDLCFileWrite decodes only the existing GitHub file-write inputs, not execution authority.
// workflowPDLCFileWrite разбирает только существующие GitHub file-write inputs, не полномочия исполнения.
type workflowPDLCFileWrite struct {
	Path, Branch, Message string
	Content               string `json:"content_base64"`
	SHA                   string `json:"sha"`
}

// workflowPDLCHead keeps the authoritative PR identity used by immutable content verification.
// Draft may be true for document authors; the separate developer PULL_REQUEST predicate still requires Ready.
// workflowPDLCHead хранит авторитетную identity PR для immutable проверки содержимого.
// Draft допустим у авторов документов; отдельное условие PULL_REQUEST разработчика по-прежнему требует Ready.
type workflowPDLCHead struct {
	Number                 int64 `json:"number"`
	State, Head, Base, SHA string
	Draft                  *bool `json:"draft"`
}

// workflowPDLCContentPage mirrors the existing native full-source digest and bounded text page.
// SourceDigest attests fetched bytes in the trusted adapter, not semantic FULL reading by the model.
// workflowPDLCContentPage отражает существующие native full-source digest и ограниченную страницу.
// SourceDigest подтверждает полученные adapter bytes, не semantic FULL-чтение моделью.
type workflowPDLCContentPage struct {
	Path, Type, SHA, Text string
	Size                  int64
	CommitSHA             string `json:"commit_sha"`
	OffsetBytes           int64  `json:"offset_bytes"`
	NextOffsetBytes       int64  `json:"next_offset_bytes"`
	EOF                   bool   `json:"eof"`
	SourceDigest          string `json:"source_digest"`
	ChunkDigest           string `json:"chunk_digest"`
}

// resolveWorkflowRepositoryFileTx resolves every locator through the same owner boundary as
// existing workflow receipts, then requires fresh head/content reads after the native write.
// A lookup or chronology database failure remains unavailable, never successful file acceptance.
// resolveWorkflowRepositoryFileTx разрешает каждый locator через owner boundary существующих
// workflow receipts и требует чтения head/content после native записи. Ошибка БД остаётся
// unavailable и не превращается в успешную приёмку файла.
func resolveWorkflowRepositoryFileTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, requirement entity.WorkflowResultRequirement, declaration entity.WorkflowDeclaredResult) (entity.WorkflowAcceptedResult, string, error) {
	var proofs [3]workflowIntegrationResult
	for index, ref := range []string{declaration.ReceiptRef, declaration.HeadReceiptRef, declaration.ContentReceiptRef} {
		proof, reason, err := readWorkflowIntegrationResultTx(ctx, tx, current, lease, ref)
		if err != nil || reason != "" {
			return entity.WorkflowAcceptedResult{}, reason, err
		}
		proofs[index] = proof
	}
	result, reason := acceptWorkflowRepositoryFileResult(requirement, declaration, proofs[0], proofs[1], proofs[2])
	if reason != "" {
		return entity.WorkflowAcceptedResult{}, reason, nil
	}
	pins := pgx.StrictNamedArgs{"organization_id": current.organizationID, "write_receipt_ref": declaration.ReceiptRef, "head_receipt_ref": declaration.HeadReceiptRef, "content_receipt_ref": declaration.ContentReceiptRef}
	var ordered bool
	if err := tx.QueryRow(ctx, queryWorkflowPDLCReadOrder, pins).Scan(&ordered); err != nil {
		return entity.WorkflowAcceptedResult{}, "", errs.ErrUnavailable
	}
	if !ordered {
		return entity.WorkflowAcceptedResult{}, workflowRepositoryFileMismatch, nil
	}
	return result, "", nil
}

// workflowPDLCProofScope checks immutable proof bytes and the exact repository before parsing
// operation semantics. The caller separately resolves producing lineage and read chronology.
// workflowPDLCProofScope проверяет immutable proof bytes и точный repository до разбора semantics.
// Вызывающий отдельно разрешает producing lineage и хронологию чтений.
func workflowPDLCProofScope(proof workflowIntegrationResult, repository string) bool {
	_, valid := workflowResultObject(proof.Scope, []string{"owner", "repository"}, []string{"owner", "repository"})
	var scope map[string]string
	return valid && proof.LineageFailure == "" && json.Unmarshal(proof.Scope, &scope) == nil &&
		scope["owner"]+"/"+scope["repository"] == repository && proof.Digest == workflowResultSHA([]byte(proof.Summary))
}

// workflowPDLCResultHeadMatches binds the developer's Ready PR identity to the same immutable
// head as its mandatory file proof, independently of declaration or requirement ordering.
// workflowPDLCResultHeadMatches связывает Ready PR разработчика с тем же immutable head,
// что обязательное файловое доказательство, независимо от порядка declarations и requirements.
func workflowPDLCResultHeadMatches(results []entity.WorkflowAcceptedResult) bool {
	var file, pull entity.WorkflowAcceptedResult
	fileCount, pullCount := 0, 0
	for _, result := range results {
		switch result.Kind {
		case "REPOSITORY_FILE":
			file, fileCount = result, fileCount+1
		case "PULL_REQUEST":
			pull, pullCount = result, pullCount+1
		}
	}
	return fileCount <= 1 && (pullCount == 0 || pullCount == 1 && fileCount == 1 &&
		file.Repository == pull.Repository && file.Number == pull.Number && file.HeadSHA == pull.HeadSHA)
}

// acceptWorkflowRepositoryFileResult compares three already owner-resolved native proofs with
// the published file intent. It changes no state and cannot establish lineage or freshness by itself.
// acceptWorkflowRepositoryFileResult сопоставляет три уже разрешённых owner native proofs с
// опубликованным файловым намерением. Не меняет state и сама не доказывает lineage либо freshness.
func acceptWorkflowRepositoryFileResult(requirement entity.WorkflowResultRequirement, declaration entity.WorkflowDeclaredResult, write, head, content workflowIntegrationResult) (entity.WorkflowAcceptedResult, string) {
	if requirement.Kind != "REPOSITORY_FILE" || !workflowservice.ValidRepositoryBranch(requirement.Branch) ||
		!workflowservice.ValidRepositoryFilePath(requirement.FileName) || !workflowservice.RepositoryPattern.MatchString(requirement.Repository) ||
		!workflowPDLCProofScope(write, requirement.Repository) || !workflowPDLCProofScope(head, requirement.Repository) || !workflowPDLCProofScope(content, requirement.Repository) {
		return entity.WorkflowAcceptedResult{}, workflowRepositoryFileRequired
	}
	number, validNumber := workflowservice.RepositoryPullNumber(requirement.TargetRef)
	input, data, inputErr := parseWorkflowPDLCFileWrite(write.Operation, write.Input)
	if inputErr != nil {
		if errors.Is(inputErr, errs.ErrInvalid) {
			return entity.WorkflowAcceptedResult{}, workflowRepositoryFileMismatch
		}
		return entity.WorkflowAcceptedResult{}, workflowRepositoryFileRequired
	}
	_, validWriteOutput := workflowResultObject([]byte(write.Summary), []string{"sha", "path"}, []string{"sha", "path"})
	var output struct{ SHA, Path string }
	if !validNumber || !validWriteOutput || json.Unmarshal([]byte(write.Summary), &output) != nil ||
		input.Path != requirement.FileName || input.Branch != requirement.Branch ||
		output.Path != requirement.FileName || !workflowservice.CommitPattern.MatchString(output.SHA) || write.ProviderRef != "github-commit:"+output.SHA {
		return entity.WorkflowAcceptedResult{}, workflowRepositoryFileRequired
	}
	_, validHeadInput := workflowResultObject(head.Input, []string{"pull_request_number"}, []string{"pull_request_number"})
	_, validHeadOutput := workflowResultObject([]byte(head.Summary), []string{"number", "state", "head", "base", "sha", "draft"},
		[]string{"number", "title", "body", "state", "head", "base", "sha", "draft", "url", "base_sha", "changed_files"})
	var headInput struct {
		Number int64 `json:"pull_request_number"`
	}
	var pull workflowPDLCHead
	if head.Operation != "github.pull_request.read" || !validHeadInput || !validHeadOutput || json.Unmarshal(head.Input, &headInput) != nil || json.Unmarshal([]byte(head.Summary), &pull) != nil ||
		headInput.Number != number || pull.Number != number || pull.State != "open" || pull.Head != requirement.Branch || pull.Base == "" || pull.Base == pull.Head ||
		pull.Draft == nil || !workflowservice.CommitPattern.MatchString(pull.SHA) || head.ProviderRef != "github-pull:"+strconv.FormatInt(number, 10) {
		return entity.WorkflowAcceptedResult{}, workflowRepositoryFileMismatch
	}
	fields, validContentInput := workflowResultObject(content.Input, []string{"path", "ref"}, []string{"path", "ref", "expected_sha", "offset_bytes", "max_bytes"})
	_, validContentOutput := workflowResultObject([]byte(content.Summary), []string{"path", "type", "sha", "size", "text", "commit_sha", "offset_bytes", "next_offset_bytes", "eof", "source_digest", "chunk_digest"},
		[]string{"path", "type", "sha", "size", "text", "commit_sha", "offset_bytes", "next_offset_bytes", "eof", "source_digest", "chunk_digest"})
	var contentInput struct {
		Path, Ref   string
		ExpectedSHA string `json:"expected_sha"`
		OffsetBytes int64  `json:"offset_bytes"`
		MaxBytes    int64  `json:"max_bytes"`
	}
	var page workflowPDLCContentPage
	for _, raw := range fields {
		validContentInput = validContentInput && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
	}
	_, hasMaximum := fields["max_bytes"]
	_, hasExpected := fields["expected_sha"]
	if content.Operation != "github.repository.content.read" || !validContentInput || !validContentOutput || json.Unmarshal(content.Input, &contentInput) != nil || json.Unmarshal([]byte(content.Summary), &page) != nil ||
		contentInput.Path != requirement.FileName || contentInput.Ref != pull.SHA || contentInput.OffsetBytes != 0 || page.Path != requirement.FileName || page.Type != "file" ||
		hasMaximum && (contentInput.MaxBytes < 1 || contentInput.MaxBytes > 16<<10) || hasExpected && contentInput.ExpectedSHA == "" ||
		page.CommitSHA != pull.SHA || !workflowservice.CommitPattern.MatchString(page.SHA) || content.ProviderRef != "github-content:"+page.SHA ||
		contentInput.ExpectedSHA != "" && contentInput.ExpectedSHA != page.SHA || page.OffsetBytes != 0 || page.Size != int64(len(data)) ||
		page.NextOffsetBytes != int64(len(page.Text)) || page.NextOffsetBytes > page.Size || page.EOF != (page.NextOffsetBytes == page.Size) ||
		len(page.Text) > 16<<10 || hasMaximum && int64(len(page.Text)) > contentInput.MaxBytes ||
		page.Text == "" && !page.EOF ||
		page.SourceDigest != "sha256:"+workflowResultSHA(data) || page.ChunkDigest != "sha256:"+workflowResultSHA([]byte(page.Text)) || !bytes.HasPrefix(data, []byte(page.Text)) {
		return entity.WorkflowAcceptedResult{}, workflowRepositoryFileMismatch
	}
	digest := workflowPDLCResultDigest("kodex.workflow-repository-file.v1", requirement.Key, requirement.Repository, requirement.Branch, requirement.FileName, requirement.TargetRef,
		declaration.ReceiptRef, write.Digest, declaration.HeadReceiptRef, head.Digest, declaration.ContentReceiptRef, content.Digest)
	return entity.WorkflowAcceptedResult{Key: requirement.Key, Kind: requirement.Kind, Repository: requirement.Repository, FileName: requirement.FileName, Branch: requirement.Branch,
		Number: number, HeadSHA: pull.SHA, ReceiptRef: declaration.ReceiptRef, HeadReceiptRef: declaration.HeadReceiptRef, ContentReceiptRef: declaration.ContentReceiptRef, Digest: digest}, ""
}

package httptransport

import (
	"regexp"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

// workflowPredicateKey bounds public predicate identifiers; matching syntax is not evidence authority.
// workflowPredicateKey ограничивает публичный ключ условия; правильная форма не доказывает полномочий.
var workflowPredicateKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)

// workflowResultRequirements copies every public predicate into the owner command. References
// select evidence only; this adapter neither grants authority nor attests that results exist.
// workflowResultRequirements переносит все публичные условия в команду владельца. Ссылки
// выбирают доказательства; преобразователь не выдаёт полномочий и не подтверждает результат.
func workflowResultRequirements(values []generated.WorkflowResultRequirement) []*controlplanev1.WorkflowResultRequirement {
	result := make([]*controlplanev1.WorkflowResultRequirement, 0, len(values))
	for _, value := range values {
		operation := ""
		if value.Operation != nil {
			operation = string(*value.Operation)
		}
		result = append(result, &controlplanev1.WorkflowResultRequirement{
			EvidenceKey: stringValue(value.EvidenceKey), Key: value.Key, Kind: string(value.Kind), FileName: stringValue(value.FileName),
			MediaType: stringValue(value.MediaType), Repository: stringValue(value.Repository),
			SubjectStepKey: stringValue(value.SubjectStepKey), Operation: operation, TargetRef: stringValue(value.TargetRef),
		})
	}
	return result
}

// validWorkflowPublicMessage rejects unknown string-backed enums and oversized new workflow
// fields before the shared HTTP/WebSocket projection. Owner lifecycle and result provenance
// remain domain responsibilities; this check never infers a successful business outcome.
// validWorkflowPublicMessage отклоняет неизвестные строковые enum и превышение новых лимитов
// перед общей HTTP/WebSocket проекцией. Жизненный цикл и происхождение результата проверяет
// владелец; эта проверка не выводит успешность бизнес-шага.
func validWorkflowPublicMessage(message proto.Message) bool {
	switch value := message.(type) {
	case *controlplanev1.WorkflowVersion:
		if value == nil || !(value.ExecutionMode == "" || value.ExecutionMode == "MANAGER_DRIVEN" || value.ExecutionMode == "SERVER_DAG_V1") || !(value.ResultEvidenceProfile == "" || value.ResultEvidenceProfile == "GITHUB_REVIEW_QA_V1" && value.ExecutionMode == "SERVER_DAG_V1") {
			return false
		}
		for _, step := range value.Steps {
			if step == nil {
				return false
			}
			counts := map[string]int{}
			for _, requirement := range step.ResultRequirements {
				if requirement == nil {
					return false
				}
				if value.ResultEvidenceProfile == "" {
					if requirement.EvidenceKey != "" {
						return false
					}
					continue
				}
				if requirement.Kind == "REVIEW" || requirement.Kind == "QA" {
					counts[requirement.Kind]++
					if counts[requirement.Kind] > 1 {
						return false
					}
				}
				if requirement.Kind != "QA" {
					continue
				}
				found := false
				for _, candidate := range step.ResultRequirements {
					found = found || candidate != nil && candidate.Key == requirement.EvidenceKey && candidate.Kind == "ARTIFACT" && candidate.MediaType == "application/json"
				}
				if requirement.EvidenceKey == "" || !found {
					return false
				}
			}
		}
		return true
	case *controlplanev1.WorkflowStep:
		if value == nil || len(value.ResultRequirements) > 4 || len(value.DependsOn) > 200 {
			return false
		}
		seen := map[string]bool{}
		for _, dependency := range value.DependsOn {
			if !workflowPredicateKey.MatchString(dependency) || seen[dependency] {
				return false
			}
			seen[dependency] = true
		}
	case *controlplanev1.WorkflowResultRequirement:
		return value != nil && workflowPredicateKey.MatchString(value.Key) && (value.EvidenceKey == "" || value.Kind == "QA" && workflowPredicateKey.MatchString(value.EvidenceKey)) && workflowPublicResultKind(value.Kind) &&
			workflowPublicText(value.FileName, 160) && workflowPublicText(value.MediaType, 160) &&
			workflowPublicText(value.Repository, 200) && (value.SubjectStepKey == "" || workflowPredicateKey.MatchString(value.SubjectStepKey)) && workflowPublicText(value.TargetRef, 96) &&
			(value.Operation == "" || value.Operation == "github.pull_request.merge" || value.Operation == "ssh.compose.deploy")
	case *controlplanev1.RunNode:
		return value != nil && len(value.WorkflowResults) <= 4
	case *controlplanev1.WorkflowAttemptResult:
		if value == nil || value.Attempt < 1 || value.Attempt > 4 || !fileTargetRef(value.RunRef) || !workflowPublicText(value.Reason, 96) || len(value.Results) > 4 || len(value.Evidence) > 4 {
			return false
		}
		if !value.Accepted && len(value.Results) != 0 {
			return false
		}
		if len(value.Evidence) > 0 && (value.Accepted || value.Reason != "CHANGES_REQUIRED" || len(value.Results) != 0) {
			return false
		}
		for _, result := range value.Results {
			if result == nil || result.Verdict != "" && result.Verdict != "PASS" {
				return false
			}
		}
		negative := false
		for _, evidence := range value.Evidence {
			if evidence != nil && (evidence.Kind == "REVIEW" || evidence.Kind == "QA") && (!workflowPublicRepository.MatchString(evidence.Repository) || evidence.Number < 1 || !workflowPublicHead.MatchString(evidence.HeadSha)) {
				return false
			}
			if evidence == nil || evidence.Kind == "REVIEW" && (evidence.ReceiptRef == "" || evidence.HeadSha == "") || evidence.Kind == "QA" && (evidence.ArtifactRef == "" || evidence.ArtifactRevision == "" || evidence.HeadSha == "") {
				return false
			}
			negative = negative || (evidence.Kind == "REVIEW" || evidence.Kind == "QA") && evidence.Verdict == "CHANGES_REQUIRED"
		}
		return len(value.Evidence) == 0 || negative
	case *controlplanev1.WorkflowAcceptedResult:
		return value != nil && workflowPredicateKey.MatchString(value.Key) && workflowPublicResultKind(value.Kind) && validManagedDigest(value.Digest) &&
			(value.ArtifactRef == "" || fileTargetRef(value.ArtifactRef)) && (value.ReceiptRef == "" || fileTargetRef(value.ReceiptRef)) &&
			workflowPublicText(value.ArtifactRevision, 96) && workflowPublicText(value.Repository, 200) && workflowPublicText(value.TargetRef, 96) &&
			workflowPublicText(value.HeadSha, 64) && value.Number >= 0 && value.Number <= maximumSafeJSONInteger &&
			(value.Verdict == "" || value.Verdict == "PASS" || (value.Kind == "REVIEW" || value.Kind == "QA") && value.Verdict == "CHANGES_REQUIRED") && (value.BundleSha256 == "" || validManagedDigest(value.BundleSha256)) &&
			(value.ImageDigest == "" || strings.HasPrefix(value.ImageDigest, "sha256:") && validManagedDigest(strings.TrimPrefix(value.ImageDigest, "sha256:")))
	}
	return true
}

// workflowPublicResultKind mirrors the closed public evidence vocabulary without accepting
// provider-specific extensions or treating the kind as proof that evidence exists.
// workflowPublicResultKind сохраняет закрытый публичный набор доказательств без расширений
// провайдера и без признания самого вида доказательством существования результата.
func workflowPublicResultKind(kind string) bool {
	switch kind {
	case "ARTIFACT", "PULL_REQUEST", "REVIEW", "QA", "INTEGRATION_RECEIPT", "DEPLOYMENT":
		return true
	default:
		return false
	}
}

// workflowPublicText applies the OpenAPI code-point bound to valid UTF-8 text without rewriting it.
// workflowPublicText применяет предел OpenAPI в символах к корректному UTF-8 без изменения текста.
func workflowPublicText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}

// workflowPublicRepository validates only a public repository locator, never its authorization.
// workflowPublicRepository проверяет только public repository locator, не его authorization.
var workflowPublicRepository = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// workflowPublicHead rejects branch names and abbreviated hashes in new negative evidence.
// workflowPublicHead отвергает branch names и сокращённые hashes в новом negative evidence.
var workflowPublicHead = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)

package httptransport

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/controlplaneapi"
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
			Branch: stringValue(value.Branch), EvidenceKey: stringValue(value.EvidenceKey), Key: value.Key, Kind: string(value.Kind), FileName: stringValue(value.FileName),
			MediaType: stringValue(value.MediaType), Repository: stringValue(value.Repository),
			SubjectStepKey: stringValue(value.SubjectStepKey), SubjectResultKey: stringValue(value.SubjectResultKey), Operation: operation, TargetRef: stringValue(value.TargetRef),
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
		if value == nil || !(value.ReworkPolicy == "" || value.ExecutionMode == "SERVER_DAG_V1" && value.ReworkPolicy == "OWNER_APPROVED_UPSTREAM_V1") || !(value.ExecutionMode == "" || value.ExecutionMode == "MANAGER_DRIVEN" || value.ExecutionMode == "SERVER_DAG_V1") || !(value.ResultEvidenceProfile == "" || (value.ResultEvidenceProfile == "GITHUB_REVIEW_QA_V1" || value.ResultEvidenceProfile == "GITHUB_PDLC_V1") && value.ExecutionMode == "SERVER_DAG_V1") {
			return false
		}
		for _, step := range value.Steps {
			if step == nil {
				return false
			}
			if value.ResultEvidenceProfile == "GITHUB_PDLC_V1" && !validPublicPDLCStep(step) {
				return false
			}
			counts := map[string]int{}
			for _, requirement := range step.ResultRequirements {
				if requirement == nil {
					return false
				}
				if value.ResultEvidenceProfile == "" {
					if requirement.EvidenceKey != "" || requirement.Branch != "" || requirement.Kind == "REPOSITORY_FILE" || requirement.Kind == "PR_COMMENT" {
						return false
					}
					continue
				}
				if value.ResultEvidenceProfile != "GITHUB_PDLC_V1" && (requirement.Branch != "" || requirement.Kind == "REPOSITORY_FILE" || requirement.Kind == "PR_COMMENT") {
					return false
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
		if value.ResultEvidenceProfile == "GITHUB_REVIEW_QA_V1" || value.ResultEvidenceProfile == "GITHUB_PDLC_V1" {
			counts := map[string]int{}
			for _, step := range value.Steps {
				for _, requirement := range step.ResultRequirements {
					if requirement.Kind == "PULL_REQUEST" {
						counts[step.Ref]++
					}
				}
			}
			for _, step := range value.Steps {
				for _, requirement := range step.ResultRequirements {
					if (requirement.Kind == "REVIEW" || requirement.Kind == "QA") && (counts[requirement.SubjectStepKey] != 1 || !slices.Contains(step.DependsOn, requirement.SubjectStepKey)) {
						return false
					}
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
		if value != nil && (value.Kind == "REPOSITORY_FILE" || value.Kind == "PR_COMMENT") {
			return validPublicPDLCStep(&controlplanev1.WorkflowStep{Ref: value.Key, ResultRequirements: []*controlplanev1.WorkflowResultRequirement{value}})
		}
		return value != nil && workflowPredicateKey.MatchString(value.Key) && (value.EvidenceKey == "" || value.Kind == "QA" && workflowPredicateKey.MatchString(value.EvidenceKey)) && workflowPublicResultKind(value.Kind) &&
			value.Branch == "" && workflowPublicText(value.FileName, 160) && workflowPublicText(value.MediaType, 160) &&
			workflowPublicText(value.Repository, 200) && (value.SubjectStepKey == "" || workflowPredicateKey.MatchString(value.SubjectStepKey)) && workflowPublicText(value.TargetRef, 96) && (value.SubjectResultKey == "" || value.Kind == "ARTIFACT_REVIEW" && workflowPredicateKey.MatchString(value.SubjectResultKey)) &&
			(value.Operation == "" || value.Operation == "github.pull_request.merge" || value.Operation == "ssh.compose.deploy")
	case *controlplanev1.RunNode:
		return value != nil && len(value.WorkflowResults) <= 4
	case *controlplanev1.WorkflowAttemptResult:
		if value == nil || !workflowPublicEffectiveState(value.EffectiveState) || value.Attempt < 1 || value.Attempt > 4 || !fileTargetRef(value.RunRef) || !workflowPublicText(value.Reason, 96) || len(value.Results) > 4 || len(value.Evidence) > 4 {
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
			if evidence == nil || evidence.Kind == "ARTIFACT_REVIEW" && (evidence.ArtifactRef == "" || evidence.ArtifactRevision == "") || evidence.Kind == "REVIEW" && (evidence.ReceiptRef == "" || evidence.HeadSha == "") || evidence.Kind == "QA" && (evidence.ArtifactRef == "" || evidence.ArtifactRevision == "" || evidence.HeadSha == "") {
				return false
			}
			negative = negative || (evidence.Kind == "ARTIFACT_REVIEW" || evidence.Kind == "REVIEW" || evidence.Kind == "QA") && evidence.Verdict == "CHANGES_REQUIRED"
		}
		return len(value.Evidence) == 0 || negative
	case *controlplanev1.WorkflowAcceptedResult:
		if value != nil {
			if value.Kind == "REPOSITORY_FILE" {
				if !controlplaneapi.ValidWorkflowPublicationRepositoryFileTarget(value.Repository, value.Branch, value.FileName, strconv.FormatInt(value.Number, 10)) || !workflowPublicHead.MatchString(value.HeadSha) ||
					!fileTargetRef(value.ReceiptRef) || !fileTargetRef(value.HeadReceiptRef) || !fileTargetRef(value.ContentReceiptRef) || value.ReceiptRef == value.HeadReceiptRef || value.ReceiptRef == value.ContentReceiptRef || value.HeadReceiptRef == value.ContentReceiptRef ||
					value.ArtifactRef != "" || value.ArtifactRevision != "" || value.Verdict != "" || value.TargetRef != "" || value.BundleSha256 != "" || value.ImageDigest != "" || value.HealthOk {
					return false
				}
			} else if value.FileName != "" || value.Branch != "" || value.HeadReceiptRef != "" || value.ContentReceiptRef != "" {
				return false
			}
			if value.Kind == "PR_COMMENT" && (!workflowPublicRepository.MatchString(value.Repository) || value.Number < 1 || value.Number > 2147483647 || !fileTargetRef(value.ReceiptRef) || value.HeadSha != "" || value.Verdict != "" || value.ArtifactRef != "" || value.ArtifactRevision != "" || value.TargetRef != "" || value.BundleSha256 != "" || value.ImageDigest != "" || value.HealthOk) {
				return false
			}
		}
		return value != nil && workflowPredicateKey.MatchString(value.Key) && workflowPublicResultKind(value.Kind) && validManagedDigest(value.Digest) &&
			(value.ArtifactRef == "" || fileTargetRef(value.ArtifactRef)) && (value.ReceiptRef == "" || fileTargetRef(value.ReceiptRef)) &&
			workflowPublicText(value.ArtifactRevision, 96) && workflowPublicText(value.Repository, 200) && workflowPublicText(value.TargetRef, 96) &&
			workflowPublicText(value.HeadSha, 64) && value.Number >= 0 && value.Number <= maximumSafeJSONInteger &&
			(value.Verdict == "" || value.Verdict == "PASS" || (value.Kind == "ARTIFACT_REVIEW" || value.Kind == "REVIEW" || value.Kind == "QA") && value.Verdict == "CHANGES_REQUIRED") && (value.BundleSha256 == "" || validManagedDigest(value.BundleSha256)) &&
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
	case "ARTIFACT", "ARTIFACT_REVIEW", "REPOSITORY_FILE", "PR_COMMENT", "PULL_REQUEST", "REVIEW", "QA", "INTEGRATION_RECEIPT", "DEPLOYMENT":
		return true
	default:
		return false
	}
}

// validPublicPDLCStep reuses the canonical closed publication grammar for owner HTTP projection.
// This checks shape and exact targets only; the owner remains responsible for access and effects.
// validPublicPDLCStep использует каноническую закрытую грамматику публикации для HTTP projection.
// Проверяет только форму и exact targets; доступ и effects по-прежнему проверяет owner.
func validPublicPDLCStep(step *controlplanev1.WorkflowStep) bool {
	if step == nil {
		return false
	}
	projection := controlplaneapi.WorkflowPublicationStep{Key: step.Ref, DependsOn: step.DependsOn, RequiredCapabilityKeys: step.RequiredCapabilityKeys}
	for _, requirement := range step.ResultRequirements {
		if requirement == nil {
			return false
		}
		projection.ResultRequirements = append(projection.ResultRequirements, controlplaneapi.WorkflowPublicationRequirement{Key: requirement.Key, Kind: requirement.Kind, FileName: requirement.FileName, Branch: requirement.Branch, Repository: requirement.Repository, TargetRef: requirement.TargetRef,
			MediaType: requirement.MediaType, SubjectStepKey: requirement.SubjectStepKey, SubjectResultKey: requirement.SubjectResultKey, EvidenceKey: requirement.EvidenceKey, Operation: requirement.Operation})
	}
	return controlplaneapi.ValidWorkflowPublicationServerStepForProfile(projection, "GITHUB_PDLC_V1")
}

// workflowPublicText applies the OpenAPI code-point bound to valid UTF-8 text without rewriting it.
// workflowPublicText применяет предел OpenAPI в символах к корректному UTF-8 без изменения текста.
func workflowPublicText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}

// workflowPublicEffectiveState accepts the additive owner freshness vocabulary; empty is legacy.
// workflowPublicEffectiveState принимает additive owner freshness; пустое значение означает legacy.
func workflowPublicEffectiveState(value string) bool {
	switch value {
	case "", "CURRENT", "HISTORICAL", "STALE_DEPENDENCY", "REWORK_PENDING", "WAITING_OWNER", "BLOCKED":
		return true
	default:
		return false
	}
}

// workflowPublicRepository validates only a public repository locator, never its authorization.
// workflowPublicRepository проверяет только public repository locator, не его authorization.
var workflowPublicRepository = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// workflowPublicHead rejects branch names and abbreviated hashes in new negative evidence.
// workflowPublicHead отвергает branch names и сокращённые hashes в новом negative evidence.
var workflowPublicHead = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)

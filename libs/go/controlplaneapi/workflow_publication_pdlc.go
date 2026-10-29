package controlplaneapi

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// workflowPDLCProfile names the explicit fourth-version file/comment publication contract.
// workflowPDLCProfile обозначает явный контракт публикации файлов и комментариев четвёртой версии.
const workflowPDLCProfile = "GITHUB_PDLC_V1"

// workflowPublicationBranchPattern bounds transport branch names without authorizing a write.
// workflowPublicationBranchPattern ограничивает имена веток при передаче, не разрешая запись.
var workflowPublicationBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./-]{0,254}$`)

// workflowPublicationNativeEvidence selects the two closed native-evidence contracts only.
// workflowPublicationNativeEvidence выбирает только два закрытых контракта native evidence.
func workflowPublicationNativeEvidence(profile string) bool {
	return profile == "GITHUB_REVIEW_QA_V1" || profile == workflowPDLCProfile
}

// workflowPublicationPullNumber rejects aliases, leading zeroes and provider integer overflow.
// workflowPublicationPullNumber отвергает псевдонимы, ведущие нули и переполнение номера провайдера.
func workflowPublicationPullNumber(value string) bool {
	number, err := strconv.ParseInt(value, 10, 32)
	return err == nil && number > 0 && strconv.FormatInt(number, 10) == value
}

// workflowPublicationFilePath preserves a bounded exact UTF8 repository path without traversal.
// workflowPublicationFilePath сохраняет ограниченный точный UTF8 путь репозитория без traversal.
func workflowPublicationFilePath(value string) bool {
	if len(value) < 1 || len(value) > 1024 || !utf8.ValidString(value) || strings.ContainsAny(value, "\\\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// workflowPublicationBranch excludes ref aliases and ambiguous components before commitment.
// workflowPublicationBranch исключает ref aliases и неоднозначные компоненты до вычисления digest.
func workflowPublicationBranch(value string) bool {
	if !workflowPublicationBranchPattern.MatchString(value) || strings.HasPrefix(value, "refs/") || strings.Contains(value, "..") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// validWorkflowPublicationPDLCTargets pins one file and comment per step to the same existing PR;
// a Ready PR predicate requires file evidence and never implies permission to create another PR.
// validWorkflowPublicationPDLCTargets закрепляет файл и комментарий шага за одним существующим PR;
// условие Ready PR требует файловое доказательство и не разрешает создание другого PR.
func validWorkflowPublicationPDLCTargets(requirements []WorkflowPublicationRequirement) bool {
	counts := map[string]int{}
	var repository, target string
	for _, requirement := range requirements {
		if requirement.Kind != "REPOSITORY_FILE" && requirement.Kind != "PR_COMMENT" && requirement.Kind != "PULL_REQUEST" {
			continue
		}
		counts[requirement.Kind]++
		if counts[requirement.Kind] > 1 || repository != "" && (repository != requirement.Repository || target != requirement.TargetRef) {
			return false
		}
		repository, target = requirement.Repository, requirement.TargetRef
	}
	return counts["PULL_REQUEST"] == 0 || counts["REPOSITORY_FILE"] == 1
}

package workflow

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// GitHubPDLCV1 requires native repository-file and PR-comment evidence in an explicitly
// published profile. It does not change grants or reinterpret historical workflow versions.
// GitHubPDLCV1 требует native доказательства файла репозитория и комментария PR в явно
// опубликованном профиле. Не меняет grants и не переинтерпретирует прежние версии процессов.
const GitHubPDLCV1 = "GITHUB_PDLC_V1"

// pdlcBranchPattern bounds the initial profile to plain branch names, never full Git ref aliases.
// pdlcBranchPattern ограничивает первый профиль обычными именами веток, исключая полные Git ref.
var pdlcBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./-]{0,254}$`)

// NativeReviewEvidence selects profiles with strict COMMENT/QA receipts; absence preserves the
// original head/verdict-only declaration contract and cannot enable native effect slots.
// NativeReviewEvidence выбирает профили со строгими COMMENT/QA receipts; отсутствие сохраняет
// прежний контракт head/verdict и не включает native effect slots.
func NativeReviewEvidence(profile string) bool {
	return profile == GitHubReviewQAV1 || profile == GitHubPDLCV1
}

// ValidRepositoryFilePath accepts a bounded exact repository path without filesystem traversal,
// separators with platform-dependent meaning or ignored empty/dot components.
// ValidRepositoryFilePath принимает ограниченный точный путь репозитория без filesystem traversal,
// неоднозначных разделителей и игнорируемых пустых либо точечных компонентов.
func ValidRepositoryFilePath(value string) bool {
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

// ValidRepositoryBranch validates only the selected branch syntax; current default/protection
// and existence are still checked by the GitHub adapter before autonomous external writes.
// ValidRepositoryBranch проверяет только синтаксис выбранной ветки; текущие default/protection
// и существование по-прежнему проверяет GitHub adapter до автономной внешней записи.
func ValidRepositoryBranch(value string) bool {
	if !pdlcBranchPattern.MatchString(value) || strings.HasPrefix(value, "refs/") || strings.Contains(value, "..") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// RepositoryPullNumber parses the exact provider-supported positive decimal selector, excluding
// URLs, aliases and leading-zero spellings before any native receipt or repository lookup.
// RepositoryPullNumber разбирает точный положительный десятичный номер провайдера, исключая
// URL, псевдонимы и ведущие нули до проверки native receipt либо репозитория.
func RepositoryPullNumber(value string) (int64, bool) {
	number, err := strconv.ParseInt(value, 10, 32)
	return number, err == nil && number > 0 && strconv.FormatInt(number, 10) == value
}

// validPDLCRequirementRefs prevents ambiguous file authority and mismatched same-step PR
// selectors. A developer PR predicate also requires a native file predicate in this profile.
// validPDLCRequirementRefs запрещает неоднозначные файловые полномочия и несовпадающие номера PR
// одного шага. Условие PR разработчика в этом профиле также требует native файловое условие.
func validPDLCRequirementRefs(profile string, requirements []entity.WorkflowResultRequirement) bool {
	if profile != GitHubPDLCV1 {
		return true
	}
	counts := map[string]int{}
	var repository, target string
	for _, r := range requirements {
		if r.Kind != "REPOSITORY_FILE" && r.Kind != "PR_COMMENT" && r.Kind != "PULL_REQUEST" {
			continue
		}
		counts[r.Kind]++
		if counts[r.Kind] > 1 || repository != "" && (repository != r.Repository || target != r.TargetRef) {
			return false
		}
		repository, target = r.Repository, r.TargetRef
	}
	return counts["PULL_REQUEST"] == 0 || counts["REPOSITORY_FILE"] == 1
}

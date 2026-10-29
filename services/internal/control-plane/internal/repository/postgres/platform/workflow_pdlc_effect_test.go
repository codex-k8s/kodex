package platform

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// workflowPDLCEffectStep pins one existing PR and a published author branch without granting
// access to arbitrary repositories, branches or merge/deploy operations.
// workflowPDLCEffectStep закрепляет существующий PR и опубликованную ветку автора без доступа
// к произвольным repository/branch либо операциям merge/deploy.
func workflowPDLCEffectStep() entity.WorkflowStep {
	return entity.WorkflowStep{Key: "author", ResultRequirements: []entity.WorkflowResultRequirement{
		{Key: "document", Kind: "REPOSITORY_FILE", Repository: "acme/repo", Branch: "feature/journal", FileName: "docs/requirements.md", TargetRef: "3"},
		{Key: "response", Kind: "PR_COMMENT", Repository: "acme/repo", TargetRef: "3"},
		{Key: "pr", Kind: "PULL_REQUEST", Repository: "acme/repo", TargetRef: "3"},
	}}
}

// TestWorkflowPDLCPublishedEffectSlots exercises the real slot selector, not a separate allowlist.
// It permits several document paths on the owner-published branch and closes target/schema drift.
// TestWorkflowPDLCPublishedEffectSlots проверяет настоящий selector, не отдельный allowlist.
// Допускает несколько document paths в owner-published branch и закрывает target/schema drift.
func TestWorkflowPDLCPublishedEffectSlots(t *testing.T) {
	for _, mode := range []string{"file-create", "file-update", "another-file", "comment", "ready", "foreign-repository", "foreign-branch", "invalid-path", "invalid-base64", "duplicate-input", "null-input", "unknown-input", "wrong-comment-pr", "blank-comment", "wrong-ready-pr", "ready-branch-alias", "unpublished-create", "unpublished-merge", "legacy-profile"} {
		t.Run(mode, func(t *testing.T) {
			step := workflowPDLCEffectStep()
			profile := workflowservice.GitHubPDLCV1
			op := "github.repository.content.create"
			scope := []byte(`{"owner":"acme","repository":"repo"}`)
			input := map[string]any{"path": "docs/requirements.md", "branch": "feature/journal", "message": "Уточнить требования", "content_base64": base64.StdEncoding.EncodeToString([]byte("Требования\n"))}
			want := "file_" + workflowResultSHA([]byte("document\x00docs/requirements.md"))
			switch mode {
			case "file-update":
				op, input["sha"] = "github.repository.content.update", strings.Repeat("a", 40)
			case "another-file":
				input["path"] = "docs/acceptance.md"
				want = "file_" + workflowResultSHA([]byte("document\x00docs/acceptance.md"))
			case "comment", "wrong-comment-pr", "blank-comment":
				op, want = "github.pull_request.comment.create", "response"
				input = map[string]any{"pull_request_number": 3, "body": "Документация обновлена"}
				if mode == "wrong-comment-pr" {
					input["pull_request_number"] = 4
				} else if mode == "blank-comment" {
					input["body"] = " \n "
				}
			case "ready", "wrong-ready-pr", "ready-branch-alias":
				op, want = "github.pull_request.ready", "ready_"+workflowResultSHA([]byte("pr"))
				input = map[string]any{"pull_request_number": 3, "expected_head_sha": strings.Repeat("a", 40)}
				if mode == "wrong-ready-pr" {
					input["pull_request_number"] = 4
				} else if mode == "ready-branch-alias" {
					input["expected_head_sha"] = "feature/journal"
				}
			case "foreign-repository":
				scope = []byte(`{"owner":"acme","repository":"foreign"}`)
			case "foreign-branch":
				input["branch"] = "main"
			case "invalid-path":
				input["path"] = "docs/../.env"
			case "invalid-base64":
				input["content_base64"] = "not:base64"
			case "null-input":
				input["content_base64"] = nil
			case "unknown-input":
				input["actor"] = "owner"
			case "unpublished-create":
				op = "github.pull_request.create"
			case "unpublished-merge":
				op = "github.pull_request.merge"
			case "legacy-profile":
				profile = ""
			}
			raw := asJSON(input)
			if mode == "duplicate-input" {
				raw = []byte(`{"path":"docs/other.md","path":"docs/requirements.md","branch":"feature/journal","message":"Уточнить","content_base64":"VGV4dA=="}`)
			}
			key, err := workflowEffectSlotForProfile(profile, step, nil, op, scope, raw)
			positive := contains([]string{"file-create", "file-update", "another-file", "comment", "ready"}, mode)
			if positive {
				if err != nil || key != want {
					t.Fatalf("published native effect slot unavailable: case=%s key=%s err=%v", mode, key, err)
				}
			} else if err == nil || key != "" {
				t.Fatal("unpublished native effect admitted")
			}
		})
	}
}

// TestWorkflowPDLCFilesRequireExplicitProfile preserves the old profile's operation denial
// when its otherwise valid historical PR requirement has no new file/branch publication.
// TestWorkflowPDLCFilesRequireExplicitProfile сохраняет отказ операции прежнего профиля,
// если его корректный старый PR requirement не публикует новый file/branch selector.
func TestWorkflowPDLCFilesRequireExplicitProfile(t *testing.T) {
	step := entity.WorkflowStep{ResultRequirements: []entity.WorkflowResultRequirement{{Key: "pr", Kind: "PULL_REQUEST", Repository: "acme/repo"}}}
	for _, op := range []string{"github.repository.content.create", "github.repository.content.update", "github.pull_request.comment.create", "github.pull_request.ready"} {
		_, err := workflowEffectSlot(step, nil, op, []byte(`{"owner":"acme","repository":"repo"}`), []byte(`{}`))
		if !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("legacy workflow gained a new autonomous write")
		}
	}
}

// TestWorkflowPDLCFileSemanticIntent preserves desired bytes across create/update, commit-message
// changes and fresh blob preconditions; a different path, branch, scope or content stays distinct.
// TestWorkflowPDLCFileSemanticIntent сохраняет желаемые bytes между create/update, изменениями
// commit message и свежими blob preconditions; иной path, branch, scope либо content отличим.
func TestWorkflowPDLCFileSemanticIntent(t *testing.T) {
	base := map[string]any{"path": "docs/spec.md", "branch": "feature/journal", "message": "Уточнить документ", "content_base64": base64.StdEncoding.EncodeToString([]byte("Точные требования\n"))}
	first, err := workflowEffectSemanticDigestForProfile(workflowservice.GitHubPDLCV1, "file-key", "github.repository.content.create", "scope", asJSON(base))
	if err != nil || first == "" {
		t.Fatal("canonical file intent unavailable")
	}
	for _, mode := range []string{"update", "message", "blob", "content", "path", "branch", "scope", "result-key"} {
		t.Run(mode, func(t *testing.T) {
			input := map[string]any{}
			for key, value := range base {
				input[key] = value
			}
			op, scope, key := "github.repository.content.update", "scope", "file-key"
			input["sha"] = strings.Repeat("a", 40)
			switch mode {
			case "message":
				input["message"] = "Новое описание того же изменения"
			case "blob":
				input["sha"] = strings.Repeat("b", 40)
			case "content":
				input["content_base64"] = base64.StdEncoding.EncodeToString([]byte("Уточнённые требования\n"))
			case "path":
				input["path"] = "docs/other.md"
			case "branch":
				input["branch"] = "feature/other"
			case "scope":
				scope = "other-scope"
			case "result-key":
				key = "other-key"
			}
			actual, err := workflowEffectSemanticDigestForProfile(workflowservice.GitHubPDLCV1, key, op, scope, asJSON(input))
			same := contains([]string{"update", "message", "blob"}, mode)
			if err != nil || (actual == first) != same {
				t.Fatal("file intent lost semantic distinction", mode)
			}
		})
	}
	for _, profile := range []string{"", workflowservice.GitHubReviewQAV1} {
		actual, err := workflowEffectSemanticDigestForProfile(profile, "file-key", "github.repository.content.create", "scope", asJSON(base))
		original, originalErr := workflowEffectSemanticDigest("file-key", "github.repository.content.create", "scope", asJSON(base))
		if err != nil || originalErr != nil || actual != original {
			t.Fatal("historical complete-input semantic bytes changed")
		}
	}
}

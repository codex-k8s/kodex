package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

// githubReadyMutation is the sole GraphQL document accepted by the Ready adapter; callers cannot supply queries, node IDs, or merge instructions.
// githubReadyMutation — единственный GraphQL document адаптера Ready; caller не передаёт запросы, node IDs либо указания на слияние.
const githubReadyMutation = `mutation KodexReady($input: MarkPullRequestReadyForReviewInput!) { markPullRequestReadyForReview(input: $input) { pullRequest { id } } }`

// githubReadyView reports observed Ready state at one exact head, distinguishing a pre-existing Ready state from a transition observed after the single mutation.
// githubReadyView сообщает наблюдённое состояние Ready на точном head, различая исходно готовый PR и переход после единственного изменения.
type githubReadyView struct {
	Number       int    `json:"number"`
	SHA          string `json:"sha"`
	State        string `json:"state"`
	Draft        bool   `json:"draft"`
	URL          string `json:"url"`
	AlreadyReady bool   `json:"already_ready"`
}

// executeGitHubReady resolves the node from the granted repository and exact head, performs at most one closed mutation, and accepts only a fresh matching Ready readback; recovery is strictly read-only.
// executeGitHubReady разрешает node из выданного репозитория и точного head, выполняет не более одного закрытого изменения и принимает лишь свежее совпадающее Ready-чтение; восстановление строго read-only.
// GitHub exposes no head CAS for this operation. A concurrent head change after the request remains UNKNOWN and cannot qualify a workflow result or permit a repeated mutation.
// GitHub не предоставляет head CAS этой операции. Смена head после запроса сохраняет UNKNOWN и не допускает результат workflow либо повтор изменения.
func executeGitHubReady(ctx context.Context, client *github.Client, owner, repo string, request Request, capability integrationpackage.Capability, in githubCatalogInput, recovery bool) (Result, error) {
	if !sourceCommitPattern.MatchString(in.ExpectedHeadSHA) {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	read := func() (*github.PullRequest, error) {
		pull, err := githubRead(ctx, capability, func() (*github.PullRequest, *github.Response, error) {
			return client.PullRequests.Get(ctx, owner, repo, in.Number)
		})
		if err != nil {
			return nil, err
		}
		if pull == nil || pull.GetNumber() != in.Number || pull.Draft == nil || pull.GetState() != "open" ||
			pull.GetHead().GetSHA() != in.ExpectedHeadSHA || pull.GetNodeID() == "" || len(pull.GetNodeID()) > 256 ||
			strings.ContainsAny(pull.GetNodeID(), " \t\r\n\x00") || pull.GetHTMLURL() != "https://github.com/"+owner+"/"+repo+"/pull/"+strconv.Itoa(in.Number) {
			return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		return pull, nil
	}
	current, err := read()
	if err != nil {
		if recovery {
			return Result{}, &UnknownOutcomeError{}
		}
		return Result{}, err
	}
	alreadyReady := !current.GetDraft()
	if !alreadyReady {
		if recovery {
			return Result{}, &UnknownOutcomeError{}
		}
		body := map[string]any{"query": githubReadyMutation, "variables": map[string]any{"input": map[string]string{"pullRequestId": current.GetNodeID(), "clientMutationId": request.EffectKey}}}
		mutation, err := client.NewRequest(http.MethodPost, "graphql", body)
		if err != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		// The GraphQL body is not evidence of success; only the authoritative REST readback below may establish the resulting state.
		// GraphQL body не является доказательством успеха; итоговое состояние подтверждает только авторитетный REST readback ниже.
		response, mutationErr := client.Do(ctx, mutation, nil)
		if err := githubMutationError(response, mutationErr); err != nil && !IsUnknownOutcome(err) {
			return Result{}, err
		}
		after, err := read()
		if err != nil || after.GetNodeID() != current.GetNodeID() || after.GetDraft() {
			return Result{}, &UnknownOutcomeError{}
		}
		current = after
	}
	return providerResult(request, "github-pull:"+strconv.Itoa(in.Number), githubReadyView{current.GetNumber(), current.GetHead().GetSHA(), current.GetState(), current.GetDraft(), current.GetHTMLURL(), alreadyReady})
}

// recoverGitHubReady opens the same pinned provider client and delegates only to the read-only branch of the closed Ready adapter, retaining native output validation in Recover.
// recoverGitHubReady открывает тот же закреплённый provider client и вызывает только read-only ветку закрытого Ready адаптера; native выход проверяется в Recover.
func (adapter *Adapter) recoverGitHubReady(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, raw []byte) (Result, error) {
	var input githubCatalogInput
	if json.Unmarshal(raw, &input) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	client, cleanup, err := adapter.githubClient(ctx, request.Credential)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	return executeGitHubReady(ctx, client, configuration["owner"], configuration["repository"], request, capability, input, true)
}

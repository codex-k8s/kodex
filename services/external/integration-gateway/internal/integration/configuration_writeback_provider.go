package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func (execution *ConfigurationWriteBackExecution) providerJSON(ctx context.Context, method, path string, query url.Values, input, output any) error {
	base, client := execution.adapter.githubBaseURL, execution.adapter.githubHTTPClient
	if execution.definition.Spec.Adapter == "GITLAB" {
		var err error
		base, err = parseProviderBaseURL(execution.configuration["base_url"])
		if err != nil {
			return errWriteBackGit
		}
		client = execution.adapter.providerHTTPClient
	}
	endpoint := *base
	decoded, err := url.PathUnescape(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return errWriteBackGit
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery = decoded, path, query.Encode()
	var body []byte
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil || len(body) > 512<<10 {
			return errWriteBackGit
		}
	}
	defer clear(body)
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return errWriteBackGit
	}
	request.Header.Set("Authorization", "Bearer "+string(execution.credential))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := client.Do(request)
	if err != nil {
		return writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_UNAVAILABLE)
	}
	if response == nil || response.Body == nil {
		return errWriteBackGit
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_CREDENTIAL_REJECTED)
	}
	if response.StatusCode == http.StatusForbidden {
		return writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_ACCESS_DENIED)
	}
	if method == http.MethodGet && response.StatusCode != http.StatusOK || method == http.MethodPost && response.StatusCode != http.StatusCreated {
		return writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_UNAVAILABLE)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumSourceResponseBytes+1))
	defer clear(raw)
	if err != nil || len(raw) == 0 || len(raw) > maximumSourceResponseBytes {
		return errWriteBackGit
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(output) != nil {
		return errWriteBackGit
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return errWriteBackGit
	}
	return nil
}

func (execution *ConfigurationWriteBackExecution) VerifyBranch(ctx context.Context, candidate ConfigurationWriteBackCandidate) error {
	ctx, cancel := context.WithDeadline(ctx, execution.deadline)
	defer cancel()
	p := execution.work.GetProposal()
	if !sourceCommitPattern.MatchString(candidate.CommitSHA) {
		return errWriteBackGit
	}
	reader := configurationSourceReader{adapter: execution.adapter, credential: execution.credential, maximumContent: maximumSourceContentBytes}
	work := &cp.ManagedConfigurationSourceWork{RefName: p.GetProposalBranch(), Path: p.GetPath()}
	// После merge provider может удалить proposal branch; PR recovery читает immutable commit.
	if execution.work.GetEffect() == cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_PULL_REQUEST && execution.work.GetMode() == cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_RECOVER_READ_ONLY {
		work.RefName = candidate.CommitSHA
	}
	var result ConfigurationSourceResult
	var err error
	if execution.definition.Spec.Adapter == "GITHUB" {
		if err = execution.validateInput("github.repository.content.read", map[string]any{"ref": p.GetProposalBranch(), "path": p.GetPath()}); err != nil {
			return err
		}
		result, err = reader.readGitHub(ctx, execution.configuration, work)
	} else {
		if err = execution.validateInput("gitlab.repository.file.read", map[string]any{"ref": p.GetProposalBranch(), "file_path": p.GetPath()}); err != nil {
			return err
		}
		result, err = reader.readGitLab(ctx, execution.configuration, work)
	}
	defer clear(result.Content)
	if err != nil {
		return err
	}
	if result.CommitSHA != candidate.CommitSHA || sourceContentDigest(result.Content) != p.GetProposedContentSha256() {
		return writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_RESPONSE_INVALID)
	}
	if execution.definition.Spec.Adapter == "GITHUB" {
		var commit struct {
			SHA  string `json:"sha"`
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
			Parents []struct {
				SHA string `json:"sha"`
			} `json:"parents"`
		}
		err = execution.providerJSON(ctx, http.MethodGet, execution.githubRoot()+"/git/commits/"+candidate.CommitSHA, nil, nil, &commit)
		if err != nil {
			return err
		}
		if commit.SHA != candidate.CommitSHA || commit.Tree.SHA != candidate.TreeSHA || len(commit.Parents) != 1 || commit.Parents[0].SHA != p.GetBaseCommitSha() {
			return errWriteBackGit
		}
	} else {
		var commit struct {
			ID      string   `json:"id"`
			Parents []string `json:"parent_ids"`
		}
		err = execution.providerJSON(ctx, http.MethodGet, execution.gitlabRoot()+"/repository/commits/"+candidate.CommitSHA, nil, nil, &commit)
		if err != nil {
			return err
		}
		if commit.ID != candidate.CommitSHA || len(commit.Parents) != 1 || commit.Parents[0] != p.GetBaseCommitSha() {
			return errWriteBackGit
		}
	}
	return nil
}

func (execution *ConfigurationWriteBackExecution) githubRoot() string {
	return "/repos/" + url.PathEscape(execution.configuration["owner"]) + "/" + url.PathEscape(execution.configuration["repository"])
}
func (execution *ConfigurationWriteBackExecution) gitlabRoot() string {
	return "/api/v4/projects/" + url.PathEscape(execution.configuration["project_path"])
}

type githubWriteBackPR struct {
	Number int64  `json:"number"`
	URL    string `json:"html_url"`
	Body   string `json:"body"`
	Head   struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"base"`
}
type gitlabWriteBackPR struct {
	IID           int64  `json:"iid"`
	URL           string `json:"web_url"`
	Description   string `json:"description"`
	SourceBranch  string `json:"source_branch"`
	TargetBranch  string `json:"target_branch"`
	SHA           string `json:"sha"`
	SourceProject int64  `json:"source_project_id"`
	TargetProject int64  `json:"target_project_id"`
	References    struct {
		Full string `json:"full"`
	} `json:"references"`
}

func (execution *ConfigurationWriteBackExecution) githubPR(value githubWriteBackPR, commit string) (ConfigurationWriteBackPullRequest, error) {
	p := execution.work.GetProposal()
	ref := strconv.FormatInt(value.Number, 10)
	if value.Number < 1 || value.Body != execution.work.GetEffectMarker() || value.Head.Ref != p.GetProposalBranch() || value.Head.SHA != commit || value.Base.Ref != p.GetSourceRefName() || value.Head.Repo.FullName != p.GetRepositoryRef() || value.Base.Repo.FullName != p.GetRepositoryRef() || value.URL != "https://github.com/"+p.GetRepositoryRef()+"/pull/"+ref {
		return ConfigurationWriteBackPullRequest{}, errWriteBackGit
	}
	return ConfigurationWriteBackPullRequest{Ref: ref, URL: value.URL}, nil
}
func (execution *ConfigurationWriteBackExecution) gitlabPR(value gitlabWriteBackPR, commit string) (ConfigurationWriteBackPullRequest, error) {
	p := execution.work.GetProposal()
	ref := strconv.FormatInt(value.IID, 10)
	base, err := parseProviderBaseURL(execution.configuration["base_url"])
	if err != nil || value.IID < 1 || value.Description != execution.work.GetEffectMarker() || value.SourceBranch != p.GetProposalBranch() || value.SHA != commit || value.TargetBranch != p.GetSourceRefName() || value.SourceProject < 1 || value.SourceProject != value.TargetProject || value.References.Full != p.GetRepositoryRef()+"!"+ref || value.URL != strings.TrimSuffix(base.String(), "/")+"/"+p.GetRepositoryRef()+"/-/merge_requests/"+ref {
		return ConfigurationWriteBackPullRequest{}, errWriteBackGit
	}
	return ConfigurationWriteBackPullRequest{Ref: ref, URL: value.URL}, nil
}

func (execution *ConfigurationWriteBackExecution) FindPullRequest(ctx context.Context, commit string) (ConfigurationWriteBackPullRequest, bool, error) {
	ctx, cancel := context.WithDeadline(ctx, execution.deadline)
	defer cancel()
	p := execution.work.GetProposal()
	var found ConfigurationWriteBackPullRequest
	count := 0
	for page := 1; page <= 10; page++ {
		query := url.Values{"state": {"all"}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		length := 0
		if execution.definition.Spec.Adapter == "GITHUB" {
			query.Set("head", execution.configuration["owner"]+":"+p.GetProposalBranch())
			query.Set("base", p.GetSourceRefName())
			var values []githubWriteBackPR
			if err := execution.providerJSON(ctx, http.MethodGet, execution.githubRoot()+"/pulls", query, nil, &values); err != nil {
				return found, false, err
			}
			length = len(values)
			if length > 100 {
				return found, false, errWriteBackGit
			}
			for _, value := range values {
				if value.Body != execution.work.GetEffectMarker() {
					continue
				}
				result, err := execution.githubPR(value, commit)
				if err != nil {
					return found, false, err
				}
				found = result
				count++
			}
		} else {
			query.Set("source_branch", p.GetProposalBranch())
			query.Set("target_branch", p.GetSourceRefName())
			query.Set("scope", "all")
			var values []gitlabWriteBackPR
			if err := execution.providerJSON(ctx, http.MethodGet, execution.gitlabRoot()+"/merge_requests", query, nil, &values); err != nil {
				return found, false, err
			}
			length = len(values)
			if length > 100 {
				return found, false, errWriteBackGit
			}
			for _, value := range values {
				if value.Description != execution.work.GetEffectMarker() {
					continue
				}
				result, err := execution.gitlabPR(value, commit)
				if err != nil {
					return found, false, err
				}
				found = result
				count++
			}
		}
		if count > 1 {
			return found, false, errWriteBackGit
		}
		if length < 100 {
			return found, count == 1, nil
		}
	}
	return found, false, errWriteBackGit
}

func (execution *ConfigurationWriteBackExecution) CreatePullRequest(ctx context.Context, commit string) (ConfigurationWriteBackPullRequest, error) {
	ctx, cancel := context.WithDeadline(ctx, execution.deadline)
	defer cancel()
	if execution.work.GetMode() != cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_EXECUTE {
		return ConfigurationWriteBackPullRequest{}, errWriteBackGit
	}
	p := execution.work.GetProposal()
	if execution.definition.Spec.Adapter == "GITHUB" {
		input := map[string]any{"head": p.GetProposalBranch(), "base": p.GetSourceRefName(), "title": execution.work.GetCommitMessage(), "body": execution.work.GetEffectMarker()}
		if err := execution.validateInput("github.pull_request.create", input); err != nil {
			return ConfigurationWriteBackPullRequest{}, err
		}
		var result githubWriteBackPR
		if err := execution.providerJSON(ctx, http.MethodPost, execution.githubRoot()+"/pulls", nil, input, &result); err != nil {
			return ConfigurationWriteBackPullRequest{}, err
		}
		return execution.githubPR(result, commit)
	}
	input := map[string]any{"source_branch": p.GetProposalBranch(), "target_branch": p.GetSourceRefName(), "title": execution.work.GetCommitMessage(), "description": execution.work.GetEffectMarker()}
	if err := execution.validateInput("gitlab.merge_request.create", input); err != nil {
		return ConfigurationWriteBackPullRequest{}, err
	}
	var result gitlabWriteBackPR
	if err := execution.providerJSON(ctx, http.MethodPost, execution.gitlabRoot()+"/merge_requests", nil, input, &result); err != nil {
		return ConfigurationWriteBackPullRequest{}, err
	}
	return execution.gitlabPR(result, commit)
}

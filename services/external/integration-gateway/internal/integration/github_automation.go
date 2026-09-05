package integration

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

type githubWorkflowView struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}

type githubRunView struct {
	ID         int64  `json:"id"`
	WorkflowID int64  `json:"workflow_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadSHA    string `json:"head_sha"`
	HeadBranch string `json:"head_branch"`
	URL        string `json:"url"`
}

func projectGitHubRun(item *github.WorkflowRun) githubRunView {
	return githubRunView{item.GetID(), item.GetWorkflowID(), item.GetName(), item.GetStatus(), item.GetConclusion(), item.GetHeadSHA(), item.GetHeadBranch(), item.GetHTMLURL()}
}

type githubJobView struct {
	ID         int64  `json:"id"`
	RunID      int64  `json:"run_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadSHA    string `json:"head_sha"`
}

func projectGitHubJob(item *github.WorkflowJob) githubJobView {
	return githubJobView{item.GetID(), item.GetRunID(), item.GetName(), item.GetStatus(), item.GetConclusion(), item.GetHeadSHA()}
}

type githubCheckView struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadSHA    string `json:"head_sha"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
}

func projectGitHubCheck(item *github.CheckRun) githubCheckView {
	return githubCheckView{item.GetID(), item.GetName(), item.GetStatus(), item.GetConclusion(), item.GetHeadSHA(), item.GetOutput().GetTitle(), item.GetOutput().GetSummary()}
}

func (adapter *Adapter) executeGitHubAutomation(ctx context.Context, client *github.Client, owner, repo string, request Request, capability integrationpackage.Capability, in githubCatalogInput, options github.ListOptions) (Result, error) {
	switch request.Operation {
	case "github.check_run.list":
		return githubCatalogPage(ctx, capability, request, in.Limit, in.Cursor, func() ([]githubCheckView, *github.Response, error) {
			result, response, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, in.Ref, &github.ListCheckRunsOptions{ListOptions: options})
			views := []githubCheckView{}
			if result != nil {
				for _, item := range result.CheckRuns {
					views = append(views, projectGitHubCheck(item))
				}
			}
			return views, response, err
		})
	case "github.check_run.read":
		item, err := githubRead(ctx, capability, func() (*github.CheckRun, *github.Response, error) {
			return client.Checks.GetCheckRun(ctx, owner, repo, in.CheckID)
		})
		if err != nil {
			return Result{}, err
		}
		if item == nil || item.GetID() != in.CheckID {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "github-check:"+strconv.FormatInt(in.CheckID, 10), projectGitHubCheck(item))
	case "github.actions.workflow.list":
		return githubCatalogPage(ctx, capability, request, in.Limit, in.Cursor, func() ([]githubWorkflowView, *github.Response, error) {
			result, response, err := client.Actions.ListWorkflows(ctx, owner, repo, &options)
			views := []githubWorkflowView{}
			if result != nil {
				for _, item := range result.Workflows {
					views = append(views, githubWorkflowView{item.GetID(), item.GetName(), item.GetPath(), item.GetState()})
				}
			}
			return views, response, err
		})
	case "github.actions.workflow.read":
		item, err := githubRead(ctx, capability, func() (*github.Workflow, *github.Response, error) {
			return client.Actions.GetWorkflowByID(ctx, owner, repo, in.WorkflowID)
		})
		if err != nil {
			return Result{}, err
		}
		if item == nil || item.GetID() != in.WorkflowID {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "github-workflow:"+strconv.FormatInt(in.WorkflowID, 10), githubWorkflowView{item.GetID(), item.GetName(), item.GetPath(), item.GetState()})
	case "github.actions.workflow.dispatch":
		inputs, err := githubWorkflowInputs(in.WorkflowInputs)
		if err != nil {
			return Result{}, err
		}
		response, err := client.Actions.CreateWorkflowDispatchEventByID(ctx, owner, repo, in.WorkflowID, github.CreateWorkflowDispatchEventRequest{Ref: in.Ref, Inputs: inputs})
		return githubCommandResult(request, response, err)
	case "github.actions.run.list":
		return githubCatalogPage(ctx, capability, request, in.Limit, in.Cursor, func() ([]githubRunView, *github.Response, error) {
			result, response, err := client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, &github.ListWorkflowRunsOptions{Branch: in.Branch, Status: in.State, ListOptions: options})
			views := []githubRunView{}
			if result != nil {
				for _, item := range result.WorkflowRuns {
					views = append(views, projectGitHubRun(item))
				}
			}
			return views, response, err
		})
	case "github.actions.run.read":
		item, err := githubRead(ctx, capability, func() (*github.WorkflowRun, *github.Response, error) {
			return client.Actions.GetWorkflowRunByID(ctx, owner, repo, in.RunID)
		})
		if err != nil {
			return Result{}, err
		}
		if item == nil || item.GetID() != in.RunID {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "github-run:"+strconv.FormatInt(in.RunID, 10), projectGitHubRun(item))
	case "github.actions.run.rerun":
		response, err := client.Actions.RerunWorkflowByID(ctx, owner, repo, in.RunID)
		return githubCommandResult(request, response, err)
	case "github.actions.run.cancel":
		response, err := client.Actions.CancelWorkflowRunByID(ctx, owner, repo, in.RunID)
		return githubCommandResult(request, response, err)
	case "github.actions.job.list":
		return githubCatalogPage(ctx, capability, request, in.Limit, in.Cursor, func() ([]githubJobView, *github.Response, error) {
			result, response, err := client.Actions.ListWorkflowJobs(ctx, owner, repo, in.RunID, &github.ListWorkflowJobsOptions{ListOptions: options})
			views := []githubJobView{}
			if result != nil {
				for _, item := range result.Jobs {
					if item.GetRunID() != in.RunID {
						return nil, response, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
					}
					views = append(views, projectGitHubJob(item))
				}
			}
			return views, response, err
		})
	case "github.actions.job.read":
		item, err := githubRead(ctx, capability, func() (*github.WorkflowJob, *github.Response, error) {
			return client.Actions.GetWorkflowJobByID(ctx, owner, repo, in.JobID)
		})
		if err != nil {
			return Result{}, err
		}
		if item == nil || item.GetID() != in.JobID || item.GetRunID() != in.RunID {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "github-job:"+strconv.FormatInt(in.JobID, 10), projectGitHubJob(item))
	default:
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

func githubWorkflowInputs(raw string) (map[string]any, error) {
	if raw == "" {
		return nil, nil
	}
	rejected := &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, rejected
	}
	inputs := map[string]any{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || key == "" || len(key) > 100 || len(inputs) >= 25 {
			return nil, rejected
		}
		if _, exists := inputs[key]; exists {
			return nil, rejected
		}
		var value any
		if decoder.Decode(&value) != nil {
			return nil, rejected
		}
		switch typed := value.(type) {
		case string:
			if len(typed) > 4096 {
				return nil, rejected
			}
		case bool, json.Number:
		default:
			return nil, rejected
		}
		inputs[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, rejected
	}
	return inputs, nil
}

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	githubapi "github.com/google/go-github/v82/github"
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
)

const (
	githubReviewEventComment          = "COMMENT"
	githubReviewEventApprove          = "APPROVE"
	githubReviewEventRequestChanges   = "REQUEST_CHANGES"
	githubWatermarkStart              = "<!-- kodex:artifact v1"
	githubWatermarkEnd                = "-->"
	githubProviderRelationshipResult  = "github:relationship:updated"
	githubProviderRelationshipVersion = "local-provider-relationship"
)

// Execute performs one provider write command against GitHub.
func (a *Adapter) Execute(ctx context.Context, request providerclient.WriteRequest) (providerclient.WriteResult, error) {
	if request.ProviderSlug != enum.ProviderSlugGitHub {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
	if countWriteCommands(request) != 1 {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
	if request.UpdateRelationship != nil {
		return executeRelationshipWrite(request)
	}
	if request.Credential.ExternalAccountID == uuid.Nil || request.Credential.Token.Len() == 0 {
		return providerclient.WriteResult{}, errs.ErrInvalidArgument
	}
	client, err := a.githubClient(request.Credential.Token)
	if err != nil {
		return providerclient.WriteResult{}, err
	}
	switch {
	case request.CreateIssue != nil:
		return a.executeCreateIssue(ctx, client, request.CreateIssue)
	case request.UpdateIssue != nil:
		return a.executeUpdateIssue(ctx, client, request.UpdateIssue)
	case request.CreateComment != nil:
		return a.executeCreateComment(ctx, client, request.CreateComment)
	case request.UpdateComment != nil:
		return a.executeUpdateComment(ctx, client, request.UpdateComment)
	case request.CreatePullRequest != nil:
		return a.executeCreatePullRequest(ctx, client, request.CreatePullRequest)
	case request.UpdatePullRequest != nil:
		return a.executeUpdatePullRequest(ctx, client, request.UpdatePullRequest)
	case request.CreateReviewSignal != nil:
		return a.executeCreateReviewSignal(ctx, client, request.CreateReviewSignal)
	default:
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
}

func (a *Adapter) executeCreateIssue(ctx context.Context, client *githubapi.Client, command *providerclient.CreateIssueCommand) (providerclient.WriteResult, error) {
	owner, repo, err := a.repositoryRefFromTarget(ctx, client, command.RepositoryTarget)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	body, err := bodyWithWatermark(command.Body, command.WatermarkJSON)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindPermanent, 0, err)
	}
	issueRequest, err := issueRequestForCreate(command, body)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindPermanent, 0, err)
	}
	issue, response, err := client.Issues.Create(ctx, owner, repo, issueRequest)
	if err != nil {
		return providerclient.WriteResult{}, classifyGitHubError(err)
	}
	snapshot := issueAPISnapshot(owner+"/"+repo, issue, enum.WorkItemKindIssue)
	return providerclient.WriteResult{
		ResultRef:        issue.GetHTMLURL(),
		ProviderObjectID: snapshot.ProviderWorkItemID,
		ProviderVersion:  providerVersionFromResponse(response, issueUpdatedAt(issue, time.Now().UTC())),
		WorkItem:         &snapshot,
	}, nil
}

func (a *Adapter) executeUpdateIssue(ctx context.Context, client *githubapi.Client, command *providerclient.UpdateIssueCommand) (providerclient.WriteResult, error) {
	target, err := a.workItemScopeFromTarget(ctx, client, command.Target)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	bodyInput, err := a.bodyInputWithProviderBody(ctx, client, target, command.Body, command.WatermarkJSON, providerBodyIssue)
	if err != nil {
		return providerclient.WriteResult{}, err
	}
	body, err := optionalBodyWithWatermark(bodyInput, command.WatermarkJSON)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindPermanent, 0, err)
	}
	issueRequest, err := issueRequestForUpdate(command, body)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindPermanent, 0, err)
	}
	issue, response, err := a.editIssue(ctx, client, target, issueRequest, command.ExpectedProviderVersion)
	if err != nil {
		return providerclient.WriteResult{}, classifyGitHubError(err)
	}
	snapshot := issueAPISnapshot(target.repository(), issue, enum.WorkItemKindIssue)
	if issue.IsPullRequest() {
		snapshot.Kind = string(enum.WorkItemKindPullRequest)
		snapshot.ProviderWorkItemID = providerWorkItemID(target.repository(), enum.WorkItemKindPullRequest, issue.GetNumber())
	}
	return providerclient.WriteResult{
		ResultRef:        issue.GetHTMLURL(),
		ProviderObjectID: snapshot.ProviderWorkItemID,
		ProviderVersion:  providerVersionFromResponse(response, issueUpdatedAt(issue, time.Now().UTC())),
		WorkItem:         &snapshot,
	}, nil
}

func (a *Adapter) executeCreateComment(ctx context.Context, client *githubapi.Client, command *providerclient.CreateCommentCommand) (providerclient.WriteResult, error) {
	target, err := a.workItemScopeFromTarget(ctx, client, command.Target)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	workItem, err := a.fetchWorkItemSnapshot(ctx, client, target)
	if err != nil {
		return providerclient.WriteResult{}, err
	}
	comment, response, err := client.Issues.CreateComment(ctx, target.owner, target.repo, target.number, &githubapi.IssueComment{
		Body: githubapi.Ptr(strings.TrimSpace(command.Body)),
	})
	if err != nil {
		return providerclient.WriteResult{}, classifyGitHubError(err)
	}
	snapshot := issueCommentSnapshot(workItem.ProviderWorkItemID, comment)
	return providerclient.WriteResult{
		ResultRef:        comment.GetHTMLURL(),
		ProviderObjectID: snapshot.ProviderCommentID,
		ProviderVersion:  providerVersionFromResponse(response, commentUpdatedAt(snapshot, time.Now().UTC())),
		WorkItem:         &workItem,
		Comment:          &snapshot,
	}, nil
}

func (a *Adapter) executeUpdateComment(ctx context.Context, client *githubapi.Client, command *providerclient.UpdateCommentCommand) (providerclient.WriteResult, error) {
	target, err := a.workItemScopeFromTarget(ctx, client, command.Target)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	commentID, err := parsePositiveInt64(command.ProviderCommentID)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	workItem, err := a.fetchWorkItemSnapshot(ctx, client, target)
	if err != nil {
		return providerclient.WriteResult{}, err
	}
	comment, response, err := a.editComment(ctx, client, target, commentID, strings.TrimSpace(command.Body), command.ExpectedProviderVersion)
	if err != nil {
		return providerclient.WriteResult{}, classifyGitHubError(err)
	}
	snapshot := issueCommentSnapshot(workItem.ProviderWorkItemID, comment)
	return providerclient.WriteResult{
		ResultRef:        comment.GetHTMLURL(),
		ProviderObjectID: snapshot.ProviderCommentID,
		ProviderVersion:  providerVersionFromResponse(response, commentUpdatedAt(snapshot, time.Now().UTC())),
		WorkItem:         &workItem,
		Comment:          &snapshot,
	}, nil
}

func (a *Adapter) executeCreatePullRequest(ctx context.Context, client *githubapi.Client, command *providerclient.CreatePullRequestCommand) (providerclient.WriteResult, error) {
	if strings.TrimSpace(command.LinkedIssueRef) != "" || len(trimWriteStrings(command.Labels)) > 0 {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
	owner, repo, err := a.repositoryRefFromTarget(ctx, client, command.RepositoryTarget)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	body, err := bodyWithWatermark(command.Body, command.WatermarkJSON)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindPermanent, 0, err)
	}
	pullRequest, response, err := client.PullRequests.Create(ctx, owner, repo, &githubapi.NewPullRequest{
		Title: githubapi.Ptr(strings.TrimSpace(command.Title)),
		Body:  githubapi.Ptr(body),
		Head:  githubapi.Ptr(strings.TrimSpace(command.HeadBranch)),
		Base:  githubapi.Ptr(strings.TrimSpace(command.BaseBranch)),
		Draft: githubapi.Ptr(command.Draft),
	})
	if err != nil {
		return providerclient.WriteResult{}, classifyGitHubError(err)
	}
	snapshot := pullRequestAPISnapshot(owner+"/"+repo, pullRequest)
	return providerclient.WriteResult{
		ResultRef:        pullRequest.GetHTMLURL(),
		ProviderObjectID: snapshot.ProviderWorkItemID,
		ProviderVersion:  providerVersionFromResponse(response, pullRequest.GetUpdatedAt().Time),
		WorkItem:         &snapshot,
	}, nil
}

func (a *Adapter) executeUpdatePullRequest(ctx context.Context, client *githubapi.Client, command *providerclient.UpdatePullRequestCommand) (providerclient.WriteResult, error) {
	target, err := a.workItemScopeFromTarget(ctx, client, command.Target)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	if target.kind != "" && target.kind != enum.WorkItemKindPullRequest {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
	// GitHub exposes labels, assignees and milestone on PRs, but mutates them
	// through the issue endpoint because every PR is backed by an issue.
	hasIssueBackedFields := command.Labels != nil || command.AssigneeProviderLogins != nil || command.Milestone != nil
	hasPullRequestOnlyFields := command.BaseBranch != nil || command.MaintainerCanModify != nil
	if hasIssueBackedFields && hasPullRequestOnlyFields {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
	if hasIssueBackedFields {
		return a.executeUpdateIssue(ctx, client, &providerclient.UpdateIssueCommand{
			Target:                  command.Target,
			Title:                   command.Title,
			Body:                    command.Body,
			Labels:                  command.Labels,
			AssigneeProviderLogins:  command.AssigneeProviderLogins,
			Milestone:               command.Milestone,
			State:                   command.State,
			WatermarkJSON:           command.WatermarkJSON,
			ExpectedProviderVersion: command.ExpectedProviderVersion,
		})
	}
	bodyInput, err := a.bodyInputWithProviderBody(ctx, client, target, command.Body, command.WatermarkJSON, providerBodyPullRequest)
	if err != nil {
		return providerclient.WriteResult{}, err
	}
	body, err := optionalBodyWithWatermark(bodyInput, command.WatermarkJSON)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindPermanent, 0, err)
	}
	request, err := pullRequestForUpdate(command, body)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindPermanent, 0, err)
	}
	pullRequest, response, err := a.editPullRequest(ctx, client, target, request, command.ExpectedProviderVersion)
	if err != nil {
		return providerclient.WriteResult{}, classifyGitHubError(err)
	}
	snapshot := pullRequestAPISnapshot(target.repository(), pullRequest)
	return providerclient.WriteResult{
		ResultRef:        pullRequest.GetHTMLURL(),
		ProviderObjectID: snapshot.ProviderWorkItemID,
		ProviderVersion:  providerVersionFromResponse(response, pullRequest.GetUpdatedAt().Time),
		WorkItem:         &snapshot,
	}, nil
}

func (a *Adapter) executeCreateReviewSignal(ctx context.Context, client *githubapi.Client, command *providerclient.CreateReviewSignalCommand) (providerclient.WriteResult, error) {
	target, err := a.workItemScopeFromTarget(ctx, client, command.Target)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	if target.kind != enum.WorkItemKindPullRequest {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
	workItem, err := a.fetchWorkItemSnapshot(ctx, client, target)
	if err != nil {
		return providerclient.WriteResult{}, err
	}
	body := strings.TrimSpace(command.Body)
	comments, err := draftReviewComments(command.InlineComments)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	event, err := reviewSignalEvent(command.Kind)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	if command.Kind != providerclient.ReviewSignalKindApproval && body == "" && len(comments) == 0 {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindPermanent, 0, nil)
	}
	reviewRequest := &githubapi.PullRequestReviewRequest{
		Event:    githubapi.Ptr(event),
		Comments: comments,
	}
	if body != "" {
		reviewRequest.Body = githubapi.Ptr(body)
	}
	review, response, err := client.PullRequests.CreateReview(ctx, target.owner, target.repo, target.number, reviewRequest)
	if err != nil {
		return providerclient.WriteResult{}, classifyGitHubError(err)
	}
	snapshot := reviewSnapshot(workItem.ProviderWorkItemID, review)
	return providerclient.WriteResult{
		ResultRef:        review.GetHTMLURL(),
		ProviderObjectID: snapshot.ProviderCommentID,
		ProviderVersion:  providerVersionFromResponse(response, commentUpdatedAt(snapshot, time.Now().UTC())),
		WorkItem:         &workItem,
		Comment:          &snapshot,
	}, nil
}

func executeRelationshipWrite(request providerclient.WriteRequest) (providerclient.WriteResult, error) {
	command := request.UpdateRelationship
	if command == nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
	return providerclient.WriteResult{
		ResultRef:        githubProviderRelationshipResult,
		ProviderObjectID: request.TargetRef,
		ProviderVersion:  githubProviderRelationshipVersion,
		Relationship: &providerclient.RelationshipResult{
			Source:            command.Source,
			Target:            command.Target,
			TargetProviderRef: command.TargetProviderRef,
			RelationshipType:  command.RelationshipType,
			SourceKind:        command.SourceKind,
			Confidence:        command.Confidence,
		},
	}, nil
}

func (a *Adapter) repositoryRefFromTarget(ctx context.Context, client *githubapi.Client, target providerclient.Target) (string, string, error) {
	if target.ProviderSlug != enum.ProviderSlugGitHub {
		return "", "", providerclient.ErrUnsupported
	}
	if repositoryFullName := strings.TrimSpace(target.RepositoryFullName); repositoryFullName != "" {
		return parseRepositoryRef(repositoryFullName)
	}
	if providerRepositoryID := strings.TrimSpace(target.ProviderRepositoryID); providerRepositoryID != "" {
		return a.resolveRepositoryRef(ctx, client, "provider_repository_id:"+providerRepositoryID)
	}
	if webURL := strings.TrimSpace(target.WebURL); webURL != "" {
		return parseGitHubRepositoryURL(webURL)
	}
	return "", "", providerclient.ErrUnsupported
}

func (a *Adapter) workItemScopeFromTarget(ctx context.Context, client *githubapi.Client, target providerclient.Target) (workItemScope, error) {
	if target.ProviderSlug != enum.ProviderSlugGitHub {
		return workItemScope{}, providerclient.ErrUnsupported
	}
	if providerObjectID := strings.TrimSpace(target.ProviderObjectID); providerObjectID != "" {
		return parseGitHubProviderWorkItemID(providerObjectID)
	}
	if webURL := strings.TrimSpace(target.WebURL); webURL != "" {
		return parseGitHubWorkItemURL(webURL)
	}
	number := int(target.Number)
	if number <= 0 || int64(number) != target.Number {
		return workItemScope{}, providerclient.ErrUnsupported
	}
	owner, repo, err := a.repositoryRefFromTarget(ctx, client, target)
	if err != nil {
		return workItemScope{}, err
	}
	switch target.WorkItemKind {
	case enum.WorkItemKindIssue, enum.WorkItemKindPullRequest, "":
		return workItemScope{owner: owner, repo: repo, kind: target.WorkItemKind, number: number}, nil
	default:
		return workItemScope{}, providerclient.ErrUnsupported
	}
}

func issueRequestForCreate(command *providerclient.CreateIssueCommand, body string) (*githubapi.IssueRequest, error) {
	if strings.TrimSpace(command.Title) == "" {
		return nil, providerclient.ErrUnsupported
	}
	request := &githubapi.IssueRequest{
		Title: githubapi.Ptr(strings.TrimSpace(command.Title)),
		Body:  githubapi.Ptr(body),
	}
	if labels := trimWriteStrings(command.Labels); len(labels) > 0 {
		request.Labels = &labels
	}
	if assignees := trimWriteStrings(command.AssigneeProviderLogins); len(assignees) > 0 {
		request.Assignees = &assignees
	}
	if milestone := strings.TrimSpace(command.Milestone); milestone != "" {
		milestoneNumber, err := parsePositiveInt(milestone)
		if err != nil {
			return nil, err
		}
		request.Milestone = githubapi.Ptr(milestoneNumber)
	}
	if workItemType := strings.TrimSpace(command.WorkItemType); workItemType != "" {
		request.Type = githubapi.Ptr(workItemType)
	}
	return request, nil
}

func issueRequestForUpdate(command *providerclient.UpdateIssueCommand, body *string) (*githubapi.IssueRequest, error) {
	request := &githubapi.IssueRequest{}
	if command.Title != nil {
		request.Title = githubapi.Ptr(strings.TrimSpace(*command.Title))
	}
	if body != nil {
		request.Body = githubapi.Ptr(*body)
	}
	if command.Labels != nil {
		labels := trimWriteStrings(command.Labels.Values)
		request.Labels = &labels
	}
	if command.AssigneeProviderLogins != nil {
		assignees := trimWriteStrings(command.AssigneeProviderLogins.Values)
		request.Assignees = &assignees
	}
	if command.Milestone != nil {
		milestone := strings.TrimSpace(*command.Milestone)
		if milestone == "" {
			return nil, providerclient.ErrUnsupported
		} else {
			milestoneNumber, err := parsePositiveInt(milestone)
			if err != nil {
				return nil, err
			}
			request.Milestone = githubapi.Ptr(milestoneNumber)
		}
	}
	if command.State != nil {
		request.State = githubapi.Ptr(strings.TrimSpace(*command.State))
	}
	if command.WorkItemType != nil {
		request.Type = githubapi.Ptr(strings.TrimSpace(*command.WorkItemType))
	}
	return request, nil
}

func pullRequestForUpdate(command *providerclient.UpdatePullRequestCommand, body *string) (*githubapi.PullRequest, error) {
	request := &githubapi.PullRequest{}
	if command.Title != nil {
		request.Title = githubapi.Ptr(strings.TrimSpace(*command.Title))
	}
	if body != nil {
		request.Body = githubapi.Ptr(*body)
	}
	if command.State != nil {
		request.State = githubapi.Ptr(strings.TrimSpace(*command.State))
	}
	if command.BaseBranch != nil {
		baseBranch := strings.TrimSpace(*command.BaseBranch)
		if baseBranch == "" {
			return nil, providerclient.ErrUnsupported
		}
		if command.State != nil && strings.EqualFold(strings.TrimSpace(*command.State), "closed") {
			return nil, providerclient.ErrUnsupported
		}
		request.Base = &githubapi.PullRequestBranch{Ref: githubapi.Ptr(baseBranch)}
	}
	if command.MaintainerCanModify != nil {
		request.MaintainerCanModify = command.MaintainerCanModify
	}
	return request, nil
}

func (a *Adapter) editIssue(ctx context.Context, client *githubapi.Client, target workItemScope, request *githubapi.IssueRequest, expectedVersion string) (*githubapi.Issue, *githubapi.Response, error) {
	if strings.TrimSpace(expectedVersion) == "" {
		return client.Issues.Edit(ctx, target.owner, target.repo, target.number, request)
	}
	path := fmt.Sprintf("repos/%s/%s/issues/%d", target.owner, target.repo, target.number)
	httpRequest, err := client.NewRequest("PATCH", path, request)
	if err != nil {
		return nil, nil, err
	}
	httpRequest.Header.Set("If-Match", strings.TrimSpace(expectedVersion))
	issue := new(githubapi.Issue)
	response, err := client.Do(ctx, httpRequest, issue)
	if err != nil {
		return nil, response, err
	}
	return issue, response, nil
}

func (a *Adapter) editPullRequest(ctx context.Context, client *githubapi.Client, target workItemScope, request *githubapi.PullRequest, expectedVersion string) (*githubapi.PullRequest, *githubapi.Response, error) {
	if strings.TrimSpace(expectedVersion) == "" {
		return client.PullRequests.Edit(ctx, target.owner, target.repo, target.number, request)
	}
	path := fmt.Sprintf("repos/%s/%s/pulls/%d", target.owner, target.repo, target.number)
	httpRequest, err := client.NewRequest("PATCH", path, pullRequestUpdatePayload(request))
	if err != nil {
		return nil, nil, err
	}
	httpRequest.Header.Set("If-Match", strings.TrimSpace(expectedVersion))
	pullRequest := new(githubapi.PullRequest)
	response, err := client.Do(ctx, httpRequest, pullRequest)
	if err != nil {
		return nil, response, err
	}
	return pullRequest, response, nil
}

type pullRequestUpdatePayloadBody struct {
	Title               *string `json:"title,omitempty"`
	Body                *string `json:"body,omitempty"`
	State               *string `json:"state,omitempty"`
	Base                *string `json:"base,omitempty"`
	MaintainerCanModify *bool   `json:"maintainer_can_modify,omitempty"`
}

func pullRequestUpdatePayload(request *githubapi.PullRequest) pullRequestUpdatePayloadBody {
	payload := pullRequestUpdatePayloadBody{
		Title:               request.Title,
		Body:                request.Body,
		State:               request.State,
		MaintainerCanModify: request.MaintainerCanModify,
	}
	if request.Base != nil && request.GetState() != "closed" {
		payload.Base = request.Base.Ref
	}
	return payload
}

func (a *Adapter) editComment(ctx context.Context, client *githubapi.Client, target workItemScope, commentID int64, body string, expectedVersion string) (*githubapi.IssueComment, *githubapi.Response, error) {
	comment := &githubapi.IssueComment{Body: githubapi.Ptr(body)}
	if strings.TrimSpace(expectedVersion) == "" {
		return client.Issues.EditComment(ctx, target.owner, target.repo, commentID, comment)
	}
	path := fmt.Sprintf("repos/%s/%s/issues/comments/%d", target.owner, target.repo, commentID)
	httpRequest, err := client.NewRequest("PATCH", path, comment)
	if err != nil {
		return nil, nil, err
	}
	httpRequest.Header.Set("If-Match", strings.TrimSpace(expectedVersion))
	updated := new(githubapi.IssueComment)
	response, err := client.Do(ctx, httpRequest, updated)
	if err != nil {
		return nil, response, err
	}
	return updated, response, nil
}

func draftReviewComments(comments []providerclient.ReviewInlineComment) ([]*githubapi.DraftReviewComment, error) {
	if len(comments) == 0 {
		return nil, nil
	}
	result := make([]*githubapi.DraftReviewComment, 0, len(comments))
	for _, comment := range comments {
		if strings.TrimSpace(comment.InReplyToProviderCommentID) != "" {
			return nil, providerclient.ErrUnsupported
		}
		line, err := optionalPositiveInt(comment.Line)
		if err != nil || line == nil {
			return nil, providerclient.ErrUnsupported
		}
		startLine, err := optionalPositiveInt(comment.StartLine)
		if err != nil {
			return nil, providerclient.ErrUnsupported
		}
		result = append(result, &githubapi.DraftReviewComment{
			Path:      githubapi.Ptr(strings.TrimSpace(comment.Path)),
			Body:      githubapi.Ptr(strings.TrimSpace(comment.Body)),
			Line:      line,
			StartLine: startLine,
			Side:      optionalGithubString(comment.Side),
			StartSide: optionalGithubString(comment.StartSide),
		})
	}
	return result, nil
}

func reviewSignalEvent(kind providerclient.ReviewSignalKind) (string, error) {
	switch kind {
	case providerclient.ReviewSignalKindComment:
		return githubReviewEventComment, nil
	case providerclient.ReviewSignalKindApproval:
		return githubReviewEventApprove, nil
	case providerclient.ReviewSignalKindChangesRequested:
		return githubReviewEventRequestChanges, nil
	default:
		return "", providerclient.ErrUnsupported
	}
}

func bodyWithWatermark(body string, watermarkJSON []byte) (string, error) {
	body = strings.TrimSpace(body)
	if len(watermarkJSON) == 0 {
		return body, nil
	}
	watermark, err := watermarkBlock(watermarkJSON)
	if err != nil {
		return "", err
	}
	if strings.Contains(body, githubWatermarkStart) {
		return replaceWatermarkBlock(body, watermark)
	}
	if body == "" {
		return watermark, nil
	}
	return body + "\n\n" + watermark, nil
}

func replaceWatermarkBlock(body string, watermark string) (string, error) {
	start := strings.Index(body, githubWatermarkStart)
	if start < 0 {
		return body, nil
	}
	relativeEnd := strings.Index(body[start:], githubWatermarkEnd)
	if relativeEnd < 0 {
		return "", providerclient.ErrPermanent
	}
	end := start + relativeEnd + len(githubWatermarkEnd)
	prefix := strings.TrimRight(body[:start], "\n")
	suffix := strings.TrimLeft(body[end:], "\n")
	switch {
	case prefix == "" && suffix == "":
		return watermark, nil
	case prefix == "":
		return watermark + "\n\n" + suffix, nil
	case suffix == "":
		return prefix + "\n\n" + watermark, nil
	default:
		return prefix + "\n\n" + watermark + "\n\n" + suffix, nil
	}
}

type providerBodySource string

const (
	providerBodyIssue       providerBodySource = "issue"
	providerBodyPullRequest providerBodySource = "pull_request"
)

func (a *Adapter) bodyInputWithProviderBody(
	ctx context.Context,
	client *githubapi.Client,
	target workItemScope,
	bodyInput *string,
	watermarkJSON *[]byte,
	source providerBodySource,
) (*string, error) {
	if bodyInput != nil || watermarkJSON == nil {
		return bodyInput, nil
	}
	var currentBody string
	switch source {
	case providerBodyIssue:
		current, _, err := client.Issues.Get(ctx, target.owner, target.repo, target.number)
		if err != nil {
			return nil, classifyGitHubError(err)
		}
		currentBody = current.GetBody()
	case providerBodyPullRequest:
		current, _, err := client.PullRequests.Get(ctx, target.owner, target.repo, target.number)
		if err != nil {
			return nil, classifyGitHubError(err)
		}
		currentBody = current.GetBody()
	default:
		return nil, providerclient.ErrUnsupported
	}
	return &currentBody, nil
}

func optionalBodyWithWatermark(body *string, watermarkJSON *[]byte) (*string, error) {
	if body == nil && watermarkJSON == nil {
		return nil, nil
	}
	text := ""
	if body != nil {
		text = *body
	}
	var raw []byte
	if watermarkJSON != nil {
		raw = *watermarkJSON
	}
	result, err := bodyWithWatermark(text, raw)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func watermarkBlock(raw []byte) (string, error) {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	lines := make([]string, 0, len(fields)+2)
	lines = append(lines, githubWatermarkStart)
	for _, key := range sortedKeys(fields) {
		value := strings.TrimSpace(fmt.Sprint(fields[key]))
		if strings.TrimSpace(key) == "" || value == "" {
			continue
		}
		lines = append(lines, strings.TrimSpace(key)+": "+value)
	}
	lines = append(lines, githubWatermarkEnd)
	return strings.Join(lines, "\n"), nil
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func countWriteCommands(request providerclient.WriteRequest) int {
	count := 0
	if request.CreateIssue != nil {
		count++
	}
	if request.UpdateIssue != nil {
		count++
	}
	if request.CreateComment != nil {
		count++
	}
	if request.UpdateComment != nil {
		count++
	}
	if request.CreatePullRequest != nil {
		count++
	}
	if request.UpdatePullRequest != nil {
		count++
	}
	if request.CreateReviewSignal != nil {
		count++
	}
	if request.UpdateRelationship != nil {
		count++
	}
	return count
}

func providerVersionFromResponse(response *githubapi.Response, fallback time.Time) string {
	if response != nil {
		if etag := strings.TrimSpace(response.Header.Get("ETag")); etag != "" {
			return etag
		}
	}
	if fallback.IsZero() {
		return ""
	}
	return fallback.UTC().Format(time.RFC3339Nano)
}

func trimWriteStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func optionalGithubString(value string) *string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return githubapi.Ptr(trimmed)
	}
	return nil
}

func optionalPositiveInt(value *int64) (*int, error) {
	if value == nil {
		return nil, nil
	}
	if *value <= 0 || *value > math.MaxInt {
		return nil, providerclient.ErrUnsupported
	}
	result := int(*value)
	return &result, nil
}

func parsePositiveInt(value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0, providerclient.ErrUnsupported
	}
	return parsed, nil
}

func parsePositiveInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, providerclient.ErrUnsupported
	}
	return parsed, nil
}

package providerhub

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFollowUpDispatcherMapsCreateIssueRequestAndResponse(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("91919191-1111-2222-3333-444444444444")
	projectID := uuid.MustParse("91919191-2222-3333-4444-555555555555")
	repositoryID := uuid.MustParse("91919191-3333-4444-5555-666666666666")
	accountID := uuid.MustParse("91919191-4444-5555-6666-777777777777")
	client := &fakeProviderHubClient{
		response: &providersv1.ProviderOperationResponse{
			ProviderOperation: &providersv1.ProviderOperation{
				ProviderOperationId: "op-1",
				Status:              providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_SUCCEEDED,
				ResultRef:           ptr("github:issue:123"),
				ProviderVersion:     ptr("etag:1"),
			},
			Result: &providersv1.ProviderOperationCommandResult{
				Target: &providersv1.ProviderTarget{
					ProviderSlug:       "github",
					RepositoryFullName: ptr("codex-k8s/kodex"),
					WorkItemKind:       ptrEnum(providersv1.WorkItemKind_WORK_ITEM_KIND_ISSUE),
					Number:             ptrInt64(123),
				},
			},
		},
	}
	dispatcher, err := newFollowUpDispatcher(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newFollowUpDispatcher() err = %v", err)
	}

	result, err := dispatcher.CreateIssue(context.Background(), agentservice.ProviderCreateIssueInput{
		Meta:              value.CommandMeta{CommandID: commandID, IdempotencyKey: "follow-up-dispatch", Actor: value.Actor{Type: "user", ID: "owner"}},
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		ProviderSlug:      "github",
		RepositoryTarget:  agentservice.ProviderCommandTarget{ProviderSlug: "github", RepositoryFullName: "codex-k8s/kodex"},
		Title:             "Follow-up",
		Body:              "Safe body",
		Labels:            []string{"agent"},
		WorkItemType:      "task",
		ExternalAccountID: accountID,
		OperationPolicyContext: agentservice.ProviderOperationPolicyContext{
			RiskLevel:     agentservice.ProviderRiskLevelLow,
			OperationType: agentservice.ProviderOperationTypeCreateIssue,
		},
	})
	if err != nil {
		t.Fatalf("CreateIssue() err = %v", err)
	}
	if client.createIssueRequest.GetMeta().GetExpectedVersion() != 0 || client.createIssueRequest.GetMeta().ExpectedVersion != nil {
		t.Fatalf("provider expected_version = %v", client.createIssueRequest.GetMeta().ExpectedVersion)
	}
	if client.createIssueRequest.GetProjectId() != projectID.String() || client.createIssueRequest.GetExternalAccountId() != accountID.String() ||
		client.createIssueRequest.GetTitle() != "Follow-up" || client.createIssueRequest.GetRepositoryTarget().GetRepositoryFullName() != "codex-k8s/kodex" {
		t.Fatalf("request = %+v", client.createIssueRequest)
	}
	if result.ProviderOperationRef != "provider_operation:op-1" || result.Status != agentservice.ProviderOperationStatusSucceeded ||
		result.Target.Number != 123 || result.ProviderVersion != "etag:1" {
		t.Fatalf("result = %+v", result)
	}
}

func TestFollowUpDispatcherMapsUpdateAndCommentRequests(t *testing.T) {
	t.Parallel()

	accountID := uuid.MustParse("92929292-1111-2222-3333-444444444444")
	body := "Safe status body"
	client := &fakeProviderHubClient{
		response: &providersv1.ProviderOperationResponse{
			ProviderOperation: &providersv1.ProviderOperation{
				ProviderOperationId: "op-2",
				Status:              providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_SUCCEEDED,
			},
			Result: &providersv1.ProviderOperationCommandResult{ResultRef: ptr("github:issue:123")},
		},
	}
	dispatcher, err := newFollowUpDispatcher(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newFollowUpDispatcher() err = %v", err)
	}
	_, err = dispatcher.UpdateIssue(context.Background(), agentservice.ProviderUpdateIssueInput{
		Meta:                    value.CommandMeta{CommandID: uuid.MustParse("92929292-2222-3333-4444-555555555555"), Actor: value.Actor{Type: "user", ID: "owner"}},
		Target:                  agentservice.ProviderCommandTarget{ProviderSlug: "github", RepositoryFullName: "codex-k8s/kodex", WorkItemKind: "issue", Number: 123},
		Body:                    &body,
		Labels:                  &agentservice.ProviderStringListPatch{Values: []string{"agent", "qa"}},
		ExpectedProviderVersion: "etag:1",
		ExternalAccountID:       accountID,
		OperationPolicyContext: agentservice.ProviderOperationPolicyContext{
			RiskLevel:     agentservice.ProviderRiskLevelLow,
			OperationType: agentservice.ProviderOperationTypeUpdateIssue,
		},
	})
	if err != nil {
		t.Fatalf("UpdateIssue() err = %v", err)
	}
	if client.updateIssueRequest.GetBody() != body || client.updateIssueRequest.GetLabels().GetValues()[0] != "agent" ||
		client.updateIssueRequest.GetExpectedProviderVersion() != "etag:1" {
		t.Fatalf("update issue request = %+v", client.updateIssueRequest)
	}
	_, err = dispatcher.CreateComment(context.Background(), agentservice.ProviderCreateCommentInput{
		Meta:              value.CommandMeta{CommandID: uuid.MustParse("92929292-3333-4444-5555-666666666666"), Actor: value.Actor{Type: "user", ID: "owner"}},
		Target:            agentservice.ProviderCommandTarget{ProviderSlug: "github", RepositoryFullName: "codex-k8s/kodex", WorkItemKind: "issue", Number: 123},
		Body:              "Safe comment",
		ExternalAccountID: accountID,
		OperationPolicyContext: agentservice.ProviderOperationPolicyContext{
			RiskLevel:     agentservice.ProviderRiskLevelLow,
			OperationType: agentservice.ProviderOperationTypeCreateComment,
		},
	})
	if err != nil {
		t.Fatalf("CreateComment() err = %v", err)
	}
	if client.createCommentRequest.GetBody() != "Safe comment" || client.createCommentRequest.GetTarget().GetNumber() != 123 {
		t.Fatalf("create comment request = %+v", client.createCommentRequest)
	}
}

func TestFollowUpDispatcherMapsProviderHubErrors(t *testing.T) {
	t.Parallel()

	dispatcher, err := newFollowUpDispatcher(&fakeProviderHubClient{err: status.Error(codes.InvalidArgument, "bad request")}, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newFollowUpDispatcher() err = %v", err)
	}
	_, err = dispatcher.CreateIssue(context.Background(), agentservice.ProviderCreateIssueInput{})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("CreateIssue() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

type fakeProviderHubClient struct {
	createIssueRequest   *providersv1.CreateIssueRequest
	updateIssueRequest   *providersv1.UpdateIssueRequest
	createCommentRequest *providersv1.CreateCommentRequest
	updateCommentRequest *providersv1.UpdateCommentRequest
	response             *providersv1.ProviderOperationResponse
	err                  error
}

func (f *fakeProviderHubClient) CreateIssue(_ context.Context, request *providersv1.CreateIssueRequest, _ ...grpc.CallOption) (*providersv1.ProviderOperationResponse, error) {
	f.createIssueRequest = request
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func (f *fakeProviderHubClient) UpdateIssue(_ context.Context, request *providersv1.UpdateIssueRequest, _ ...grpc.CallOption) (*providersv1.ProviderOperationResponse, error) {
	f.updateIssueRequest = request
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func (f *fakeProviderHubClient) CreateComment(_ context.Context, request *providersv1.CreateCommentRequest, _ ...grpc.CallOption) (*providersv1.ProviderOperationResponse, error) {
	f.createCommentRequest = request
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func (f *fakeProviderHubClient) UpdateComment(_ context.Context, request *providersv1.UpdateCommentRequest, _ ...grpc.CallOption) (*providersv1.ProviderOperationResponse, error) {
	f.updateCommentRequest = request
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func ptr(value string) *string {
	return &value
}

func ptrInt64(value int64) *int64 {
	return &value
}

func ptrEnum(value providersv1.WorkItemKind) *providersv1.WorkItemKind {
	return &value
}

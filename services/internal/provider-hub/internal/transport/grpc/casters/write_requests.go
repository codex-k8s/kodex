package casters

import (
	"strings"

	"github.com/google/uuid"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

// CreateIssueInput maps a typed provider issue creation request to the domain model.
func CreateIssueInput(request *providersv1.CreateIssueRequest) (providerservice.CreateIssueInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.CreateIssueInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return providerservice.CreateIssueInput{}, err
	}
	repositoryID, err := requiredUUID(request.GetRepositoryId())
	if err != nil {
		return providerservice.CreateIssueInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.CreateIssueInput{}, err
	}
	repositoryTarget, err := ProviderTargetFromProto(request.GetRepositoryTarget())
	if err != nil {
		return providerservice.CreateIssueInput{}, err
	}
	return providerservice.CreateIssueInput{
		ProjectID:              projectID,
		RepositoryID:           repositoryID,
		ProviderSlug:           providerSlug(request.GetProviderSlug()),
		RepositoryTarget:       repositoryTarget,
		Title:                  strings.TrimSpace(request.GetTitle()),
		Body:                   strings.TrimSpace(request.GetBody()),
		Labels:                 trimProtoStrings(request.GetLabels()),
		AssigneeProviderLogins: trimProtoStrings(request.GetAssigneeProviderLogins()),
		Milestone:              optionalStringPtrValue(request.Milestone),
		WorkItemType:           optionalStringPtrValue(request.WorkItemType),
		WatermarkJSON:          []byte(strings.TrimSpace(request.GetWatermarkJson())),
		Meta:                   meta,
		ExternalAccountID:      externalAccountID,
	}, nil
}

// UpdateIssueInput maps a typed provider issue update request to the domain model.
func UpdateIssueInput(request *providersv1.UpdateIssueRequest) (providerservice.UpdateIssueInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.UpdateIssueInput{}, err
	}
	target, err := ProviderTargetFromProto(request.GetTarget())
	if err != nil {
		return providerservice.UpdateIssueInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.UpdateIssueInput{}, err
	}
	labels, err := stringListPatchFromProto(request.GetLabels())
	if err != nil {
		return providerservice.UpdateIssueInput{}, err
	}
	assignees, err := stringListPatchFromProto(request.GetAssigneeProviderLogins())
	if err != nil {
		return providerservice.UpdateIssueInput{}, err
	}
	watermarkJSON := optionalJSONPointer(request.WatermarkJson)
	return providerservice.UpdateIssueInput{
		Target:                  target,
		Title:                   optionalStringPtrValue(request.Title),
		Body:                    optionalStringPtrValue(request.Body),
		Labels:                  labels,
		AssigneeProviderLogins:  assignees,
		Milestone:               optionalStringPtrValue(request.Milestone),
		State:                   optionalStringPtrValue(request.State),
		WorkItemType:            optionalStringPtrValue(request.WorkItemType),
		WatermarkJSON:           watermarkJSON,
		ExpectedProviderVersion: strings.TrimSpace(request.GetExpectedProviderVersion()),
		Meta:                    meta,
		ExternalAccountID:       externalAccountID,
	}, nil
}

// CreateCommentInput maps a provider comment creation request to the domain model.
func CreateCommentInput(request *providersv1.CreateCommentRequest) (providerservice.CreateCommentInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.CreateCommentInput{}, err
	}
	target, err := ProviderTargetFromProto(request.GetTarget())
	if err != nil {
		return providerservice.CreateCommentInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.CreateCommentInput{}, err
	}
	return providerservice.CreateCommentInput{
		Target:            target,
		Body:              strings.TrimSpace(request.GetBody()),
		Meta:              meta,
		ExternalAccountID: externalAccountID,
	}, nil
}

// UpdateCommentInput maps a provider comment update request to the domain model.
func UpdateCommentInput(request *providersv1.UpdateCommentRequest) (providerservice.UpdateCommentInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.UpdateCommentInput{}, err
	}
	target, err := ProviderTargetFromProto(request.GetTarget())
	if err != nil {
		return providerservice.UpdateCommentInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.UpdateCommentInput{}, err
	}
	return providerservice.UpdateCommentInput{
		Target:                  target,
		ProviderCommentID:       strings.TrimSpace(request.GetProviderCommentId()),
		Body:                    strings.TrimSpace(request.GetBody()),
		ExpectedProviderVersion: strings.TrimSpace(request.GetExpectedProviderVersion()),
		Meta:                    meta,
		ExternalAccountID:       externalAccountID,
	}, nil
}

// CreatePullRequestInput maps a typed PR/MR creation request to the domain model.
func CreatePullRequestInput(request *providersv1.CreatePullRequestRequest) (providerservice.CreatePullRequestInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.CreatePullRequestInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return providerservice.CreatePullRequestInput{}, err
	}
	repositoryID, err := requiredUUID(request.GetRepositoryId())
	if err != nil {
		return providerservice.CreatePullRequestInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.CreatePullRequestInput{}, err
	}
	repositoryTarget, err := ProviderTargetFromProto(request.GetRepositoryTarget())
	if err != nil {
		return providerservice.CreatePullRequestInput{}, err
	}
	return providerservice.CreatePullRequestInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		ProviderSlug:      providerSlug(request.GetProviderSlug()),
		RepositoryTarget:  repositoryTarget,
		Title:             strings.TrimSpace(request.GetTitle()),
		Body:              strings.TrimSpace(request.GetBody()),
		HeadBranch:        strings.TrimSpace(request.GetHeadBranch()),
		BaseBranch:        strings.TrimSpace(request.GetBaseBranch()),
		Draft:             request.GetDraft(),
		Labels:            trimProtoStrings(request.GetLabels()),
		LinkedIssueRef:    optionalStringPtrValue(request.LinkedIssueRef),
		WatermarkJSON:     []byte(strings.TrimSpace(request.GetWatermarkJson())),
		Meta:              meta,
		ExternalAccountID: externalAccountID,
	}, nil
}

// CreateRepositoryInput maps a provider repository creation request to the domain model.
func CreateRepositoryInput(request *providersv1.CreateRepositoryRequest) (providerservice.CreateRepositoryInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.CreateRepositoryInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return providerservice.CreateRepositoryInput{}, err
	}
	repositoryID, err := requiredUUID(request.GetRepositoryId())
	if err != nil {
		return providerservice.CreateRepositoryInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.CreateRepositoryInput{}, err
	}
	ownerKind, err := repositoryOwnerKindFromProto(request.GetOwnerKind())
	if err != nil {
		return providerservice.CreateRepositoryInput{}, err
	}
	visibility, err := repositoryVisibilityFromProto(request.GetVisibility())
	if err != nil {
		return providerservice.CreateRepositoryInput{}, err
	}
	return providerservice.CreateRepositoryInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		ProviderSlug:      providerSlug(request.GetProviderSlug()),
		OwnerKind:         ownerKind,
		ProviderOwner:     optionalStringPtrValue(request.ProviderOwner),
		RepositoryName:    strings.TrimSpace(request.GetRepositoryName()),
		Visibility:        visibility,
		Description:       optionalStringPtrValue(request.Description),
		Meta:              meta,
		ExternalAccountID: externalAccountID,
	}, nil
}

type repositoryBranchPullRequestFields struct {
	Meta              value.CommandMeta
	ProjectID         uuid.UUID
	RepositoryID      uuid.UUID
	ExternalAccountID uuid.UUID
	RepositoryTarget  providerservice.ProviderTarget
}

type repositoryBranchPullRequestProto interface {
	GetMeta() *providersv1.CommandMeta
	GetProjectId() string
	GetRepositoryId() string
	GetExternalAccountId() string
	GetRepositoryTarget() *providersv1.ProviderTarget
	GetProviderSlug() string
	GetBaseBranch() string
	GetCommitMessage() string
	GetTitle() string
	GetBody() string
	GetDraft() bool
	GetWatermarkJson() string
}

type repositoryFileProto interface {
	GetPath() string
	GetContent() string
	GetExecutable() bool
}

func repositoryBranchPullRequestFieldsFromProto(
	meta *providersv1.CommandMeta,
	projectID string,
	repositoryID string,
	externalAccountID string,
	repositoryTarget *providersv1.ProviderTarget,
) (repositoryBranchPullRequestFields, error) {
	commandMeta, err := CommandMetaFromProto(meta)
	if err != nil {
		return repositoryBranchPullRequestFields{}, err
	}
	parsedProjectID, err := requiredUUID(projectID)
	if err != nil {
		return repositoryBranchPullRequestFields{}, err
	}
	parsedRepositoryID, err := requiredUUID(repositoryID)
	if err != nil {
		return repositoryBranchPullRequestFields{}, err
	}
	parsedExternalAccountID, err := requiredUUID(externalAccountID)
	if err != nil {
		return repositoryBranchPullRequestFields{}, err
	}
	parsedRepositoryTarget, err := ProviderTargetFromProto(repositoryTarget)
	if err != nil {
		return repositoryBranchPullRequestFields{}, err
	}
	return repositoryBranchPullRequestFields{
		Meta:              commandMeta,
		ProjectID:         parsedProjectID,
		RepositoryID:      parsedRepositoryID,
		ExternalAccountID: parsedExternalAccountID,
		RepositoryTarget:  parsedRepositoryTarget,
	}, nil
}

func repositoryBranchPullRequestInputFromProto(request repositoryBranchPullRequestProto) (providerservice.RepositoryBranchPullRequestInput, error) {
	fields, err := repositoryBranchPullRequestFieldsFromProto(
		request.GetMeta(),
		request.GetProjectId(),
		request.GetRepositoryId(),
		request.GetExternalAccountId(),
		request.GetRepositoryTarget(),
	)
	if err != nil {
		return providerservice.RepositoryBranchPullRequestInput{}, err
	}
	return providerservice.RepositoryBranchPullRequestInput{
		ProjectID:         fields.ProjectID,
		RepositoryID:      fields.RepositoryID,
		ProviderSlug:      providerSlug(request.GetProviderSlug()),
		RepositoryTarget:  fields.RepositoryTarget,
		BaseBranch:        strings.TrimSpace(request.GetBaseBranch()),
		CommitMessage:     strings.TrimSpace(request.GetCommitMessage()),
		Title:             strings.TrimSpace(request.GetTitle()),
		Body:              strings.TrimSpace(request.GetBody()),
		Draft:             request.GetDraft(),
		WatermarkJSON:     []byte(strings.TrimSpace(request.GetWatermarkJson())),
		Meta:              fields.Meta,
		ExternalAccountID: fields.ExternalAccountID,
	}, nil
}

func repositoryFilesFromProto[T any](files []T, fields func(T) (string, string, bool)) []providerservice.RepositoryFile {
	result := make([]providerservice.RepositoryFile, 0, len(files))
	for _, file := range files {
		path, content, executable := fields(file)
		if strings.TrimSpace(path) == "" && content == "" && !executable {
			continue
		}
		result = append(result, providerservice.RepositoryFile{
			Path:       strings.TrimSpace(path),
			Content:    content,
			Executable: executable,
		})
	}
	return result
}

func repositoryFilesFromTypedProto[T repositoryFileProto](files []T) []providerservice.RepositoryFile {
	return repositoryFilesFromProto(files, func(file T) (string, string, bool) {
		return file.GetPath(), file.GetContent(), file.GetExecutable()
	})
}

func repositoryBranchPullRequestFromProto[T repositoryBranchPullRequestProto, O any](
	request T,
	branch func(T) string,
	files func(T) []providerservice.RepositoryFile,
	output func(providerservice.RepositoryBranchPullRequestInput, string, []providerservice.RepositoryFile) O,
) (O, error) {
	commonInput, err := repositoryBranchPullRequestInputFromProto(request)
	if err != nil {
		var zero O
		return zero, err
	}
	return output(commonInput, strings.TrimSpace(branch(request)), files(request)), nil
}

// CreateBootstrapPullRequestInput maps a bootstrap PR request to the domain model.
func CreateBootstrapPullRequestInput(request *providersv1.CreateBootstrapPullRequestRequest) (providerservice.CreateBootstrapPullRequestInput, error) {
	return repositoryBranchPullRequestFromProto(request, bootstrapBranchFromProto, bootstrapFilesFromRequest, newBootstrapPullRequestInput)
}

func bootstrapBranchFromProto(request *providersv1.CreateBootstrapPullRequestRequest) string {
	return request.GetBootstrapBranch()
}

func bootstrapFilesFromRequest(request *providersv1.CreateBootstrapPullRequestRequest) []providerservice.RepositoryFile {
	return repositoryFilesFromTypedProto(request.GetFiles())
}

func newBootstrapPullRequestInput(
	commonInput providerservice.RepositoryBranchPullRequestInput,
	bootstrapBranch string,
	files []providerservice.RepositoryFile,
) providerservice.CreateBootstrapPullRequestInput {
	return providerservice.CreateBootstrapPullRequestInput{
		RepositoryBranchPullRequestInput: commonInput,
		BootstrapBranch:                  bootstrapBranch,
		Files:                            files,
	}
}

// CreateAdoptionPullRequestInput maps an adoption PR request to the domain model.
func CreateAdoptionPullRequestInput(request *providersv1.CreateAdoptionPullRequestRequest) (providerservice.CreateAdoptionPullRequestInput, error) {
	return repositoryBranchPullRequestFromProto(request, adoptionBranchFromProto, adoptionFilesFromRequest, newAdoptionPullRequestInput)
}

func adoptionBranchFromProto(request *providersv1.CreateAdoptionPullRequestRequest) string {
	return request.GetAdoptionBranch()
}

func adoptionFilesFromRequest(request *providersv1.CreateAdoptionPullRequestRequest) []providerservice.RepositoryFile {
	return repositoryFilesFromTypedProto(request.GetFiles())
}

func newAdoptionPullRequestInput(
	commonInput providerservice.RepositoryBranchPullRequestInput,
	adoptionBranch string,
	files []providerservice.RepositoryFile,
) providerservice.CreateAdoptionPullRequestInput {
	return providerservice.CreateAdoptionPullRequestInput{
		RepositoryBranchPullRequestInput: commonInput,
		AdoptionBranch:                   adoptionBranch,
		Files:                            files,
	}
}

// ScanRepositoryForAdoptionInput maps a lightweight adoption scan request to the domain model.
func ScanRepositoryForAdoptionInput(request *providersv1.ScanRepositoryForAdoptionRequest) (providerservice.ScanRepositoryForAdoptionInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.ScanRepositoryForAdoptionInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.ScanRepositoryForAdoptionInput{}, err
	}
	repositoryTarget, err := ProviderTargetFromProto(request.GetRepositoryTarget())
	if err != nil {
		return providerservice.ScanRepositoryForAdoptionInput{}, err
	}
	options := repositoryAdoptionScanOptionsFromProto(request.GetOptions())
	return providerservice.ScanRepositoryForAdoptionInput{
		ProviderSlug:      providerSlug(request.GetProviderSlug()),
		RepositoryTarget:  repositoryTarget,
		Options:           options,
		Meta:              meta,
		ExternalAccountID: externalAccountID,
	}, nil
}

func repositoryAdoptionScanOptionsFromProto(options *providersv1.RepositoryAdoptionScanOptions) providerservice.RepositoryAdoptionScanOptions {
	if options == nil {
		return providerservice.RepositoryAdoptionScanOptions{}
	}
	return providerservice.RepositoryAdoptionScanOptions{
		RequestedRef:       strings.TrimSpace(options.GetRequestedRef()),
		AllowedRefPrefixes: trimProtoStrings(options.GetAllowedRefPrefixes()),
		MaxTreeEntries:     int(options.GetMaxTreeEntries()),
		MaxMarkerPaths:     int(options.GetMaxMarkerPaths()),
		MarkerPathHints:    trimProtoStrings(options.GetMarkerPathHints()),
	}
}

// UpdatePullRequestInput maps a typed PR/MR update request to the domain model.
func UpdatePullRequestInput(request *providersv1.UpdatePullRequestRequest) (providerservice.UpdatePullRequestInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.UpdatePullRequestInput{}, err
	}
	target, err := ProviderTargetFromProto(request.GetTarget())
	if err != nil {
		return providerservice.UpdatePullRequestInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.UpdatePullRequestInput{}, err
	}
	labels, err := stringListPatchFromProto(request.GetLabels())
	if err != nil {
		return providerservice.UpdatePullRequestInput{}, err
	}
	assignees, err := stringListPatchFromProto(request.GetAssigneeProviderLogins())
	if err != nil {
		return providerservice.UpdatePullRequestInput{}, err
	}
	watermarkJSON := optionalJSONPointer(request.WatermarkJson)
	return providerservice.UpdatePullRequestInput{
		Target:                  target,
		Title:                   optionalStringPtrValue(request.Title),
		Body:                    optionalStringPtrValue(request.Body),
		Labels:                  labels,
		AssigneeProviderLogins:  assignees,
		Milestone:               optionalStringPtrValue(request.Milestone),
		State:                   optionalStringPtrValue(request.State),
		BaseBranch:              optionalStringPtrValue(request.BaseBranch),
		MaintainerCanModify:     request.MaintainerCanModify,
		WatermarkJSON:           watermarkJSON,
		ExpectedProviderVersion: strings.TrimSpace(request.GetExpectedProviderVersion()),
		Meta:                    meta,
		ExternalAccountID:       externalAccountID,
	}, nil
}

// CreateReviewSignalInput maps a typed review signal request to the domain model.
func CreateReviewSignalInput(request *providersv1.CreateReviewSignalRequest) (providerservice.CreateReviewSignalInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.CreateReviewSignalInput{}, err
	}
	target, err := ProviderTargetFromProto(request.GetTarget())
	if err != nil {
		return providerservice.CreateReviewSignalInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.CreateReviewSignalInput{}, err
	}
	kind, err := reviewSignalKindFromProto(request.GetKind())
	if err != nil {
		return providerservice.CreateReviewSignalInput{}, err
	}
	inlineComments, err := reviewInlineCommentsFromProto(request.GetInlineComments())
	if err != nil {
		return providerservice.CreateReviewSignalInput{}, err
	}
	return providerservice.CreateReviewSignalInput{
		Target:            target,
		Kind:              kind,
		Body:              strings.TrimSpace(request.GetBody()),
		InlineComments:    inlineComments,
		Meta:              meta,
		ExternalAccountID: externalAccountID,
	}, nil
}

// UpdateRelationshipInput maps a relationship update request to the domain model.
func UpdateRelationshipInput(request *providersv1.UpdateRelationshipRequest) (providerservice.UpdateRelationshipInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return providerservice.UpdateRelationshipInput{}, err
	}
	source, err := ProviderTargetFromProto(request.GetSource())
	if err != nil {
		return providerservice.UpdateRelationshipInput{}, err
	}
	var target *providerservice.ProviderTarget
	if request.Target != nil {
		mapped, targetErr := ProviderTargetFromProto(request.GetTarget())
		if targetErr != nil {
			return providerservice.UpdateRelationshipInput{}, targetErr
		}
		target = &mapped
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return providerservice.UpdateRelationshipInput{}, err
	}
	sourceKind, err := relationshipSourceFromProto(request.GetSourceKind())
	if err != nil {
		return providerservice.UpdateRelationshipInput{}, err
	}
	confidence, err := relationshipConfidenceFromProto(request.GetConfidence())
	if err != nil {
		return providerservice.UpdateRelationshipInput{}, err
	}
	return providerservice.UpdateRelationshipInput{
		Source:            source,
		Target:            target,
		TargetProviderRef: optionalStringPtrValue(request.TargetProviderRef),
		RelationshipType:  strings.TrimSpace(request.GetRelationshipType()),
		SourceKind:        sourceKind,
		Confidence:        confidence,
		Meta:              meta,
		ExternalAccountID: externalAccountID,
	}, nil
}

// ProviderTargetFromProto maps a provider-native target to the domain model.
func ProviderTargetFromProto(target *providersv1.ProviderTarget) (providerservice.ProviderTarget, error) {
	if target == nil {
		return providerservice.ProviderTarget{}, errs.ErrInvalidArgument
	}
	workItemKind, err := optionalWorkItemKindFromProto(target.GetWorkItemKind())
	if err != nil {
		return providerservice.ProviderTarget{}, err
	}
	return providerservice.ProviderTarget{
		ProviderSlug:         providerSlug(target.GetProviderSlug()),
		RepositoryFullName:   strings.TrimSpace(target.GetRepositoryFullName()),
		ProviderRepositoryID: strings.TrimSpace(target.GetProviderRepositoryId()),
		WorkItemKind:         workItemKind,
		Number:               target.GetNumber(),
		ProviderObjectID:     strings.TrimSpace(target.GetProviderObjectId()),
		WebURL:               strings.TrimSpace(target.GetWebUrl()),
	}, nil
}

func optionalWorkItemKindFromProto(kind providersv1.WorkItemKind) (enum.WorkItemKind, error) {
	if kind == providersv1.WorkItemKind_WORK_ITEM_KIND_UNSPECIFIED {
		return "", nil
	}
	return workItemKindFromProto(kind)
}

func reviewInlineCommentsFromProto(comments []*providersv1.ReviewInlineComment) ([]providerservice.ProviderInlineComment, error) {
	if len(comments) == 0 {
		return nil, nil
	}
	result := make([]providerservice.ProviderInlineComment, 0, len(comments))
	for _, comment := range comments {
		if comment == nil {
			return nil, errs.ErrInvalidArgument
		}
		line, err := optionalPositiveInt64Field(comment.Line)
		if err != nil {
			return nil, err
		}
		startLine, err := optionalPositiveInt64Field(comment.StartLine)
		if err != nil {
			return nil, err
		}
		result = append(result, providerservice.ProviderInlineComment{
			Path:                       strings.TrimSpace(comment.GetPath()),
			Body:                       strings.TrimSpace(comment.GetBody()),
			Line:                       line,
			StartLine:                  startLine,
			Side:                       strings.TrimSpace(comment.GetSide()),
			StartSide:                  strings.TrimSpace(comment.GetStartSide()),
			InReplyToProviderCommentID: strings.TrimSpace(comment.GetInReplyToProviderCommentId()),
		})
	}
	return result, nil
}

func stringListPatchFromProto(patch *providersv1.StringListPatch) (*value.StringListPatch, error) {
	if patch == nil {
		return nil, nil
	}
	values := trimProtoStrings(patch.GetValues())
	return &value.StringListPatch{Values: values}, nil
}

func optionalStringPtrValue(text *string) *string {
	if text == nil {
		return nil
	}
	value := strings.TrimSpace(*text)
	return &value
}

func optionalJSONPointer(text *string) *[]byte {
	if text == nil {
		return nil
	}
	value := []byte(strings.TrimSpace(*text))
	return &value
}

func relationshipSourceFromProto(source providersv1.RelationshipSource) (enum.RelationshipSource, error) {
	if source == providersv1.RelationshipSource_RELATIONSHIP_SOURCE_UNSPECIFIED {
		return "", errs.ErrInvalidArgument
	}
	mapped, ok := relationshipSources[source]
	if !ok {
		return "", errs.ErrInvalidArgument
	}
	return mapped, nil
}

func relationshipConfidenceFromProto(confidence providersv1.RelationshipConfidence) (enum.RelationshipConfidence, error) {
	if confidence == providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_UNSPECIFIED {
		return "", errs.ErrInvalidArgument
	}
	mapped, ok := relationshipConfidenceLevels[confidence]
	if !ok {
		return "", errs.ErrInvalidArgument
	}
	return mapped, nil
}

func optionalPositiveInt64Field(value *int64) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	if *value <= 0 {
		return nil, errs.ErrInvalidArgument
	}
	return value, nil
}

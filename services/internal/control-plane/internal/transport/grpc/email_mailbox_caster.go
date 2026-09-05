package grpc

import (
	"encoding/json"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func mailboxEnum(value int32, names map[int32]string, prefix string) (string, error) {
	name, ok := names[value]
	if !ok {
		return "", errs.ErrInvalid
	}
	if value == 0 {
		return "", nil
	}
	return strings.ToLower(strings.TrimPrefix(name, prefix)), nil
}

func mailboxDescriptor(input *cp.EmailMailboxCredentialReference) api.Descriptor {
	return api.Descriptor{Name: input.GetName(), Generation: input.GetGeneration()}
}

func mailboxEndpoint(input *cp.EmailMailboxEndpoint) (*api.Endpoint, error) {
	if input == nil {
		return nil, nil
	}
	auth, err := mailboxEnum(int32(input.GetAuthMethod()), cp.EmailMailboxAuthMethod_name, "EMAIL_MAILBOX_AUTH_METHOD_")
	if err != nil {
		return nil, err
	}
	tls, err := mailboxEnum(int32(input.GetTlsMode()), cp.EmailMailboxTLSMode_name, "EMAIL_MAILBOX_TLS_MODE_")
	if err != nil {
		return nil, err
	}
	return &api.Endpoint{Host: input.GetHost(), Port: int(input.GetPort()), ServerName: input.GetServerName(),
		AuthMethod: api.EndpointAuthMethod(auth), TlsMode: api.EndpointTlsMode(tls),
		Ca: mailboxDescriptor(input.GetCa()), Username: mailboxDescriptor(input.GetUsername()), Secret: mailboxDescriptor(input.GetSecret())}, nil
}

func mailboxSpecification(input *cp.EmailMailboxSpecification) (entity.EmailMailboxSpecification, error) {
	if input == nil {
		return entity.EmailMailboxSpecification{}, errs.ErrInvalid
	}
	protocol, err := mailboxEnum(int32(input.GetReceiveProtocol()), cp.EmailMailboxReceiveProtocol_name, "EMAIL_MAILBOX_RECEIVE_PROTOCOL_")
	if err != nil {
		return entity.EmailMailboxSpecification{}, err
	}
	result := entity.EmailMailboxSpecification{Enabled: input.GetEnabled(), ReceiveProtocol: api.MailboxReceiveProtocol(protocol),
		AllowedFolders: input.GetAllowedFolders(), ArchiveFolder: input.GetArchiveFolder(), DraftsFolder: input.GetDraftsFolder(),
		Folder: input.GetFolder(), Sender: input.GetSender(), ReplyTo: input.GetReplyTo(), Recipients: input.GetRecipients(), HelloName: input.GetHelloName()}
	for _, pair := range []struct {
		input  *cp.EmailMailboxEndpoint
		target **api.Endpoint
	}{
		{input.GetImap(), &result.IMAP}, {input.GetPop(), &result.POP},
	} {
		*pair.target, err = mailboxEndpoint(pair.input)
		if err != nil {
			return entity.EmailMailboxSpecification{}, err
		}
	}
	smtp, err := mailboxEndpoint(input.GetSmtp())
	if err != nil {
		return entity.EmailMailboxSpecification{}, err
	}
	if smtp != nil {
		result.SMTP = *smtp
	}
	limits := input.GetLimits()
	if int64(int(limits.GetAttachmentBytes())) != limits.GetAttachmentBytes() || int64(int(limits.GetMessageBytes())) != limits.GetMessageBytes() {
		return entity.EmailMailboxSpecification{}, errs.ErrInvalid
	}
	result.Limits = api.Limits{AttachmentBytes: int(limits.GetAttachmentBytes()), MessageBytes: int(limits.GetMessageBytes()),
		MaxAttachments: int(limits.GetMaxAttachments()), MaxRecipients: int(limits.GetMaxRecipients()),
		PageSize: int(limits.GetPageSize()), ScanMessages: int(limits.GetScanMessages()), TimeoutSeconds: int(limits.GetTimeoutSeconds())}
	for _, policy := range input.GetPolicies() {
		if policy == nil {
			return entity.EmailMailboxSpecification{}, errs.ErrInvalid
		}
		operation, err := mailboxEnum(int32(policy.GetOperation()), cp.EmailOperation_name, "EMAIL_OPERATION_")
		if err != nil {
			return entity.EmailMailboxSpecification{}, err
		}
		approval, err := mailboxEnum(int32(policy.GetPolicy()), cp.EmailApprovalPolicy_name, "EMAIL_APPROVAL_POLICY_")
		if err != nil {
			return entity.EmailMailboxSpecification{}, err
		}
		result.Policies = append(result.Policies, api.OperationPolicy{Operation: api.Operation(operation), Policy: api.Policy(approval), Folders: policy.GetFolders()})
	}
	return result, nil
}

func mailboxContent(input *cp.EmailMailboxDraftContent) (string, string, error) {
	switch content := input.GetContent().(type) {
	case *cp.EmailMailboxDraftContent_Yaml:
		return "YAML", content.Yaml, nil
	case *cp.EmailMailboxDraftContent_Specification:
		specification, err := mailboxSpecification(content.Specification)
		if err != nil {
			return "", "", err
		}
		raw, err := json.Marshal(specification)
		return "JSON", string(raw), err
	default:
		return "", "", errs.ErrInvalid
	}
}

func castMailboxDescriptor(input api.Descriptor) *cp.EmailMailboxCredentialReference {
	return &cp.EmailMailboxCredentialReference{Name: input.Name, Generation: input.Generation}
}
func castMailboxEndpoint(input *api.Endpoint) *cp.EmailMailboxEndpoint {
	if input == nil {
		return nil
	}
	return &cp.EmailMailboxEndpoint{Host: input.Host, Port: int32(input.Port), ServerName: input.ServerName,
		AuthMethod: cp.EmailMailboxAuthMethod(cp.EmailMailboxAuthMethod_value["EMAIL_MAILBOX_AUTH_METHOD_"+strings.ToUpper(string(input.AuthMethod))]),
		TlsMode:    cp.EmailMailboxTLSMode(cp.EmailMailboxTLSMode_value["EMAIL_MAILBOX_TLS_MODE_"+strings.ToUpper(string(input.TlsMode))]),
		Ca:         castMailboxDescriptor(input.Ca), Username: castMailboxDescriptor(input.Username), Secret: castMailboxDescriptor(input.Secret)}
}
func castMailboxSpecification(input entity.EmailMailboxSpecification) *cp.EmailMailboxSpecification {
	result := &cp.EmailMailboxSpecification{Enabled: input.Enabled,
		ReceiveProtocol: cp.EmailMailboxReceiveProtocol(cp.EmailMailboxReceiveProtocol_value["EMAIL_MAILBOX_RECEIVE_PROTOCOL_"+strings.ToUpper(string(input.ReceiveProtocol))]),
		AllowedFolders:  input.AllowedFolders, ArchiveFolder: input.ArchiveFolder, DraftsFolder: input.DraftsFolder,
		Folder: input.Folder, Sender: input.Sender, ReplyTo: input.ReplyTo, Recipients: input.Recipients, HelloName: input.HelloName,
		Smtp: castMailboxEndpoint(&input.SMTP), Imap: castMailboxEndpoint(input.IMAP), Pop: castMailboxEndpoint(input.POP),
		Limits: &cp.EmailMailboxLimits{AttachmentBytes: int64(input.Limits.AttachmentBytes), MessageBytes: int64(input.Limits.MessageBytes),
			MaxAttachments: int32(input.Limits.MaxAttachments), MaxRecipients: int32(input.Limits.MaxRecipients), PageSize: int32(input.Limits.PageSize),
			ScanMessages: int32(input.Limits.ScanMessages), TimeoutSeconds: int32(input.Limits.TimeoutSeconds)}}
	for _, policy := range input.Policies {
		result.Policies = append(result.Policies, &cp.EmailMailboxOperationPolicy{Operation: emailOperationProto(string(policy.Operation)),
			Policy: cp.EmailApprovalPolicy(cp.EmailApprovalPolicy_value["EMAIL_APPROVAL_POLICY_"+strings.ToUpper(string(policy.Policy))]), Folders: policy.Folders})
	}
	return result
}
func castMailboxDiagnostics(input []entity.EmailMailboxDiagnostic) []*cp.EmailMailboxDiagnostic {
	result := make([]*cp.EmailMailboxDiagnostic, 0, len(input))
	for _, item := range input {
		result = append(result, &cp.EmailMailboxDiagnostic{Code: item.Code, Path: item.Path, Message: item.Message, Line: item.Line, Column: item.Column})
	}
	return result
}
func castMailboxView(input entity.EmailMailboxConfigurationView) *cp.EmailMailboxConfigurationView {
	result := &cp.EmailMailboxConfigurationView{ConnectionRef: input.ConnectionRef, ConnectionVersion: input.ConnectionVersion,
		MailboxRef: input.MailboxRef, Configuration: castManagedConfiguration(&input.Configuration), Revision: castManagedRevision(&input.Revision),
		Specification: castMailboxSpecification(input.Specification), BoundRevisionRef: input.BoundRevisionRef, Diagnostics: castMailboxDiagnostics(input.Diagnostics)}
	result.NextActions = castMailboxActions(input.NextActions)
	if input.Publication != nil {
		item := input.Publication
		result.Publication = &cp.EmailMailboxPublication{Ref: item.Ref, Revision: item.Revision, Digest: item.Digest,
			State:                    cp.EmailMailboxPublicationState(cp.EmailMailboxPublicationState_value["EMAIL_MAILBOX_PUBLICATION_STATE_"+item.State]),
			ConfigurationRevisionRef: item.ConfigurationRevisionRef, CreatedAt: timestamppb.New(item.CreatedAt), FailureCode: item.FailureCode}
		if !item.ReadyAt.IsZero() {
			result.Publication.ReadyAt = timestamppb.New(item.ReadyAt)
		}
	}
	return result
}

func castMailboxActions(input []entity.EmailMailboxActionAvailability) []*cp.EmailMailboxActionAvailability {
	result := make([]*cp.EmailMailboxActionAvailability, 0, len(input))
	for _, item := range input {
		result = append(result, &cp.EmailMailboxActionAvailability{Action: cp.EmailMailboxAction(cp.EmailMailboxAction_value["EMAIL_MAILBOX_ACTION_"+item.Action]), Enabled: item.Enabled, Reason: cp.EmailMailboxActionReason(cp.EmailMailboxActionReason_value["EMAIL_MAILBOX_ACTION_REASON_"+item.Reason])})
	}
	return result
}

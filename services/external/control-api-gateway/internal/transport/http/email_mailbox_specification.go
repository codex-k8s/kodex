package httptransport

import (
	"encoding/json"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"strings"
)

func mailboxValue[T any](v *T) T {
	if v != nil {
		return *v
	}
	var zero T
	return zero
}
func mailboxPointer[T any](v T) *T { return &v }

type mailboxEnum interface {
	~string
	Valid() bool
}

func mailboxEnumInput[T mailboxEnum](v *T, values map[string]int32, prefix string) (int32, bool) {
	if v == nil {
		return 0, true
	}
	n, ok := values[prefix+string(*v)]
	return n, ok && (*v).Valid() && n != 0
}
func mailboxEnumView[T mailboxEnum](v int32, names map[int32]string, prefix string) (*T, bool) {
	if v == 0 {
		return nil, true
	}
	name, ok := names[v]
	result := T(strings.TrimPrefix(name, prefix))
	return &result, ok && result.Valid()
}

func mailboxContentInput(v generated.EmailMailboxDraftContent) (*cp.EmailMailboxDraftContent, bool) {
	if (v.Specification == nil) == (v.Yaml == nil) {
		return nil, false
	}
	if v.Yaml != nil {
		if len(*v.Yaml) > 262144 {
			return nil, false
		}
		return &cp.EmailMailboxDraftContent{Content: &cp.EmailMailboxDraftContent_Yaml{Yaml: *v.Yaml}}, true
	}
	spec, ok := mailboxSpecificationInput(v.Specification)
	return &cp.EmailMailboxDraftContent{Content: &cp.EmailMailboxDraftContent_Specification{Specification: spec}}, ok
}

func mailboxCredentialReferenceInput(v *generated.EmailMailboxCredentialReference) (*cp.EmailMailboxCredentialReference, bool) {
	if v == nil {
		return nil, true
	}
	result := &cp.EmailMailboxCredentialReference{}
	result.Name = mailboxValue(v.Name)
	result.Generation = mailboxValue(v.Generation)
	if result.Generation < 0 || result.Generation > maximumSafeJSONInteger {
		return nil, false
	}
	return result, true
}

func mailboxCredentialReferenceView(v *cp.EmailMailboxCredentialReference) (*generated.EmailMailboxCredentialReference, bool) {
	if v == nil {
		return nil, true
	}
	result := &generated.EmailMailboxCredentialReference{}
	result.Name = mailboxPointer(v.GetName())
	result.Generation = mailboxPointer(v.GetGeneration())
	if v.GetGeneration() < 0 || v.GetGeneration() > maximumSafeJSONInteger {
		return nil, false
	}
	return result, true
}

func mailboxLimitsInput(v *generated.EmailMailboxLimits) (*cp.EmailMailboxLimits, bool) {
	if v == nil {
		return nil, true
	}
	result := &cp.EmailMailboxLimits{}
	result.AttachmentBytes = mailboxValue(v.AttachmentBytes)
	result.MaxAttachments = mailboxValue(v.MaxAttachments)
	result.MaxRecipients = mailboxValue(v.MaxRecipients)
	result.MessageBytes = mailboxValue(v.MessageBytes)
	result.PageSize = mailboxValue(v.PageSize)
	result.ScanMessages = mailboxValue(v.ScanMessages)
	result.TimeoutSeconds = mailboxValue(v.TimeoutSeconds)
	if result.AttachmentBytes < 0 || result.MessageBytes < 0 || result.AttachmentBytes > maximumSafeJSONInteger || result.MessageBytes > maximumSafeJSONInteger {
		return nil, false
	}
	return result, true
}

func mailboxLimitsView(v *cp.EmailMailboxLimits) (*generated.EmailMailboxLimits, bool) {
	if v == nil {
		return nil, true
	}
	result := &generated.EmailMailboxLimits{}
	result.AttachmentBytes = mailboxPointer(v.GetAttachmentBytes())
	result.MaxAttachments = mailboxPointer(v.GetMaxAttachments())
	result.MaxRecipients = mailboxPointer(v.GetMaxRecipients())
	result.MessageBytes = mailboxPointer(v.GetMessageBytes())
	result.PageSize = mailboxPointer(v.GetPageSize())
	result.ScanMessages = mailboxPointer(v.GetScanMessages())
	result.TimeoutSeconds = mailboxPointer(v.GetTimeoutSeconds())
	if v.GetAttachmentBytes() < 0 || v.GetMessageBytes() < 0 || v.GetAttachmentBytes() > maximumSafeJSONInteger || v.GetMessageBytes() > maximumSafeJSONInteger {
		return nil, false
	}
	return result, true
}

func mailboxEndpointInput(v *generated.EmailMailboxEndpoint) (*cp.EmailMailboxEndpoint, bool) {
	if v == nil {
		return nil, true
	}
	result := &cp.EmailMailboxEndpoint{}
	result.Host = mailboxValue(v.Host)
	result.Port = mailboxValue(v.Port)
	result.ServerName = mailboxValue(v.ServerName)
	nTlsMode, okTlsMode := mailboxEnumInput(v.TlsMode, cp.EmailMailboxTLSMode_value, "EMAIL_MAILBOX_TLS_MODE_")
	if !okTlsMode {
		return nil, false
	}
	result.TlsMode = cp.EmailMailboxTLSMode(nTlsMode)
	nAuthMethod, okAuthMethod := mailboxEnumInput(v.AuthMethod, cp.EmailMailboxAuthMethod_value, "EMAIL_MAILBOX_AUTH_METHOD_")
	if !okAuthMethod {
		return nil, false
	}
	result.AuthMethod = cp.EmailMailboxAuthMethod(nAuthMethod)
	vCa, okCa := mailboxCredentialReferenceInput(v.Ca)
	if !okCa {
		return nil, false
	}
	result.Ca = vCa
	vUsername, okUsername := mailboxCredentialReferenceInput(v.Username)
	if !okUsername {
		return nil, false
	}
	result.Username = vUsername
	vSecret, okSecret := mailboxCredentialReferenceInput(v.Secret)
	if !okSecret {
		return nil, false
	}
	result.Secret = vSecret
	return result, true
}

func mailboxEndpointView(v *cp.EmailMailboxEndpoint) (*generated.EmailMailboxEndpoint, bool) {
	if v == nil {
		return nil, true
	}
	result := &generated.EmailMailboxEndpoint{}
	result.Host = mailboxPointer(v.GetHost())
	result.Port = mailboxPointer(v.GetPort())
	result.ServerName = mailboxPointer(v.GetServerName())
	nTlsMode, okTlsMode := mailboxEnumView[generated.EmailMailboxTLSMode](int32(v.GetTlsMode()), cp.EmailMailboxTLSMode_name, "EMAIL_MAILBOX_TLS_MODE_")
	if !okTlsMode {
		return nil, false
	}
	result.TlsMode = nTlsMode
	nAuthMethod, okAuthMethod := mailboxEnumView[generated.EmailMailboxAuthMethod](int32(v.GetAuthMethod()), cp.EmailMailboxAuthMethod_name, "EMAIL_MAILBOX_AUTH_METHOD_")
	if !okAuthMethod {
		return nil, false
	}
	result.AuthMethod = nAuthMethod
	vCa, okCa := mailboxCredentialReferenceView(v.Ca)
	if !okCa {
		return nil, false
	}
	result.Ca = vCa
	vUsername, okUsername := mailboxCredentialReferenceView(v.Username)
	if !okUsername {
		return nil, false
	}
	result.Username = vUsername
	vSecret, okSecret := mailboxCredentialReferenceView(v.Secret)
	if !okSecret {
		return nil, false
	}
	result.Secret = vSecret
	return result, true
}

func mailboxOperationPolicyInput(v *generated.EmailMailboxOperationPolicy) (*cp.EmailMailboxOperationPolicy, bool) {
	if v == nil {
		return nil, true
	}
	result := &cp.EmailMailboxOperationPolicy{}
	result.Folders = mailboxValue(v.Folders)
	nOperation, okOperation := mailboxEnumInput(v.Operation, cp.EmailOperation_value, "EMAIL_OPERATION_")
	if !okOperation {
		return nil, false
	}
	result.Operation = cp.EmailOperation(nOperation)
	nPolicy, okPolicy := mailboxEnumInput(v.Policy, cp.EmailApprovalPolicy_value, "EMAIL_APPROVAL_POLICY_")
	if !okPolicy {
		return nil, false
	}
	result.Policy = cp.EmailApprovalPolicy(nPolicy)
	if len(result.Folders) > 100 {
		return nil, false
	}
	return result, true
}

func mailboxOperationPolicyView(v *cp.EmailMailboxOperationPolicy) (*generated.EmailMailboxOperationPolicy, bool) {
	if v == nil {
		return nil, true
	}
	result := &generated.EmailMailboxOperationPolicy{}
	result.Folders = mailboxPointer(append([]string{}, v.GetFolders()...))
	nOperation, okOperation := mailboxEnumView[generated.EmailMailboxOperation](int32(v.GetOperation()), cp.EmailOperation_name, "EMAIL_OPERATION_")
	if !okOperation {
		return nil, false
	}
	result.Operation = nOperation
	nPolicy, okPolicy := mailboxEnumView[generated.EmailMailboxApprovalPolicy](int32(v.GetPolicy()), cp.EmailApprovalPolicy_name, "EMAIL_APPROVAL_POLICY_")
	if !okPolicy {
		return nil, false
	}
	result.Policy = nPolicy
	if len(v.GetFolders()) > 100 {
		return nil, false
	}
	return result, true
}

func mailboxSpecificationInput(v *generated.EmailMailboxSpecification) (*cp.EmailMailboxSpecification, bool) {
	if v == nil {
		return nil, true
	}
	result := &cp.EmailMailboxSpecification{}
	result.Enabled = mailboxValue(v.Enabled)
	result.AllowedFolders = mailboxValue(v.AllowedFolders)
	result.ArchiveFolder = mailboxValue(v.ArchiveFolder)
	result.DraftsFolder = mailboxValue(v.DraftsFolder)
	result.Folder = mailboxValue(v.Folder)
	result.Sender = mailboxValue(v.Sender)
	result.ReplyTo = mailboxValue(v.ReplyTo)
	result.Recipients = mailboxValue(v.Recipients)
	result.HelloName = mailboxValue(v.HelloName)
	nReceiveProtocol, okReceiveProtocol := mailboxEnumInput(v.ReceiveProtocol, cp.EmailMailboxReceiveProtocol_value, "EMAIL_MAILBOX_RECEIVE_PROTOCOL_")
	if !okReceiveProtocol {
		return nil, false
	}
	result.ReceiveProtocol = cp.EmailMailboxReceiveProtocol(nReceiveProtocol)
	vSmtp, okSmtp := mailboxEndpointInput(v.Smtp)
	if !okSmtp {
		return nil, false
	}
	result.Smtp = vSmtp
	vImap, okImap := mailboxEndpointInput(v.Imap)
	if !okImap {
		return nil, false
	}
	result.Imap = vImap
	vPop, okPop := mailboxEndpointInput(v.Pop)
	if !okPop {
		return nil, false
	}
	result.Pop = vPop
	vLimits, okLimits := mailboxLimitsInput(v.Limits)
	if !okLimits {
		return nil, false
	}
	result.Limits = vLimits
	policies := []*cp.EmailMailboxOperationPolicy{}
	for _, policy := range mailboxValue(v.Policies) {
		p, ok := mailboxOperationPolicyInput(&policy)
		if !ok || p == nil {
			return nil, false
		}
		policies = append(policies, p)
	}
	result.Policies = policies
	if len(result.GetAllowedFolders()) > 100 || len(result.GetRecipients()) > 1000 || len(result.GetPolicies()) > 21 {
		return nil, false
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > 262144 {
		return nil, false
	}
	return result, true
}

func mailboxSpecificationView(v *cp.EmailMailboxSpecification) (*generated.EmailMailboxSpecification, bool) {
	if v == nil {
		return nil, true
	}
	result := &generated.EmailMailboxSpecification{}
	result.Enabled = mailboxPointer(v.GetEnabled())
	result.AllowedFolders = mailboxPointer(append([]string{}, v.GetAllowedFolders()...))
	result.ArchiveFolder = mailboxPointer(v.GetArchiveFolder())
	result.DraftsFolder = mailboxPointer(v.GetDraftsFolder())
	result.Folder = mailboxPointer(v.GetFolder())
	result.Sender = mailboxPointer(v.GetSender())
	result.ReplyTo = mailboxPointer(v.GetReplyTo())
	result.Recipients = mailboxPointer(append([]string{}, v.GetRecipients()...))
	result.HelloName = mailboxPointer(v.GetHelloName())
	nReceiveProtocol, okReceiveProtocol := mailboxEnumView[generated.EmailMailboxReceiveProtocol](int32(v.GetReceiveProtocol()), cp.EmailMailboxReceiveProtocol_name, "EMAIL_MAILBOX_RECEIVE_PROTOCOL_")
	if !okReceiveProtocol {
		return nil, false
	}
	result.ReceiveProtocol = nReceiveProtocol
	vSmtp, okSmtp := mailboxEndpointView(v.Smtp)
	if !okSmtp {
		return nil, false
	}
	result.Smtp = vSmtp
	vImap, okImap := mailboxEndpointView(v.Imap)
	if !okImap {
		return nil, false
	}
	result.Imap = vImap
	vPop, okPop := mailboxEndpointView(v.Pop)
	if !okPop {
		return nil, false
	}
	result.Pop = vPop
	vLimits, okLimits := mailboxLimitsView(v.Limits)
	if !okLimits {
		return nil, false
	}
	result.Limits = vLimits
	policies := []generated.EmailMailboxOperationPolicy{}
	for _, policy := range v.GetPolicies() {
		p, ok := mailboxOperationPolicyView(policy)
		if !ok || p == nil {
			return nil, false
		}
		policies = append(policies, *p)
	}
	result.Policies = &policies
	if len(v.GetAllowedFolders()) > 100 || len(v.GetRecipients()) > 1000 || len(v.GetPolicies()) > 21 {
		return nil, false
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > 262144 {
		return nil, false
	}
	return result, true
}

package httptransport

import (
	"encoding/json"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"strings"
)

// mailboxValue dereferences an optional DTO value or returns its type's zero value; slices and other reference-bearing values retain their backing storage. Absence handling is representation mapping, not validation or secret resolution.
// mailboxValue разыменовывает необязательное DTO value либо возвращает zero value типа; срезы и другие ссылочные значения сохраняют общее хранилище. Это mapping отсутствия, не проверка и не разрешение секрета.
func mailboxValue[T any](v *T) T {
	if v != nil {
		return *v
	}
	var zero T
	return zero
}

// mailboxPointer returns a fresh pointer to a value copy for explicit public-field presence; any nested slices or pointers remain shared unless a caller copied them first.
// mailboxPointer возвращает новый pointer на копию значения для явного присутствия публичного поля; вложенные slices и pointers остаются общими, если вызывающий код заранее их не скопировал.
func mailboxPointer[T any](v T) *T { return &v }

// mailboxEnum constrains public mailbox string enums to their generated validity predicate, keeping format conversion separate from owner policy and authorization.
// mailboxEnum ограничивает публичные mailbox string enums их generated validity predicate, отделяя преобразование формата от owner policy и авторизации.
type mailboxEnum interface {
	~string
	// Valid reports membership in the generated public enum's closed set; it does not authorize the represented operation or establish runtime readiness.
	// Valid сообщает принадлежность закрытому набору generated public enum; представленную операцию не разрешает и runtime readiness не доказывает.
	Valid() bool
}

// mailboxEnumInput maps absent optional enums to zero and present values only when both the supplied protobuf map and public validity predicate agree on a nonzero value. Unknown or explicitly unspecified values fail instead of becoming a default.
// mailboxEnumInput преобразует отсутствие optional enum в zero, а заданное значение — только при согласии protobuf map и public validity predicate на ненулевое значение. Неизвестное и явно unspecified отклоняются, а не становятся default.
func mailboxEnumInput[T mailboxEnum](v *T, values map[string]int32, prefix string) (int32, bool) {
	if v == nil {
		return 0, true
	}
	n, ok := values[prefix+string(*v)]
	return n, ok && (*v).Valid() && n != 0
}

// mailboxEnumView maps protobuf zero to an absent public enum, otherwise looks up its name, removes the supplied prefix and checks public validity. The boolean must be honored even though a pointer is returned on an invalid mapped value.
// mailboxEnumView преобразует protobuf zero в отсутствие public enum, иначе ищет имя, убирает заданный prefix и проверяет public validity. Boolean обязателен для обработки, даже если при неверном значении возвращён pointer.
func mailboxEnumView[T mailboxEnum](v int32, names map[int32]string, prefix string) (*T, bool) {
	if v == 0 {
		return nil, true
	}
	name, ok := names[v]
	result := T(strings.TrimPrefix(name, prefix))
	return &result, ok && result.Valid()
}

// mailboxContentInput requires exactly one of specification or YAML, caps YAML at 256 KiB, and maps structured content through its bounded converter. It does not parse YAML or approve mailbox settings; callers must honor the boolean before sending the possibly invalid structured result.
// mailboxContentInput требует ровно одно из specification и YAML, ограничивает YAML 256 КиБ и преобразует структуру через bounded converter. YAML не разбирает и настройки не допускает; вызывающий обязан проверить boolean до отправки возможно неверного structured result.
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

// mailboxCredentialReferenceInput preserves absent references and maps only name/generation metadata with a nonnegative safe generation bound. It neither validates the name here nor reads or mounts any credential value.
// mailboxCredentialReferenceInput сохраняет отсутствие reference и переносит только name/generation metadata с неотрицательным safe generation bound. Имя здесь не проверяет, credential value не читает и не монтирует.
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

// mailboxCredentialReferenceView builds explicit public name/generation pointers from owner metadata after a safe-generation check; it exposes a reference, not a credential value or proof that the reference is currently resolvable.
// mailboxCredentialReferenceView создаёт явные публичные name/generation pointers из owner metadata с safe-generation check; выдаёт reference, не credential value и не proof текущей разрешимости reference.
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

// mailboxLimitsInput copies optional mailbox limits and rejects negative or unsafe attachment/message byte limits. Other count, port-independent and timeout semantics remain with owner validation; this mapper does not enforce effective runtime quotas.
// mailboxLimitsInput копирует optional mailbox limits и отклоняет отрицательные либо unsafe attachment/message byte limits. Семантика остальных counts и timeout остаётся owner validation; effective runtime quotas mapper не применяет.
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

// mailboxLimitsView projects each present owner's limits field as an explicit pointer while checking only attachment/message byte integer bounds locally. It does not infer defaults, validate all count ranges or prove runtime enforcement.
// mailboxLimitsView проецирует каждое поле owner limits как явный pointer, локально сверяет только integer bounds attachment/message bytes. Defaults не выводит, все count ranges не проверяет и runtime enforcement не доказывает.
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

// mailboxEndpointInput maps optional host/port/SNI metadata, closed TLS/auth enums and three credential references, failing on invalid enum/reference conversions. Network destination authorization, DNS, certificate checks and credential loading belong to the trusted owner/runtime path.
// mailboxEndpointInput переносит optional host/port/SNI metadata, закрытые TLS/auth enums и три credential references, отклоняет неверные преобразования enum/reference. Network destination authorization, DNS, проверка сертификата и загрузка credentials принадлежат trusted owner/runtime path.
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

// mailboxEndpointView converts an optional owner endpoint to explicit host/port/SNI fields, validated public TLS/auth enums and reference-only credentials. Conversion is not a network dial, endpoint allowlist or proof of a successful TLS handshake.
// mailboxEndpointView преобразует optional owner endpoint в явные host/port/SNI, проверенные публичные TLS/auth enums и reference-only credentials. Преобразование не выполняет dial, не является endpoint allowlist и не доказывает TLS handshake.
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

// mailboxOperationPolicyInput maps optional operation/approval enums and at most 100 folder entries; the folder slice may remain shared with the input DTO. It describes requested policy, not a server-owned send approval or authorization decision.
// mailboxOperationPolicyInput переносит optional operation/approval enums и не более 100 folders; срез folders может остаться общим с input DTO. Это описание requested policy, не server-owned send approval и не authorization decision.
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

// mailboxOperationPolicyView projects validated operation/approval enums with a fresh, explicitly allocated folder slice and the 100-entry bound. The public policy describes metadata and does not create approval authority.
// mailboxOperationPolicyView проецирует проверенные operation/approval enums с новым явно выделенным срезом folders и ограничением 100 элементов. Публичная policy описывает metadata и не создаёт approval authority.
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

// mailboxSpecificationInput maps optional settings, endpoints, limits and ordered operation policies, checking enum conversions, 100 folders, 1000 recipients, 21 policies and the 256-KiB marshaled size. Selected input slices remain shared; transport syntax checks are not full mailbox or permission validation.
// mailboxSpecificationInput переносит optional settings, endpoints, limits и порядок operation policies, проверяет enum conversions, 100 folders, 1000 recipients, 21 policies и размер marshaled result 256 КиБ. Часть input slices остаётся общей; transport syntax checks не заменяют полную проверку ящика и полномочий.
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

// mailboxSpecificationView projects optional owner settings with explicit scalar pointers, copied folder/recipient lists and ordered policy views, preserving collection and 256-KiB JSON limits. It validates representability, not provider connectivity, delivery admission or a decision to send mail.
// mailboxSpecificationView проецирует optional owner settings с явными scalar pointers, копиями folder/recipient lists и порядком policy views, сохраняя collection limits и JSON 256 КиБ. Проверяет представимость, не provider connectivity, delivery admission и решение отправить письмо.
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

package integration

import (
	"context"
	"net/http"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// atlassianRecoveryUser retains only stable Server identity coordinates and explicit activity;
// display names, emails and provider payload never enter a recovery receipt.
// atlassianRecoveryUser содержит только stable Server identity и явную activity;
// display names, emails и provider payload не попадают в recovery receipt.
type atlassianRecoveryUser struct {
	Key, UserKey, Type string
	Active             *bool
}

// decodeAtlassianRecoveryJSON rejects oversized, folded duplicate and ambiguous JSON before
// typed metadata is consumed. Unknown/malformed evidence never proves external effect absence.
// decodeAtlassianRecoveryJSON отклоняет oversized, folded duplicate и ambiguous JSON до
// использования typed metadata. Unknown/malformed evidence не доказывает отсутствие effect.
func decodeAtlassianRecoveryJSON(body []byte, target any) error {
	if integrationpackage.ValidateCredentialPermissionJSON(body) != nil || decodeProviderJSON(body, target) != nil {
		return &UnknownOutcomeError{}
	}
	return nil
}

// validAtlassianRecoveryKey bounds opaque Server keys without treating a display name, payload
// selector or browser principal as the identity of the authenticated external credential.
// validAtlassianRecoveryKey ограничивает opaque Server keys, не принимая display name, payload
// selector либо browser principal за identity authenticated external credential.
func validAtlassianRecoveryKey(key string) bool {
	if key == "" || len(key) > 256 || strings.TrimSpace(key) != key {
		return false
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// readAtlassianRecoveryIdentity resolves only the authenticated Server user through a fixed GET.
// Explicit Jira inactivity rejects this exact key; Confluence uses its documented known user type.
// readAtlassianRecoveryIdentity разрешает только authenticated Server user через fixed GET.
// Явная Jira inactivity отклоняет exact key; Confluence использует documented known user type.
func (adapter *Adapter) readAtlassianRecoveryIdentity(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (string, error) {
	var body []byte
	var err error
	if request.DefinitionKey == jiraServerDefinition {
		body, err = adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, "/myself", nil, nil, "")
	} else if request.DefinitionKey == confluenceServerDefinition {
		body, err = adapter.confluenceServerJSON(ctx, request, capability, configuration, http.MethodGet, "/user/current", nil, nil, "")
	} else {
		return "", &UnknownOutcomeError{}
	}
	if err != nil {
		return "", err
	}
	var user atlassianRecoveryUser
	if err := decodeAtlassianRecoveryJSON(body, &user); err != nil {
		return "", err
	}
	if request.DefinitionKey == jiraServerDefinition {
		if !validAtlassianRecoveryKey(user.Key) || user.Active == nil {
			return "", &UnknownOutcomeError{}
		}
		if !*user.Active {
			return "", &SafeError{Code: "INTEGRATION_AUTH_REJECTED", AuthenticationHTTPStatus: http.StatusUnauthorized}
		}
		return user.Key, nil
	}
	if user.Type != "known" || !validAtlassianRecoveryKey(user.UserKey) {
		return "", &UnknownOutcomeError{}
	}
	return user.UserKey, nil
}

// recoverAtlassianServer dispatches a closed set of marker-backed creates/updates. It cannot
// execute a transition, select arbitrary URLs or convert current status into action provenance.
// recoverAtlassianServer выбирает закрытый набор marker-backed creates/updates. Переход,
// произвольный URL либо превращение current status в action provenance невозможны.
func (adapter *Adapter) recoverAtlassianServer(ctx context.Context, request Request, definition integrationpackage.Package, capability integrationpackage.Capability, configuration map[string]string, input []byte) (Result, error) {
	if request.PersonalCredentialAdmission && request.ResourcePermissionProfile != integrationpackage.CredentialResourcePermissionProfile(definition, capability) {
		return Result{}, &UnknownOutcomeError{}
	}
	key, err := adapter.readAtlassianRecoveryIdentity(ctx, request, capability, configuration)
	if err != nil {
		return Result{}, err
	}
	switch request.Operation {
	case "jira.issue.create", "jira.issue.comment.write":
		return adapter.recoverJiraServerEffect(ctx, request, capability, configuration, input, key)
	case "confluence.page.create", "confluence.page.update":
		return adapter.recoverConfluenceServerEffect(ctx, request, capability, configuration, input, key)
	default:
		return Result{}, &UnknownOutcomeError{}
	}
}

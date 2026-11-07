package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// CredentialProbeResult contains only keys of actual successful or explicitly forbidden READs.
// Unobserved operations and write permissions stay unknown; provider payload and secret never leave.
// CredentialProbeResult содержит только keys успешных либо явно запрещённых READ. Ненаблюдённые
// операции и права записи остаются unknown; provider payload и secret не возвращаются.
type CredentialProbeResult struct {
	ConfirmedCapabilityKeys, DeniedCapabilityKeys []string
	ObservedPermissionProfiles                    []string
	ExpiresAt                                     *time.Time
}

// VerifyPersonalCredential performs the existing health check followed by a finite registry of
// inputless READs in the same configured scope. Each page is limited to one item when supported;
// partial transport failure grants no operation, and401 invalidates the whole observation.
// VerifyPersonalCredential выполняет health и конечный реестр READ без обязательных inputs в том
// же scope. Page ограничена одним item, где возможно; transport failure не подтверждает операцию,
// а401 отменяет всё observation. Метод не выводит write rights из успешного GET.
func (adapter *Adapter) VerifyPersonalCredential(ctx context.Context, request Request) (CredentialProbeResult, error) {
	definition, err := adapter.validateDefinition(request)
	if err != nil {
		return CredentialProbeResult{}, err
	}
	profiles, writeKeys := integrationpackage.CredentialObservationRequests(definition)
	if (len(request.PersonalObservationProfiles) != 0 || len(request.PersonalDeniedWriteCapabilityKeys) != 0) &&
		(!slices.Equal(profiles, request.PersonalObservationProfiles) || !slices.Equal(writeKeys, request.PersonalDeniedWriteCapabilityKeys)) {
		return CredentialProbeResult{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	probes := integrationpackage.CredentialReadProbes(definition)
	if len(probes) == 0 {
		return CredentialProbeResult{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	allowed := map[string]bool{probes[0].Key: true}
	registered := make(map[string]bool, len(probes))
	for _, probe := range probes {
		registered[probe.Key] = true
	}
	if len(request.PersonalReadProbeCapabilityKeys) > len(probes) {
		return CredentialProbeResult{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	if len(request.PersonalReadProbeCapabilityKeys) > 0 {
		allowed = make(map[string]bool, len(probes))
		for _, key := range request.PersonalReadProbeCapabilityKeys {
			if !registered[key] || allowed[key] {
				return CredentialProbeResult{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
			}
			allowed[key] = true
		}
		if !allowed[probes[0].Key] {
			return CredentialProbeResult{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
		}
	}
	if _, err := adapter.Test(ctx, request); err != nil {
		return CredentialProbeResult{}, err
	}
	configuration, err := normalizeStringMap(request.Configuration)
	if err != nil {
		return CredentialProbeResult{}, err
	}
	result := CredentialProbeResult{ConfirmedCapabilityKeys: []string{probes[0].Key}}
	for _, capability := range probes[1:] {
		if !allowed[capability.Key] {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		probe := request
		probe.healthCheck = true
		probe.CapabilityKey, probe.Operation = capability.Key, capability.Operation
		probe.Risk, probe.ApprovalPolicy, probe.ResourceKind = capability.Risk, capability.ApprovalPolicy, capability.ResourceScope.Kind
		probe.Input = map[string]any{}
		for _, field := range capability.InputFields {
			if field.Key == "limit" && field.Type == "INTEGER" {
				probe.Input[field.Key] = 1
			}
		}
		probe.ResourceScope, err = capability.ResourceScopeValues(configuration)
		if err != nil {
			return CredentialProbeResult{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
		}
		encodedScope, _ := json.Marshal(probe.ResourceScope)
		encodedInput, _ := json.Marshal(probe.Input)
		canonical, validationErr := capability.ValidateInput(encodedInput)
		if validationErr != nil {
			continue
		}
		scopeHash, inputHash := sha256.Sum256(encodedScope), sha256.Sum256(canonical)
		probe.ResourceScopeDigest, probe.InputDigest = hex.EncodeToString(scopeHash[:]), hex.EncodeToString(inputHash[:])
		probe.EffectKey = "personal-read-probe"
		_, err = adapter.Execute(ctx, probe)
		if err == nil {
			result.ConfirmedCapabilityKeys = append(result.ConfirmedCapabilityKeys, capability.Key)
			continue
		}
		var rejected *SafeError
		if errors.As(err, &rejected) && rejected.Code == "INTEGRATION_AUTH_REJECTED" {
			if rejected.AuthenticationHTTPStatus == http.StatusForbidden {
				result.DeniedCapabilityKeys = append(result.DeniedCapabilityKeys, capability.Key)
			} else {
				return CredentialProbeResult{}, err
			}
		}
	}
	if len(request.PersonalObservationProfiles) != 0 && ctx.Err() == nil {
		if err := adapter.observeGitLabPersonalWriteScope(ctx, request, definition, configuration, &result); err != nil {
			return CredentialProbeResult{}, err
		}
	}
	return result, nil
}

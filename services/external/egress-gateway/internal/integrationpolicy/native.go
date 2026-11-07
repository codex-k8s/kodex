package integrationpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// NativeActive binds an optional deployment-owned route set to the core connection budget.
// It does not authorize provider operations or carry integration credentials.
// NativeActive связывает optional набор маршрутов установки с общим бюджетом соединений.
// Он не разрешает provider-операции и не содержит credentials интеграций.
type NativeActive struct {
	document shared.NativeDocument
	digest   string
	limits   policy.Limits
}

// LoadNativeFile bounds the separate projected file; failure affects only its native listener.
// LoadNativeFile ограничивает отдельный projected файл; ошибка затрагивает лишь native listener.
func LoadNativeFile(path, expectedDigest string, base *policy.Active) (*NativeActive, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open native integration policy file")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, shared.MaximumFileBytes+1))
	if err != nil {
		return nil, errors.New("read native integration policy file")
	}
	return LoadNative(raw, expectedDigest, base)
}

// LoadNative rejects unknown/duplicate fields, trailing JSON, stale base policies and wrong digests.
// LoadNative отклоняет неизвестные/повторные поля, лишний JSON, старую base policy и неверные digest.
func LoadNative(raw []byte, expectedDigest string, base *policy.Active) (*NativeActive, error) {
	if base == nil || len(raw) == 0 || len(raw) > shared.MaximumFileBytes || policy.RejectDuplicateFields(raw) != nil {
		return nil, errors.New("native integration policy document is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document shared.NativeDocument
	if decoder.Decode(&document) != nil || decoder.Decode(new(any)) != io.EOF || document.Validate() != nil || document.GatewayPolicyDigest != base.Digest() || document.Digest() != expectedDigest {
		return nil, errors.New("native integration policy projection is invalid")
	}
	return &NativeActive{document: document, digest: expectedDigest, limits: base.Limits()}, nil
}

// Revision exposes the immutable native generation for CONNECT readback.
// Revision возвращает immutable native generation для CONNECT readback.
func (a *NativeActive) Revision() string {
	return "native-" + strconv.FormatInt(a.document.Generation, 10)
}

// Digest returns the full validated document digest rather than its shortened Service label.
// Digest возвращает полный проверенный digest документа вместо короткого Service label.
func (a *NativeActive) Digest() string { return a.digest }

// Limits preserves shared core listener budgets, including when other optional profiles are closed.
// Limits сохраняет общие core budgets, включая закрытые другие optional профили.
func (a *NativeActive) Limits() policy.Limits { return a.limits }

// ProfileIdentity distinguishes native peer reachability from OpenAPI and application grants.
// ProfileIdentity отличает native peer reachability от OpenAPI и прикладных grants.
func (a *NativeActive) ProfileIdentity() (string, string, string) {
	return shared.NativeProfileName, shared.Workload, shared.NativeOperation
}

// NativeIntegrationRoute copies exact pins so callers cannot mutate the active policy.
// NativeIntegrationRoute копирует точные pins без возможности изменить active policy извне.
func (a *NativeActive) NativeIntegrationRoute(host string) *shared.NativeDestination {
	if a != nil {
		for _, destination := range a.document.Destinations {
			if destination.Hostname == host {
				destination.AddressCIDRs = append([]string(nil), destination.AddressCIDRs...)
				return &destination
			}
		}
	}
	return nil
}

// Allows accepts only one registered hostname on TLS port443, without resolving ambient DNS.
// Allows принимает лишь зарегистрированное имя на TLS443 без ambient DNS.
func (a *NativeActive) Allows(host string, port int) bool {
	return port == 443 && a.NativeIntegrationRoute(host) != nil
}

// Ready means the immutable operator route set is valid, not that a provider token or API is ready.
// Ready означает корректность immutable маршрутов оператора, а не готовность provider token либо API.
func (a *NativeActive) Ready() (bool, string) {
	if a == nil {
		return false, "native integration projection unavailable"
	}
	return true, "ready"
}

// ReadyFor narrows readiness to an exact configured target and never refreshes private DNS.
// ReadyFor сужает готовность до точной настроенной цели без refresh private DNS.
func (a *NativeActive) ReadyFor(host string) (bool, string) {
	if a.NativeIntegrationRoute(host) == nil {
		return false, "native integration destination unavailable"
	}
	return true, "ready"
}

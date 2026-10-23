// Package integrationpolicy загружает отдельный immutable допуск OpenAPI CONNECT.
package integrationpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"strconv"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// Active holds a validated integration projection and inherited gateway limits.
// Active хранит проверенную проекцию интеграций и унаследованные лимиты gateway.
type Active struct {
	document shared.Document
	digest   string
	limits   policy.Limits
}

// LoadFile bounds a projected integration policy before strict decoding.
// LoadFile ограничивает файл политики интеграций до строгого разбора.
func LoadFile(path, expectedDigest string, base *policy.Active) (*Active, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open integration policy file")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, shared.MaximumFileBytes+1))
	if err != nil {
		return nil, errors.New("read integration policy file")
	}
	return Load(raw, expectedDigest, base)
}

// Load binds a strict projection to the base gateway digest and expected digest.
// Load привязывает строгую проекцию к базовому и ожидаемому дайджестам.
func Load(raw []byte, expectedDigest string, base *policy.Active) (*Active, error) {
	if base == nil || len(raw) == 0 || len(raw) > shared.MaximumFileBytes || policy.RejectDuplicateFields(raw) != nil {
		return nil, errors.New("integration policy document is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document shared.Document
	if decoder.Decode(&document) != nil || document.Validate() != nil || document.GatewayPolicyDigest != base.Digest() {
		return nil, errors.New("integration policy projection is invalid")
	}
	if document.Digest() != expectedDigest {
		return nil, errors.New("integration policy digest mismatch")
	}
	return &Active{document: document, digest: expectedDigest, limits: base.Limits()}, nil
}

// Revision derives the integration revision from its validated generation.
// Revision формирует ревизию интеграций из проверенного поколения.
func (a *Active) Revision() string {
	return "integration-" + strconv.FormatInt(a.document.Generation, 10)
}

// Digest returns the validated projection digest.
// Digest возвращает проверенный дайджест проекции.
func (a *Active) Digest() string { return a.digest }

// Limits returns the base gateway runtime bounds retained by the projection.
// Limits возвращает сохранённые проекцией лимиты базового gateway.
func (a *Active) Limits() policy.Limits { return a.limits }

// ProfileIdentity returns the fixed integration workload and operation identity.
// ProfileIdentity возвращает фиксированную идентичность нагрузки и операции интеграций.
func (a *Active) ProfileIdentity() (string, string, string) {
	return shared.ProfileName, shared.Workload, shared.Operation
}

// Configured reports whether at least one integration destination is present.
// Configured сообщает, задан ли хотя бы один адрес интеграции.
func (a *Active) Configured() bool { return len(a.document.Destinations) > 0 }

// Allows requires an exact destination with a registered TLS mode.
// Allows требует точный адрес с зарегистрированным режимом TLS.
func (a *Active) Allows(host string, port int) bool { return a.TLSMode(host, port) != "" }

// TLSMode selects implicit TLS only for an exact registered host and port.
// TLSMode выбирает обязательный TLS только для точных зарегистрированных узла и порта.
func (a *Active) TLSMode(host string, port int) string {
	for _, destination := range a.document.Destinations {
		if destination.Hostname == host && destination.Port == port {
			return "implicit"
		}
	}
	return ""
}

// AllowsLiteral checks an exact IP address against this destination's DNS pins.
// AllowsLiteral сверяет точный IP с DNS-pins соответствующего адреса.
func (a *Active) AllowsLiteral(host string, port int, address netip.Addr) bool {
	for _, destination := range a.document.Destinations {
		if destination.Hostname != host || destination.Port != port {
			continue
		}
		for _, pin := range destination.Addresses {
			if pin == address.String() {
				return true
			}
		}
	}
	return false
}

// Destinations copies exact host and port pairs for independent readiness checks.
// Destinations копирует точные пары узла и порта для независимой проверки готовности.
func (a *Active) Destinations() []policy.Destination {
	result := make([]policy.Destination, 0, len(a.document.Destinations))
	for _, destination := range a.document.Destinations {
		result = append(result, policy.Destination{Hostname: destination.Hostname, Port: destination.Port})
	}
	return result
}

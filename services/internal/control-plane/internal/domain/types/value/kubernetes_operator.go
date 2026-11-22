package value

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
)

// MaximumKubernetesOperatorBytes bounds the public operator artifact before parsing or expansion.
// MaximumKubernetesOperatorBytes ограничивает public operator artifact до parsing либо expansion.
const MaximumKubernetesOperatorBytes = 1 << 20

// MaximumKubernetesTargetBytes includes the bounded inline resource policy and target/protection metadata.
// MaximumKubernetesTargetBytes включает bounded inline resource policy и target/protection metadata.
const MaximumKubernetesTargetBytes = 160 << 10

// ErrKubernetesOperator rejects malformed metadata without echoing origins, files or certificate bytes.
// ErrKubernetesOperator отклоняет неверные metadata без вывода origins, файлов либо certificate bytes.
var ErrKubernetesOperator = errors.New("invalid managed Kubernetes operator registry")

// KubernetesIdentityUploadEndpoint describes only the operator-installed direct broker TLS route.
// It is not a caller-selected URL, platform client identity or credential storage locator.
// KubernetesIdentityUploadEndpoint описывает только установленный оператором direct broker TLS route.
// Это не caller-selected URL, platform client identity либо credential storage locator.
type KubernetesIdentityUploadEndpoint struct {
	ServerOrigin               string   `json:"serverOrigin"`
	TLSServerName              string   `json:"tlsServerName"`
	CertificateAuthoritySHA256 string   `json:"certificateAuthoritySHA256"`
	CertificateAuthorityPEM    []byte   `json:"certificateAuthorityPEM"`
	AddressIPs                 []string `json:"addressIPs"`
}

// KubernetesProtectedNamespace records an observed UID while its name stays protected across UID changes.
// Target aliases of the same ClusterUID cannot remove that persistent name protection.
// KubernetesProtectedNamespace хранит observed UID, сохраняя защиту имени после смены UID.
// Target aliases того же ClusterUID не снимают persistent name protection.
type KubernetesProtectedNamespace struct {
	Name string `json:"name"`
	UID  string `json:"uid"`
}

// KubernetesOperatorTarget contains trusted target metadata and protected namespace incarnations.
// Neither installation nor connection grants can override this protection boundary.
// KubernetesOperatorTarget содержит trusted target metadata и protected namespace incarnations.
// Installation и connection grants не переопределяют эту protection boundary.
type KubernetesOperatorTarget struct {
	Target              ManagedKubernetesTarget           `json:"target"`
	ProtectedNamespaces []KubernetesProtectedNamespace    `json:"protectedNamespaces"`
	ResourcePolicy      *managedkubernetes.ResourcePolicy `json:"resourcePolicy,omitempty"`
}

// KubernetesOperatorNode records one attested transport profile or its explicit retirement.
// Historical generations and serving-key ownership must also be checked in persistent owner state.
// KubernetesOperatorNode задаёт attested transport profile либо его explicit retirement.
// Исторические generations и serving-key ownership также проверяются в persistent owner state.
type KubernetesOperatorNode struct {
	Transport ManagedKubernetesNodeTransport `json:"transport"`
	Retired   bool                           `json:"retired"`
}

// KubernetesOperatorRegistry is an installer-produced immutable public metadata artifact.
// Loading it proves syntax only: owner import still enforces monotonic revision and retired pins.
// KubernetesOperatorRegistry — installer-produced immutable public metadata artifact.
// Loading доказывает лишь syntax: owner import проверяет monotonic revision и retired pins.
type KubernetesOperatorRegistry struct {
	Schema            string                           `json:"schema"`
	ManagedBy         string                           `json:"managedBy"`
	Source            string                           `json:"source"`
	InstallationRef   string                           `json:"installationRef"`
	Revision          int64                            `json:"revision"`
	ProducedAt        time.Time                        `json:"producedAt"`
	IdentityNamespace string                           `json:"identityNamespace"`
	UploadEndpoint    KubernetesIdentityUploadEndpoint `json:"uploadEndpoint"`
	Targets           []KubernetesOperatorTarget       `json:"targets"`
	Nodes             []KubernetesOperatorNode         `json:"nodes"`
}

// DecodeKubernetesOperatorRegistry accepts only repo-produced compact canonical JSON and one final LF.
// Roundtrip equality rejects duplicate/case-alias/unknown keys before metadata can affect ownership.
// DecodeKubernetesOperatorRegistry принимает лишь repo-produced compact canonical JSON и один final LF.
// Roundtrip equality отклоняет duplicate/case-alias/unknown keys до влияния metadata на ownership.
func DecodeKubernetesOperatorRegistry(raw []byte, now time.Time) (KubernetesOperatorRegistry, error) {
	if len(raw) == 0 || len(raw) > MaximumKubernetesOperatorBytes {
		return KubernetesOperatorRegistry{}, ErrKubernetesOperator
	}
	var result KubernetesOperatorRegistry
	if json.Unmarshal(raw, &result) != nil {
		return KubernetesOperatorRegistry{}, ErrKubernetesOperator
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(bytes.TrimSuffix(raw, []byte{'\n'}), canonical) || result.Validate(now) != nil {
		return KubernetesOperatorRegistry{}, ErrKubernetesOperator
	}
	return result, nil
}

// Validate checks bounded operator coordinates without granting RBAC, resource ownership or pin freshness.
// Retired/expired node metadata remains readable; each active transport separately checks its expiry.
// Validate проверяет bounded operator coordinates без выдачи RBAC, resource ownership либо pin freshness.
// Retired/expired node metadata доступны для чтения; каждый active transport проверяет expiry отдельно.
func (registry KubernetesOperatorRegistry) Validate(now time.Time) error {
	if now.IsZero() || registry.Schema != "managed-kubernetes-operator/v1" || registry.ManagedBy != "git" || registry.Source != "repo:kubernetes-operator" ||
		!kubernetesOperatorRef(registry.InstallationRef) ||
		!kubernetesOperatorRevision(registry.Revision) || registry.ProducedAt.IsZero() || registry.ProducedAt.After(now.Add(30*time.Second)) ||
		len(validation.IsDNS1123Label(registry.IdentityNamespace)) != 0 ||
		registry.Targets == nil || len(registry.Targets) > 32 || registry.Nodes == nil || len(registry.Nodes) > 256 ||
		registry.UploadEndpoint.Validate(registry.ProducedAt) != nil {
		return ErrKubernetesOperator
	}
	targets := map[string]ManagedKubernetesTarget{}
	clusters := map[string]bool{}
	for _, entry := range registry.Targets {
		target := entry.Target
		if !kubernetesOperatorRef(target.Ref) || !kubernetesOperatorRevision(target.Revision) || targets[target.Ref].Ref != "" ||
			!kubernetesOperatorUUID(target.ClusterUID) || clusters[target.ClusterUID] || !managedkubernetes.ValidLocator(target.EmbeddedContext) ||
			!managedkubernetes.ValidDigest(target.CertificateAuthoritySHA256) ||
			!managedkubernetes.ValidKubeconfigSourceOrigins(target.KubeconfigSourceOrigins) ||
			!kubernetesOperatorOrigin(target.ServerOrigin, target.TLSServerName, target.AddressIPs) ||
			entry.ProtectedNamespaces == nil || len(entry.ProtectedNamespaces) > 64 {
			return ErrKubernetesOperator
		}
		payload, payloadErr := json.Marshal(entry)
		if payloadErr != nil || len(payload) > MaximumKubernetesTargetBytes {
			return ErrKubernetesOperator
		}
		if entry.ResourcePolicy != nil {
			raw, err := json.Marshal(entry.ResourcePolicy)
			if err != nil {
				return ErrKubernetesOperator
			}
			if _, _, err := managedkubernetes.DecodeResourcePolicy(raw); err != nil {
				return ErrKubernetesOperator
			}
		}
		seen := map[string]bool{}
		for _, namespace := range entry.ProtectedNamespaces {
			if len(validation.IsDNS1123Label(namespace.Name)) != 0 || !kubernetesOperatorUUID(namespace.UID) || seen[namespace.Name] {
				return ErrKubernetesOperator
			}
			seen[namespace.Name] = true
		}
		if entry.ResourcePolicy != nil {
			for _, controller := range entry.ResourcePolicy.Controllers {
				if !seen[controller.ControllerNamespace] {
					return ErrKubernetesOperator
				}
			}
		}
		targets[target.Ref] = target
		clusters[target.ClusterUID] = true
	}
	refs, servingKeys, keyClusters, activeGroups := map[string]bool{}, map[string]string{}, map[string]string{}, map[string]bool{}
	for _, entry := range registry.Nodes {
		pin := entry.Transport
		if !kubernetesOperatorRef(pin.Ref) || refs[pin.Ref] || !kubernetesOperatorRevision(pin.Generation) ||
			!kubernetesOperatorUUID(pin.NodeUID) || len(validation.IsDNS1123Subdomain(pin.NodeName)) != 0 ||
			!kubernetesOperatorRef(pin.TargetRef) || !kubernetesOperatorRevision(pin.TargetRevision) ||
			(!entry.Retired && (targets[pin.TargetRef].Ref == "" || targets[pin.TargetRef].Revision != pin.TargetRevision)) || pin.Protocol.WireProtocol() == "" ||
			!managedkubernetes.ValidDigest(pin.ServerPublicKeySHA256) || pin.NotBefore.IsZero() || !pin.ExpiresAt.After(pin.NotBefore) ||
			pin.ExpiresAt.Sub(pin.NotBefore) > 24*time.Hour ||
			!kubernetesOperatorOrigin(pin.ServerOrigin, pin.TLSServerName, pin.AddressIPs) ||
			!kubernetesOperatorCA(pin.CertificateAuthorityPEM, pin.CertificateAuthoritySHA256, pin.NotBefore) {
			return ErrKubernetesOperator
		}
		key := pin.ServerPublicKeySHA256
		cluster := targets[pin.TargetRef].ClusterUID
		group := pin.TargetRef + ":" + pin.NodeUID + ":" + string(pin.Protocol)
		if !entry.Retired && activeGroups[group] {
			return ErrKubernetesOperator
		}
		if owner, exists := servingKeys[key]; exists && owner != pin.NodeUID {
			return ErrKubernetesOperator
		}
		if previous := keyClusters[key]; previous != "" && cluster != "" && previous != cluster {
			return ErrKubernetesOperator
		}
		servingKeys[key], refs[pin.Ref] = pin.NodeUID, true
		if cluster != "" {
			keyClusters[key] = cluster
		}
		if !entry.Retired {
			activeGroups[group] = true
		}
	}
	return nil
}

// Validate requires a current public CA bundle and exact direct TLS route without proxy URL features.
// Validate требует current public CA bundle и exact direct TLS route без proxy URL features.
func (endpoint KubernetesIdentityUploadEndpoint) Validate(now time.Time) error {
	if now.IsZero() || !kubernetesOperatorOrigin(endpoint.ServerOrigin, endpoint.TLSServerName, endpoint.AddressIPs) ||
		!kubernetesOperatorCA(endpoint.CertificateAuthorityPEM, endpoint.CertificateAuthoritySHA256, now) {
		return ErrKubernetesOperator
	}
	return nil
}

// kubernetesOperatorRef validates the same finite opaque reference alphabet as owner SQL constraints.
// kubernetesOperatorRef проверяет finite opaque reference alphabet из owner SQL constraints.
func kubernetesOperatorRef(ref string) bool {
	if len(ref) < 8 || len(ref) > 96 {
		return false
	}
	for _, character := range ref {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}

// kubernetesOperatorRevision keeps metadata integers exact across SQL, Proto and browser consumers.
// kubernetesOperatorRevision сохраняет точные metadata integers между SQL, Proto и browser consumers.
func kubernetesOperatorRevision(revision int64) bool {
	return revision > 0 && revision <= 9007199254740991
}

// kubernetesOperatorUUID rejects nil, noncanonical and guessed empty incarnation identifiers.
// kubernetesOperatorUUID отклоняет nil, noncanonical и фиктивные пустые incarnation identifiers.
func kubernetesOperatorUUID(raw string) bool {
	id, err := uuid.Parse(raw)
	return err == nil && id != uuid.Nil && id.String() == raw
}

// kubernetesOperatorOrigin accepts an explicit HTTPS origin and bounded literal address pins only.
// It resolves no DNS and does not turn an address pin into evidence of TLS or NodeUID ownership.
// kubernetesOperatorOrigin принимает explicit HTTPS origin и bounded literal address pins.
// Не разрешает DNS и не считает address pin доказательством TLS либо NodeUID ownership.
func kubernetesOperatorOrigin(origin, serverName string, addresses []string) bool {
	u, err := url.Parse(origin)
	if err != nil || !managedkubernetes.ValidHTTPSOrigin(origin) || len(origin) > 1024 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" ||
		u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery ||
		strings.ContainsAny(u.Host, "\\%") || !kubernetesOperatorDNSName(serverName) ||
		len(addresses) == 0 || len(addresses) > 8 {
		return false
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 || strconv.Itoa(value) != port {
			return false
		}
	}
	seen := map[netip.Addr]bool{}
	for _, raw := range addresses {
		address, err := netip.ParseAddr(raw)
		if err != nil || address.String() != raw || address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() ||
			address.Is4In6() || address.Zone() != "" || seen[address] {
			return false
		}
		seen[address] = true
	}
	if address, err := netip.ParseAddr(u.Hostname()); err == nil {
		if !seen[address] {
			return false
		}
	} else if !kubernetesOperatorDNSName(u.Hostname()) {
		return false
	}
	return true
}

// kubernetesOperatorDNSName enforces both total DNS length and the 63-byte limit of every label.
// Kubernetes' subdomain predicate alone does not impose the individual label length limit.
// kubernetesOperatorDNSName проверяет общую длину DNS и предел 63 bytes каждого label.
// Kubernetes subdomain predicate сам не ограничивает длину отдельного label.
func kubernetesOperatorDNSName(name string) bool {
	if len(validation.IsDNS1123Subdomain(name)) != 0 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(validation.IsDNS1123Label(label)) != 0 {
			return false
		}
	}
	return true
}

// kubernetesOperatorCA verifies exact PEM bytes, current CA constraints and the configured digest.
// kubernetesOperatorCA проверяет exact PEM bytes, current CA constraints и configured digest.
func kubernetesOperatorCA(raw []byte, digest string, now time.Time) bool {
	if len(raw) == 0 || len(raw) > 64<<10 || !managedkubernetes.ValidDigest(digest) {
		return false
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != digest {
		return false
	}
	count := 0
	for rest := bytes.TrimSpace(raw); len(rest) != 0; {
		block, tail := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || !bytes.HasPrefix(rest, []byte("-----BEGIN CERTIFICATE-----")) {
			return false
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !certificate.BasicConstraintsValid || !certificate.IsCA || certificate.KeyUsage&x509.KeyUsageCertSign == 0 ||
			now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
			return false
		}
		count++
		rest = bytes.TrimSpace(tail)
	}
	return count > 0
}

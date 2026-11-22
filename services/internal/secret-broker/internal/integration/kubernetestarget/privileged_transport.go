package kubernetestarget

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// privilegedTransportUnavailable prevents silent substitution of the API or an unpinned node.
	// privilegedTransportUnavailable запрещает скрытую подмену API либо незакреплённого node.
	privilegedTransportUnavailable = "managed Kubernetes privileged transport is unavailable"
)

// PrivilegedTransportPin is an operator-owned binding between one node incarnation and one
// kubelet serving key. It must be freshly resolved by the owner with the current target/pin
// generation. NodeUID is an attested registry binding, not an atomic nodes/proxy precondition:
// Kubernetes exposes no such wire field. Node replacement requires a new pin generation and
// serving key; retired key bindings must remain rejected by the persistent owner registry.
// CertificateAuthorityPEM is public trust material; authentication still uses only the selected
// broker-held credential. No kubelet or platform service identity may replace it.
// PrivilegedTransportPin — operator-owned привязка node incarnation к kubelet serving key.
// Owner свежо разрешает её с current target/pin generation. NodeUID — attested registry binding,
// не atomic nodes/proxy precondition: такого wire field в Kubernetes нет. Node replacement требует
// новой pin generation и serving key; persistent owner registry продолжает отклонять retired keys.
// CertificateAuthorityPEM — public trust material; authentication использует только выбранную
// broker-held credential. Подмена на kubelet либо platform service identity запрещена.
type PrivilegedTransportPin struct {
	Ref, TargetID, NodeName, NodeUID, ServerURL, TLSServerName string
	CertificateAuthorityHash, ServerPublicKeyHash              string
	CertificateAuthorityPEM                                    []byte
	AddressPins                                                []string
	TargetRevision, Generation                                 uint64
	NotBefore, ExpiresAt                                       time.Time
	Protocol                                                   PrivilegedProtocol
}

// ValidateFor rejects absent, expired or foreign operator pins before any TLS connection.
// Fresh current-generation ownership and persistent anti-rollback are separate owner checks;
// this method cannot establish them from caller-supplied fields.
// ValidateFor отклоняет отсутствующий, expired либо foreign operator pin до TLS connection.
// Fresh current-generation ownership и persistent anti-rollback проверяет owner отдельно;
// этот метод не выводит их из caller-supplied fields.
func (pin PrivilegedTransportPin) ValidateFor(target Target, now time.Time) error {
	id, err := uuid.Parse(pin.NodeUID)
	key, keyErr := hex.DecodeString(pin.ServerPublicKeyHash)
	if validateTarget(target) != nil || now.IsZero() || pin.Ref == "" || len(pin.Ref) > 256 ||
		pin.TargetID != target.ID || pin.TargetRevision != target.Revision || pin.Generation == 0 ||
		err != nil || id == uuid.Nil || id.String() != pin.NodeUID ||
		len(validation.IsDNS1123Subdomain(pin.NodeName)) != 0 ||
		keyErr != nil || len(key) != sha256.Size || hex.EncodeToString(key) != pin.ServerPublicKeyHash ||
		pin.NotBefore.IsZero() || now.Before(pin.NotBefore) || !now.Before(pin.ExpiresAt) ||
		pin.ExpiresAt.Sub(pin.NotBefore) > 24*time.Hour ||
		len(pin.CertificateAuthorityPEM) == 0 || len(pin.CertificateAuthorityPEM) > maxCredentialBytes {
		return errors.New(privilegedTransportUnavailable)
	}
	trustHash := sha256.Sum256(pin.CertificateAuthorityPEM)
	if pin.Protocol.WireProtocol() == "" {
		return errors.New(privilegedTransportUnavailable)
	}
	if hex.EncodeToString(trustHash[:]) != pin.CertificateAuthorityHash || validateTarget(pin.transportTarget(target)) != nil {
		return errors.New(privilegedTransportUnavailable)
	}
	return nil
}

// transportTarget constructs only a private transport configuration from an already owner-resolved
// pin. It does not change the selected API identity or the persisted target metadata.
// transportTarget создаёт только private transport configuration из owner-resolved pin.
// Не меняет выбранную API identity либо persistent target metadata.
func (pin PrivilegedTransportPin) transportTarget(target Target) Target {
	return Target{ID: target.ID, ClusterUID: target.ClusterUID, Revision: target.Revision, Context: target.Context,
		KubeconfigSourceOrigins: append([]string(nil), target.KubeconfigSourceOrigins...),
		ServerURL:               pin.ServerURL, TLSServerName: pin.TLSServerName,
		CertificateAuthorityHash: pin.CertificateAuthorityHash, AddressPins: append([]string(nil), pin.AddressPins...)}
}

// newPrivilegedClient requires both normal TLS verification and an exact kubelet serving-key pin.
// The selected token/client key remains private and unchanged. Creating a client does not authorize
// a stream: owner admission, ordinary Pod RBAC, nodes/proxy RBAC, protected-Pod policy and exact
// PodUID/container request construction remain mandatory before any interactive action.
// newPrivilegedClient требует normal TLS verification и exact kubelet serving-key pin.
// Выбранный token/client key остаётся private и неизменным. Создание client не разрешает stream:
// до interactive action обязательны owner admission, ordinary Pod RBAC, nodes/proxy RBAC,
// protected-Pod policy и exact PodUID/container request construction.
func (credential Credential) newPrivilegedClient(pin PrivilegedTransportPin, now time.Time, timeout time.Duration) (*http.Client, func(), error) {
	if pin.ValidateFor(credential.target, now) != nil {
		return nil, nil, errors.New(privilegedTransportUnavailable)
	}
	selected := credential
	selected.target = pin.transportTarget(credential.target)
	selected.ca = append([]byte(nil), pin.CertificateAuthorityPEM...)
	client, closeIdle, err := selected.NewClient(timeout)
	if err != nil {
		return nil, nil, errors.New(privilegedTransportUnavailable)
	}
	wrapper, ok := client.Transport.(*credentialTransport)
	if !ok {
		closeIdle()
		return nil, nil, errors.New(privilegedTransportUnavailable)
	}
	wrapper.validUntil = pin.ExpiresAt
	transport, ok := wrapper.base.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify {
		closeIdle()
		return nil, nil, errors.New(privilegedTransportUnavailable)
	}
	expected := pin.ServerPublicKeyHash
	transport.TLSClientConfig.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
			return errors.New(privilegedTransportUnavailable)
		}
		actual := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
		if hex.EncodeToString(actual[:]) != expected {
			return errors.New(privilegedTransportUnavailable)
		}
		return nil
	}
	return client, closeIdle, nil
}

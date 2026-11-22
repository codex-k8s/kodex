// Package kubernetestarget owns the provider boundary for broker-held Kubernetes credentials.
// Пакет kubernetestarget владеет границей провайдера для Kubernetes credentials, хранимых broker.
package kubernetestarget

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/google/uuid"
	"go.yaml.in/yaml/v3"
)

const (
	// maxCredentialBytes bounds the private document before parsing or expansion.
	// maxCredentialBytes ограничивает приватный документ до разбора или раскрытия значений.
	maxCredentialBytes = managedkubernetes.MaximumCredentialBytes
	// invalidCredential is deliberately independent of parser errors, key bytes and subject names.
	// invalidCredential намеренно не содержит ошибок parser, ключей или имён субъектов.
	invalidCredential = "invalid managed Kubernetes credential"
)

// Target pins operator-owned connection metadata; it never comes from invocation input.
// Target закрепляет принадлежащие оператору параметры соединения; они не берутся из input вызова.
type Target = value.KubernetesTarget

// Credential retains only validated embedded authentication material inside the broker boundary.
// Credential хранит только проверенные встроенные реквизиты внутри границы broker.
type Credential struct {
	target Target
	ca     []byte
	token  string
	cert   tls.Certificate
}

// String prevents accidental formatting of the private authentication fields.
// String предотвращает случайный вывод приватных реквизитов форматированием.
func (Credential) String() string { return "[managed Kubernetes credential]" }

// GoString also redacts detailed Go formatting of credentials.
// GoString также скрывает реквизиты при подробном Go-форматировании.
func (Credential) GoString() string { return "[managed Kubernetes credential]" }

// kubeconfigDocument accepts only the embedded, single-context subset; omitted SDK features stay forbidden.
// kubeconfigDocument принимает только встроенное подмножество с одним context; остальные функции SDK запрещены.
type kubeconfigDocument struct {
	APIVersion     string         `yaml:"apiVersion"`
	Kind           string         `yaml:"kind"`
	Preferences    struct{}       `yaml:"preferences"`
	CurrentContext string         `yaml:"current-context"`
	Clusters       []namedCluster `yaml:"clusters"`
	Contexts       []namedContext `yaml:"contexts"`
	Users          []namedUser    `yaml:"users"`
}

// namedCluster binds exactly one embedded trust bundle and endpoint to its selected name.
// namedCluster связывает один встроенный trust bundle и endpoint с выбранным именем.
type namedCluster struct {
	Name    string `yaml:"name"`
	Cluster struct {
		Server    string `yaml:"server"`
		TLSName   string `yaml:"tls-server-name"`
		Authority string `yaml:"certificate-authority-data"`
	} `yaml:"cluster"`
}

// namedContext binds the sole context to explicit cluster and user names without ambient defaults.
// namedContext связывает единственный context с явными именами cluster и user без внешних defaults.
type namedContext struct {
	Name    string `yaml:"name"`
	Context struct {
		Cluster   string `yaml:"cluster"`
		User      string `yaml:"user"`
		Namespace string `yaml:"namespace"`
	} `yaml:"context"`
}

// namedUser excludes filesystem paths, exec, auth-provider, impersonation and password authentication.
// namedUser исключает пути файлов, exec, auth-provider, impersonation и аутентификацию паролем.
type namedUser struct {
	Name string `yaml:"name"`
	User struct {
		Token       string `yaml:"token"`
		Certificate string `yaml:"client-certificate-data"`
		Key         string `yaml:"client-key-data"`
	} `yaml:"user"`
}

// ParseCredential rejects ambiguous or non-embedded kubeconfig before any SDK or network operation.
// It matches the sole context and source alias, then pins transport/TLS/CA to trusted operator metadata.
// It never reads files, environment, plugins or default in-cluster credentials.
// ParseCredential отклоняет неоднозначный или невстроенный kubeconfig до SDK и сетевых операций.
// Проверяет единственный context и source alias, закрепляя transport/TLS/CA по operator metadata.
// Не читает файлы, окружение, plugins или default in-cluster credentials.
func ParseCredential(raw []byte, target Target, now time.Time) (Credential, error) {
	if len(raw) == 0 || len(raw) > maxCredentialBytes || validateTarget(target) != nil || now.IsZero() {
		return Credential{}, errors.New(invalidCredential)
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(&document) != nil || validateCredentialNode(&document, 0) != nil {
		return Credential{}, errors.New(invalidCredential)
	}
	var trailing yaml.Node
	if decoder.Decode(&trailing) != io.EOF {
		return Credential{}, errors.New(invalidCredential)
	}
	var config kubeconfigDocument
	decoder = yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if decoder.Decode(&config) != nil || config.APIVersion != "v1" || config.Kind != "Config" ||
		config.CurrentContext != target.Context || len(config.Clusters) != 1 ||
		len(config.Contexts) != 1 || len(config.Users) != 1 {
		return Credential{}, errors.New(invalidCredential)
	}
	cluster, context, user := config.Clusters[0], config.Contexts[0], config.Users[0]
	if cluster.Name == "" || user.Name == "" || context.Name != target.Context ||
		context.Context.Cluster != cluster.Name || context.Context.User != user.Name ||
		!managedkubernetes.AllowsKubeconfigSourceOrigin(target.KubeconfigSourceOrigins, cluster.Cluster.Server) ||
		(cluster.Cluster.TLSName != "" && cluster.Cluster.TLSName != target.TLSServerName) {
		return Credential{}, errors.New(invalidCredential)
	}
	ca, err := base64.StdEncoding.Strict().DecodeString(cluster.Cluster.Authority)
	if err != nil || !validAuthority(ca, now) {
		return Credential{}, errors.New(invalidCredential)
	}
	hash := sha256.Sum256(ca)
	if hex.EncodeToString(hash[:]) != target.CertificateAuthorityHash {
		return Credential{}, errors.New(invalidCredential)
	}
	target.AddressPins = append([]string(nil), target.AddressPins...)
	target.KubeconfigSourceOrigins = append([]string(nil), target.KubeconfigSourceOrigins...)
	cloneCredentialResourcePolicy(&target)
	credential := Credential{target: target, ca: ca}
	if user.User.Token != "" {
		if user.User.Certificate != "" || user.User.Key != "" || len(user.User.Token) > 16<<10 ||
			strings.ContainsAny(user.User.Token, " \t\r\n") || strings.TrimSpace(user.User.Token) != user.User.Token {
			return Credential{}, errors.New(invalidCredential)
		}
		for _, character := range user.User.Token {
			if character < 0x21 || character > 0x7e {
				return Credential{}, errors.New(invalidCredential)
			}
		}
		credential.token = user.User.Token
		return credential, nil
	}
	cert, certErr := base64.StdEncoding.Strict().DecodeString(user.User.Certificate)
	key, keyErr := base64.StdEncoding.Strict().DecodeString(user.User.Key)
	defer clear(key)
	if certErr != nil || keyErr != nil || len(cert) == 0 || len(key) == 0 {
		return Credential{}, errors.New(invalidCredential)
	}
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil || len(pair.Certificate) == 0 {
		return Credential{}, errors.New(invalidCredential)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return Credential{}, errors.New(invalidCredential)
	}
	allowedUsage := false
	for _, usage := range leaf.ExtKeyUsage {
		allowedUsage = allowedUsage || usage == x509.ExtKeyUsageClientAuth || usage == x509.ExtKeyUsageAny
	}
	if !allowedUsage {
		return Credential{}, errors.New(invalidCredential)
	}
	credential.cert = pair
	return credential, nil
}

// validateTarget checks trusted metadata itself before it is used as a TLS or endpoint pin.
// validateTarget проверяет доверенные метаданные до использования как TLS или endpoint pin.
func validateTarget(target Target) error {
	if validateCredentialResourcePolicy(target) != nil {
		return errors.New(invalidCredential)
	}
	u, err := url.Parse(target.ServerURL)
	cluster, clusterErr := uuid.Parse(target.ClusterUID)
	hash, hashErr := hex.DecodeString(target.CertificateAuthorityHash)
	if target.ID == "" || target.Revision == 0 || target.Revision > 9007199254740991 || target.Context == "" ||
		clusterErr != nil || cluster == uuid.Nil || cluster.String() != target.ClusterUID ||
		!managedkubernetes.ValidHTTPSOrigin(target.ServerURL) ||
		!managedkubernetes.ValidKubeconfigSourceOrigins(target.KubeconfigSourceOrigins) ||
		err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" ||
		u.ForceQuery || strings.ContainsAny(u.Host, "\\%") ||
		target.TLSServerName == "" || strings.ContainsAny(target.TLSServerName, " /\\:@\t\r\n") ||
		hashErr != nil || len(hash) != sha256.Size || target.CertificateAuthorityHash != strings.ToLower(target.CertificateAuthorityHash) {
		return errors.New(invalidCredential)
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return errors.New(invalidCredential)
		}
	}
	if len(target.AddressPins) == 0 || len(target.AddressPins) > 8 {
		return errors.New(invalidCredential)
	}
	seen := map[netip.Addr]bool{}
	for _, pin := range target.AddressPins {
		address, err := netip.ParseAddr(pin)
		if err != nil || address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() ||
			address.Is4In6() || address.Zone() != "" || seen[address] || address.String() != pin {
			return errors.New(invalidCredential)
		}
		seen[address] = true
	}
	if address, err := netip.ParseAddr(u.Hostname()); err == nil && !seen[address] {
		return errors.New(invalidCredential)
	}
	return nil
}

// validAuthority accepts only a nonempty PEM CA bundle with current CA certificates and no extra data.
// validAuthority принимает только непустой PEM CA bundle с действующими CA certificates без лишних данных.
func validAuthority(raw []byte, now time.Time) bool {
	remaining := bytes.TrimSpace(raw)
	count := 0
	for len(remaining) > 0 {
		block, rest := pem.Decode(remaining)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 ||
			!bytes.HasPrefix(remaining, []byte("-----BEGIN CERTIFICATE-----")) {
			return false
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !certificate.IsCA || !certificate.BasicConstraintsValid ||
			now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
			return false
		}
		count++
		remaining = bytes.TrimSpace(rest)
	}
	return count > 0
}

// validateCredentialNode rejects YAML ambiguity and non-scalar keys before typed kubeconfig decoding.
// It bounds nesting and forbids aliases, anchors, merges, custom tags and implicit timestamps.
// validateCredentialNode отклоняет неоднозначный YAML и нестроковые ключи до typed kubeconfig decode.
// Ограничивает вложенность и запрещает aliases, anchors, merges, custom tags и implicit timestamps.
func validateCredentialNode(node *yaml.Node, depth int) error {
	if node == nil || depth > 12 || node.Anchor != "" || node.Alias != nil || node.Kind == yaml.AliasNode {
		return errors.New(invalidCredential)
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if depth != 0 || len(node.Content) != 1 {
			return errors.New(invalidCredential)
		}
	case yaml.MappingNode:
		if node.Tag != "!!map" || len(node.Content)%2 != 0 {
			return errors.New(invalidCredential)
		}
		keys := map[string]bool{}
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" || keys[key.Value] {
				return errors.New(invalidCredential)
			}
			keys[key.Value] = true
		}
	case yaml.SequenceNode:
		if node.Tag != "!!seq" {
			return errors.New(invalidCredential)
		}
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return errors.New(invalidCredential)
		}
	default:
		return errors.New(invalidCredential)
	}
	for _, child := range node.Content {
		if validateCredentialNode(child, depth+1) != nil {
			return errors.New(invalidCredential)
		}
	}
	return nil
}

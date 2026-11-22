package kubernetestarget

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// credentialFixture creates disposable embedded CA/client material without external files or accounts.
// credentialFixture создаёт одноразовые встроенные CA/client реквизиты без файлов и внешних аккаунтов.
func credentialFixture(t *testing.T) (kubeconfigDocument, Target, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("generate disposable certificate key")
	}
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic-target"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, private)
	if err != nil {
		t.Fatal("create disposable certificate")
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal("encode disposable key")
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	hash := sha256.Sum256(ca)
	target := Target{
		ID: "synthetic-cluster", Revision: 1, ServerURL: "https://api.example.invalid:6443",
		ClusterUID:    "629c395b-32aa-425b-814d-5cd9a9a42da6",
		TLSServerName: "api.example.invalid", CertificateAuthorityHash: hex.EncodeToString(hash[:]),
		Context: "synthetic-context", AddressPins: []string{"192.0.2.1"},
		KubeconfigSourceOrigins: []string{"https://api.example.invalid:6443"},
	}
	config := kubeconfigDocument{APIVersion: "v1", Kind: "Config", CurrentContext: target.Context}
	cluster := namedCluster{Name: "cluster"}
	cluster.Cluster.Server = target.ServerURL
	cluster.Cluster.Authority = base64.StdEncoding.EncodeToString(ca)
	config.Clusters = []namedCluster{cluster}
	context := namedContext{Name: target.Context}
	context.Context.Cluster, context.Context.User = "cluster", "user"
	config.Contexts = []namedContext{context}
	user := namedUser{Name: "user"}
	user.User.Certificate = base64.StdEncoding.EncodeToString(ca)
	user.User.Key = base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}))
	config.Users = []namedUser{user}
	return config, target, now
}

// encodeCredential encodes only synthetic fixtures without logging their private material on failures.
// encodeCredential кодирует только synthetic fixtures без вывода приватных данных при ошибках.
func encodeCredential(t *testing.T, config kubeconfigDocument) []byte {
	t.Helper()
	raw, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal("encode disposable kubeconfig")
	}
	return raw
}

// TestParseCredentialAcceptsOnlyPinnedEmbeddedIdentity covers both authentication modes and redaction.
// TestParseCredentialAcceptsOnlyPinnedEmbeddedIdentity проверяет оба режима аутентификации и скрытие данных.
func TestParseCredentialAcceptsOnlyPinnedEmbeddedIdentity(t *testing.T) {
	config, target, now := credentialFixture(t)
	credential, err := ParseCredential(encodeCredential(t, config), target, now)
	if err != nil || len(credential.cert.Certificate) != 1 || credential.token != "" {
		t.Fatal("embedded certificate identity was rejected")
	}
	config.Users[0].User.Certificate, config.Users[0].User.Key = "", ""
	config.Users[0].User.Token = "synthetic-token-do-not-print"
	credential, err = ParseCredential(encodeCredential(t, config), target, now)
	if err != nil || credential.token != config.Users[0].User.Token {
		t.Fatal("embedded bearer identity was rejected")
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, credential), "synthetic-token-do-not-print") {
			t.Fatal("credential formatting disclosed private material")
		}
	}
	target.AddressPins[0] = "192.0.2.2"
	if credential.target.AddressPins[0] != "192.0.2.1" {
		t.Fatal("caller mutation changed pinned target")
	}
}

// TestParseCredentialRejectsClientFeatures proves untrusted kubeconfig never reaches executable loaders.
// TestParseCredentialRejectsClientFeatures доказывает отказ небезопасных client features до исполняемых loaders.
func TestParseCredentialRejectsClientFeatures(t *testing.T) {
	config, target, now := credentialFixture(t)
	raw := string(encodeCredential(t, config))
	cases := map[string]string{
		"exec plugin":        strings.Replace(raw, "    user:\n", "    user:\n        exec: {command: /definitely-not-executed}\n", 1),
		"auth provider":      strings.Replace(raw, "    user:\n", "    user:\n        auth-provider: {name: example}\n", 1),
		"token file":         strings.Replace(raw, "    user:\n", "    user:\n        tokenFile: /definitely-not-read\n", 1),
		"certificate file":   strings.Replace(raw, "    user:\n", "    user:\n        client-certificate: /definitely-not-read\n", 1),
		"key file":           strings.Replace(raw, "    user:\n", "    user:\n        client-key: /definitely-not-read\n", 1),
		"username":           strings.Replace(raw, "    user:\n", "    user:\n        username: forbidden\n", 1),
		"impersonation":      strings.Replace(raw, "    user:\n", "    user:\n        as: forbidden\n", 1),
		"proxy":              strings.Replace(raw, "    cluster:\n", "    cluster:\n        proxy-url: https://proxy.invalid\n", 1),
		"insecure false":     strings.Replace(raw, "    cluster:\n", "    cluster:\n        insecure-skip-tls-verify: false\n", 1),
		"CA file":            strings.Replace(raw, "    cluster:\n", "    cluster:\n        certificate-authority: /definitely-not-read\n", 1),
		"extensions":         raw + "extensions: []\n",
		"duplicate":          raw + "apiVersion: v1\n",
		"multiple documents": raw + "---\napiVersion: v1\n",
		"anchor":             strings.Replace(raw, "kind: Config", "kind: &value Config", 1),
		"alias":              raw + "unexpected: *value\n",
		"merge":              raw + "<<: {}\n",
		"custom tag":         strings.Replace(raw, "kind: Config", "kind: !custom Config", 1),
		"nonstring key":      raw + "1: value\n",
		"implicit number":    strings.Replace(raw, "kind: Config", "kind: 123", 1),
		"oversized":          strings.Repeat(" ", maxCredentialBytes+1),
	}
	for name, candidate := range cases {
		t.Run(name, func(t *testing.T) {
			if candidate == raw {
				t.Fatal("unsafe fixture did not modify the document")
			}
			_, err := ParseCredential([]byte(candidate), target, now)
			if err == nil || err.Error() != invalidCredential {
				t.Fatal("unsafe client feature was not rejected with a closed diagnostic")
			}
		})
	}
}

// TestParseCredentialRejectsIdentityAndTrustMismatch verifies exact pins and mutually exclusive identities.
// TestParseCredentialRejectsIdentityAndTrustMismatch проверяет exact pins и взаимоисключающие identities.
func TestParseCredentialRejectsIdentityAndTrustMismatch(t *testing.T) {
	cases := map[string]func(*kubeconfigDocument, *Target, *time.Time){
		"context": func(config *kubeconfigDocument, _ *Target, _ *time.Time) { config.CurrentContext = "foreign" },
		"cluster reference": func(config *kubeconfigDocument, _ *Target, _ *time.Time) {
			config.Contexts[0].Context.Cluster = "foreign"
		},
		"user reference": func(config *kubeconfigDocument, _ *Target, _ *time.Time) { config.Contexts[0].Context.User = "foreign" },
		"endpoint": func(config *kubeconfigDocument, _ *Target, _ *time.Time) {
			config.Clusters[0].Cluster.Server = "https://foreign.invalid"
		},
		"TLS name": func(config *kubeconfigDocument, _ *Target, _ *time.Time) {
			config.Clusters[0].Cluster.TLSName = "foreign.invalid"
		},
		"CA hash": func(_ *kubeconfigDocument, target *Target, _ *time.Time) {
			target.CertificateAuthorityHash = strings.Repeat("0", 64)
		},
		"expired CA":     func(_ *kubeconfigDocument, _ *Target, now *time.Time) { *now = now.Add(2 * time.Hour) },
		"mixed identity": func(config *kubeconfigDocument, _ *Target, _ *time.Time) { config.Users[0].User.Token = "synthetic" },
		"missing key":    func(config *kubeconfigDocument, _ *Target, _ *time.Time) { config.Users[0].User.Key = "" },
		"empty identity": func(config *kubeconfigDocument, _ *Target, _ *time.Time) {
			config.Users[0].User.Certificate, config.Users[0].User.Key = "", ""
		},
		"second context": func(config *kubeconfigDocument, _ *Target, _ *time.Time) {
			config.Contexts = append(config.Contexts, config.Contexts[0])
		},
		"revision":            func(_ *kubeconfigDocument, target *Target, _ *time.Time) { target.Revision = 0 },
		"inexact revision":    func(_ *kubeconfigDocument, target *Target, _ *time.Time) { target.Revision = 9007199254740992 },
		"missing cluster UID": func(_ *kubeconfigDocument, target *Target, _ *time.Time) { target.ClusterUID = "" },
		"nil cluster UID": func(_ *kubeconfigDocument, target *Target, _ *time.Time) {
			target.ClusterUID = "00000000-0000-0000-0000-000000000000"
		},
		"missing source aliases": func(_ *kubeconfigDocument, target *Target, _ *time.Time) { target.KubeconfigSourceOrigins = nil },
		"duplicate source aliases": func(_ *kubeconfigDocument, target *Target, _ *time.Time) {
			target.KubeconfigSourceOrigins = append(target.KubeconfigSourceOrigins, target.KubeconfigSourceOrigins[0])
		},
		"plaintext": func(_ *kubeconfigDocument, target *Target, _ *time.Time) {
			target.ServerURL = "http://api.example.invalid"
		},
		"userinfo": func(_ *kubeconfigDocument, target *Target, _ *time.Time) {
			target.ServerURL = "https://user:secret@api.example.invalid"
		},
		"path": func(_ *kubeconfigDocument, target *Target, _ *time.Time) { target.ServerURL += "/raw" },
		"port zero": func(_ *kubeconfigDocument, target *Target, _ *time.Time) {
			target.ServerURL = "https://api.example.invalid:0"
		},
		"no address pins": func(_ *kubeconfigDocument, target *Target, _ *time.Time) { target.AddressPins = nil },
		"unspecified transport destination": func(_ *kubeconfigDocument, target *Target, _ *time.Time) {
			target.ServerURL = "https://0.0.0.0:40123"
		},
		"duplicate address pins": func(_ *kubeconfigDocument, target *Target, _ *time.Time) {
			target.AddressPins = []string{"192.0.2.1", "192.0.2.1"}
		},
		"zone address":        func(_ *kubeconfigDocument, target *Target, _ *time.Time) { target.AddressPins = []string{"fe80::1%lo"} },
		"unbound IP endpoint": func(_ *kubeconfigDocument, target *Target, _ *time.Time) { target.ServerURL = "https://192.0.2.2" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			config, target, now := credentialFixture(t)
			mutate(&config, &target, &now)
			if _, err := ParseCredential(encodeCredential(t, config), target, now); err == nil {
				t.Fatal("untrusted or unavailable identity was accepted")
			}
		})
	}
}

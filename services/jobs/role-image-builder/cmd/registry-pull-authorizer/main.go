package main

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/nodepullidentity"
)

const (
	pullServerError                  = "registry pull authorizer failed"
	pullBasicAuthenticationChallenge = `Basic realm="kodex-registry"`
	internalPullRegistryHost         = "kodex-image-registry.kodex-system.svc.cluster.local:5000"
)

type pullProfile struct {
	configFile   string
	repositories []string
}

type pullAuthorizationDecision struct {
	failure       string
	statusCode    int
	authChallenge string
}

func main() {
	registryHost := os.Getenv("REGISTRY_PULL_HOST")
	if !strings.Contains(registryHost, ".") || strings.ContainsAny(registryHost, "/:@?# \\\r\n\t") {
		log.Fatal(pullServerError)
	}
	pool := x509.NewCertPool()
	for _, path := range []string{"/identity/client-ca.pem", "/identity/node-client-ca.pem"} {
		value, err := os.ReadFile(path)
		if err != nil || !pool.AppendCertsFromPEM(value) {
			log.Fatal(pullServerError)
		}
	}
	backend, _ := url.Parse("http://127.0.0.1:5006")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("redirect rejected") }}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		certificate, failure := pullClientCertificate(request)
		decision := denyPullRequest(failure)
		if failure == "" {
			decision = decidePullAuthorization(request, certificate, registryHost)
		}
		if decision.failure != "" {
			log.Printf("%s: request denied reason=%s", pullServerError, decision.failure)
			writePullAuthorizationDenial(writer, decision)
			return
		}
		target := *backend
		target.Path, target.RawQuery = request.URL.Path, request.URL.RawQuery
		upstream, err := http.NewRequestWithContext(request.Context(), request.Method, target.String(), nil)
		if err != nil {
			http.Error(writer, "upstream request rejected", http.StatusBadGateway)
			return
		}
		upstream.Header = request.Header.Clone()
		upstream.Header.Del("Authorization")
		response, err := client.Do(upstream)
		if err != nil {
			http.Error(writer, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			for _, value := range values {
				writer.Header().Add(key, value)
			}
		}
		writer.WriteHeader(response.StatusCode)
		_, _ = io.Copy(writer, io.LimitReader(response.Body, 1<<30))
	})
	server := &http.Server{Addr: ":5000", Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}} //nolint:gosec // exact client CAs and TLS 1.3.
	if err := server.ListenAndServeTLS("/identity/tls.crt", "/identity/tls.key"); err != nil {
		log.Fatal(pullServerError)
	}
}

func pullClientCertificate(request *http.Request) (*x509.Certificate, string) {
	if request == nil || request.TLS == nil {
		return nil, "tls_state"
	}
	if len(request.TLS.PeerCertificates) == 0 {
		return nil, "client_certificate"
	}
	// RequireAndVerifyClientCert уже проверил всю цепочку. Для профиля нужен
	// только leaf; наличие переданных intermediate не является ошибкой.
	return request.TLS.PeerCertificates[0], ""
}

func decidePullAuthorization(
	request *http.Request,
	certificate *x509.Certificate,
	registryHost string,
) pullAuthorizationDecision {
	if request.Method != http.MethodGet && request.Method != http.MethodHead || strings.ContainsAny(request.URL.Path, "\r\n\\") {
		return denyPullRequest("request_shape")
	}
	cn := certificate.Subject.CommonName
	profiles := map[string]pullProfile{
		"kodex-image-registry-pull-probe": {"/identity/probe-dockerconfig.json", []string{"kodex/control-plane"}},
		"kodex-buildkit-base-pull":        {"/identity/buildkit-dockerconfig.json", []string{"kodex/dockerfile", "kodex/agent-runner", "kodex/role-base-documents"}},
		"role-image-builder-input-read":   {"/identity/input-dockerconfig.json", []string{"kodex/role-image-inputs"}},
	}
	if profile, ok := profiles[cn]; ok {
		return decidePullProfileAuthorization(request, profile, registryHost)
	}
	if !authorizedNodePull(request, certificate, registryHost) {
		return denyPullRequest("node_identity")
	}
	if !pathInRepositories(request.URL.Path, []string{"kodex/agent-runner", "kodex/roles"}) {
		return denyPullRequest("node_repository")
	}
	return pullAuthorizationDecision{}
}

func decidePullProfileAuthorization(
	request *http.Request,
	profile pullProfile,
	registryHost string,
) pullAuthorizationDecision {
	username, password, supplied := request.BasicAuth()
	if !supplied {
		if len(request.Header.Values("Authorization")) == 0 {
			return pullAuthorizationDecision{
				failure:       "profile_credential_missing",
				statusCode:    http.StatusUnauthorized,
				authChallenge: pullBasicAuthenticationChallenge,
			}
		}
		return denyPullRequest("profile_credential")
	}
	if !dockerCredentialMatches(profile.configFile, username, password, registryHost) {
		return denyPullRequest("profile_credential")
	}
	if !pathInRepositories(request.URL.Path, profile.repositories) {
		return denyPullRequest("profile_repository")
	}
	return pullAuthorizationDecision{}
}

func denyPullRequest(failure string) pullAuthorizationDecision {
	if failure == "" {
		return pullAuthorizationDecision{}
	}
	return pullAuthorizationDecision{failure: failure, statusCode: http.StatusForbidden}
}

func writePullAuthorizationDenial(writer http.ResponseWriter, decision pullAuthorizationDecision) {
	if decision.authChallenge != "" {
		writer.Header().Set("WWW-Authenticate", decision.authChallenge)
	}
	http.Error(writer, "request denied", decision.statusCode)
}

func authorizedNodePull(request *http.Request, certificate *x509.Certificate, registryHost string) bool {
	remote, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || net.ParseIP(remote) == nil {
		return false
	}
	matchedIP := false
	for _, value := range certificate.IPAddresses {
		if value.Equal(net.ParseIP(remote)) {
			matchedIP = true
		}
	}
	if !matchedIP {
		return false
	}
	username, password, ok := request.BasicAuth()
	parts := strings.Split(password, ".")
	if !ok || username != certificate.Subject.CommonName || len(parts) != 3 || parts[0] != "v1" {
		return false
	}
	generation, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || !nodepullidentity.ValidCommonName(username, generation) {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
	if err != nil || !ok {
		return false
	}
	digest := sha256.Sum256([]byte(username + "\n" + parts[1] + "\n" + registryHost))
	return rsa.VerifyPSS(publicKey, cryptoHashSHA256, digest[:], signature, nil) == nil
}

const cryptoHashSHA256 = 5 // crypto.SHA256; kept constant to avoid mutable algorithm selection.

func dockerCredentialMatches(path, username, password, registryHost string) bool {
	value, err := os.ReadFile(path)
	if err != nil || len(value) > 1<<20 {
		return false
	}
	var document struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if json.Unmarshal(value, &document) != nil || len(document.Auths) < 1 || len(document.Auths) > 2 {
		return false
	}
	for host := range document.Auths {
		if host != internalPullRegistryHost && host != registryHost {
			return false
		}
	}
	entry, ok := document.Auths[registryHost]
	if !ok {
		entry, ok = document.Auths[internalPullRegistryHost]
	}
	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	return ok && err == nil && string(decoded) == username+":"+password
}

func pathInRepositories(path string, repositories []string) bool {
	if path == "/v2/" {
		return true
	}
	for _, repository := range repositories {
		if strings.HasPrefix(path, "/v2/"+repository+"/manifests/") || strings.HasPrefix(path, "/v2/"+repository+"/blobs/") {
			return true
		}
	}
	return false
}

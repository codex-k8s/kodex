package main

import (
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	expectedClientCN                     = "kodex-buildkit-staging-push"
	evidenceBasicAuthenticationChallenge = `Basic realm="kodex-image-registry-evidence"`
	serverError                          = "registry write authorizer failed"
	denialLog                            = "registry request denied"
	registryHeaderTimeout                = 5 * time.Second
	registryStreamTimeout                = 15 * time.Minute
	registryIdleTimeout                  = 30 * time.Second
)

type authorizationProfile struct {
	address, backend string
	evidence         bool
}

func main() {
	ca, err := os.ReadFile("/identity/client-ca.pem")
	if err != nil {
		log.Fatal(serverError)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		log.Fatal(serverError)
	}
	profile := authorizationProfile{address: ":5001", backend: "http://127.0.0.1:5005"}
	if os.Getenv("REGISTRY_AUTHORIZATION_PROFILE") == "evidence" {
		profile = authorizationProfile{address: ":5007", backend: "http://127.0.0.1:5008", evidence: true}
	} else if value := os.Getenv("REGISTRY_AUTHORIZATION_PROFILE"); value != "" && value != "staging" {
		log.Fatal(serverError)
	}
	backend, _ := url.Parse(profile.backend)
	client := newRegistryProxyClient()
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 ||
			len(request.TLS.VerifiedChains) == 0 {
			logRegistryDenial(profile, "unverified_mtls", request.Method)
			http.Error(writer, "request forbidden", http.StatusForbidden)
			return
		}
		commonName := request.TLS.PeerCertificates[0].Subject.CommonName
		if !profile.evidence &&
			(commonName != expectedClientCN || !allowedRegistryWrite(request.Method, request.URL.Path)) {
			logRegistryDenial(profile, "operation", request.Method)
			http.Error(writer, "request forbidden", http.StatusForbidden)
			return
		}
		if profile.evidence && !allowedEvidenceRequest(commonName, request.Method, request.URL.Path) {
			logRegistryDenial(profile, "operation", request.Method)
			http.Error(writer, "request forbidden", http.StatusForbidden)
			return
		}
		if profile.evidence &&
			!authorizedEvidenceCredential(commonName, request.Header.Get("Authorization")) {
			logRegistryDenial(profile, "application_credential", request.Method)
			writeEvidenceCredentialDenial(writer, request.Header.Values("Authorization"))
			return
		}
		target := *backend
		target.Path, target.RawQuery = request.URL.Path, request.URL.RawQuery
		upstream, err := http.NewRequestWithContext(request.Context(), request.Method, target.String(), request.Body)
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
	server := newRegistryProxyServer(profile.address, handler, pool)
	if err := server.ListenAndServeTLS("/identity/tls.crt", "/identity/tls.key"); err != nil {
		log.Fatal(serverError)
	}
}

func writeEvidenceCredentialDenial(writer http.ResponseWriter, authorization []string) {
	status := http.StatusForbidden
	if len(authorization) == 0 {
		writer.Header().Set("WWW-Authenticate", evidenceBasicAuthenticationChallenge)
		status = http.StatusUnauthorized
	}
	http.Error(writer, "request forbidden", status)
}

func newRegistryProxyClient() *http.Client {
	return &http.Client{
		Timeout: registryStreamTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("redirect rejected")
		},
	}
}

func newRegistryProxyServer(address string, handler http.Handler, pool *x509.CertPool) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: registryHeaderTimeout,
		ReadTimeout:       registryStreamTimeout,
		WriteTimeout:      registryStreamTimeout,
		IdleTimeout:       registryIdleTimeout,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCAs:  pool,
		},
	} //nolint:gosec // exact client CA and TLS 1.3.
}

func logRegistryDenial(profile authorizationProfile, reason, method string) {
	profileName := "staging"
	if profile.evidence {
		profileName = "evidence"
	}
	log.Printf(
		"%s: profile=%s reason=%s method=%s",
		denialLog,
		profileName,
		reason,
		normalizeMethod(method),
	)
}

func normalizeMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPatch,
		http.MethodPut, http.MethodDelete:
		return method
	default:
		return "OTHER"
	}
}

func allowedEvidenceRequest(commonName, method, path string) bool {
	if path == "/v2/" && method == http.MethodGet {
		return commonName == "image-admission" || commonName == "image-promotion" ||
			commonName == "kodex-image-registry-evidence-probe"
	}
	if commonName == "kodex-image-registry-evidence-probe" || method == http.MethodDelete ||
		!pathInRegistryRepository(path, "evidence/role-image-admission") || strings.ContainsAny(path, "\r\n\\") {
		return false
	}
	if commonName == "image-promotion" {
		return method == http.MethodGet || method == http.MethodHead
	}
	if commonName != "image-admission" {
		return false
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPatch, http.MethodPut:
		return true
	default:
		return false
	}
}

func authorizedEvidenceCredential(commonName, authorization string) bool {
	return authorizedEvidenceCredentialWithReader(commonName, authorization, os.ReadFile)
}

func authorizedEvidenceCredentialWithReader(
	commonName string,
	authorization string,
	readFile func(string) ([]byte, error),
) bool {
	prefix := "/identity/"
	switch commonName {
	case "image-admission":
		prefix += "admission"
	case "image-promotion":
		prefix += "promotion"
	case "kodex-image-registry-evidence-probe":
		prefix += "probe"
	default:
		return false
	}
	username, userErr := readFile(prefix + ".username")
	password, passwordErr := readFile(prefix + ".password")
	if userErr != nil || passwordErr != nil || len(username) == 0 || len(password) == 0 ||
		len(username) > 4096 || len(password) > 4096 {
		return false
	}
	usernameValue := strings.TrimRight(string(username), "\r\n")
	passwordValue := strings.TrimRight(string(password), "\r\n")
	if usernameValue == "" || passwordValue == "" || strings.ContainsAny(usernameValue, "\r\n") ||
		strings.ContainsAny(passwordValue, "\r\n") {
		return false
	}
	credential := usernameValue + ":" + passwordValue
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(credential))
	return subtle.ConstantTimeCompare([]byte(authorization), []byte(expected)) == 1
}

func allowedRegistryWrite(method, path string) bool {
	if method == http.MethodGet && path == "/v2/" {
		return true
	}
	if method == http.MethodDelete || !strings.HasPrefix(path, "/v2/") || strings.ContainsAny(path, "\r\n\\") {
		return false
	}
	for _, candidate := range []string{"staging/role-images", "staging/readiness"} {
		if pathInRegistryRepository(path, candidate) {
			switch method {
			case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPatch, http.MethodPut:
				return true
			default:
				return false
			}
		}
	}
	return false
}

func pathInRegistryRepository(path, repository string) bool {
	remainder := strings.TrimPrefix(path, "/v2/"+repository+"/")
	return remainder != path && (strings.HasPrefix(remainder, "blobs/") || strings.HasPrefix(remainder, "manifests/"))
}

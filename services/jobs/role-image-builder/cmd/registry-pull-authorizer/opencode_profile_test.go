package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/nodepullidentity"
)

// TestOptionalRuntimeProfileKeepsDefaultAndOtherIdentitiesUnchanged pins the explicit runtime boundary.
// TestOptionalRuntimeProfileKeepsDefaultAndOtherIdentitiesUnchanged закрепляет явный выбор runtime и прежние права.
func TestOptionalRuntimeProfileKeepsDefaultAndOtherIdentitiesUnchanged(t *testing.T) {
	t.Parallel()
	defaults := pullProfiles()
	for _, engine := range []string{"", "codex"} {
		profiles, valid := pullProfilesForRuntime(engine)
		if !valid || !reflect.DeepEqual(profiles, defaults) {
			t.Fatal("default pull profiles changed")
		}
	}
	profiles, valid := pullProfilesForRuntime("opencode")
	if !valid {
		t.Fatal("optional runtime was rejected")
	}
	for name, profile := range defaults {
		if name != "kodex-buildkit-base-pull" && name != "kodex-node-pull-installer" && !reflect.DeepEqual(profile, profiles[name]) {
			t.Fatal("another identity scope changed")
		}
	}
	for _, name := range []string{"kodex-buildkit-base-pull", "kodex-node-pull-installer"} {
		want := append(append([]string(nil), defaults[name].repositories...), "kodex/agent-runner-opencode")
		if !reflect.DeepEqual(profiles[name].repositories, want) {
			t.Fatal("optional repository scope changed")
		}
	}
	for _, engine := range []string{"unknown", "OPENCODE", " opencode", "opencode,other"} {
		if profiles, valid := pullProfilesForRuntime(engine); valid || profiles != nil {
			t.Fatal("unknown runtime accepted")
		}
	}
	if !reflect.DeepEqual(defaults, pullProfiles()) {
		t.Fatal("runtime selection mutated defaults")
	}
}

// TestOptionalRuntimeNodeAuthorizationKeepsSignatureAndIPBinding uses signed synthetic credentials on the real node decision path.
// TestOptionalRuntimeNodeAuthorizationKeepsSignatureAndIPBinding проверяет настоящий node decision с подписанными тестовыми реквизитами.
func TestOptionalRuntimeNodeAuthorizationKeepsSignatureAndIPBinding(t *testing.T) {
	t.Parallel()
	const host = "registry.example.invalid"
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	name := nodepullidentity.CommonName("synthetic-node", 1)
	digest := sha256.Sum256([]byte(name + "\n1\n" + host))
	signature, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, digest[:], nil)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{Subject: pkix.Name{CommonName: name}, IPAddresses: []net.IP{net.ParseIP("192.0.2.1")}, PublicKey: &key.PublicKey}
	profiles, _ := pullProfilesForRuntime("opencode")
	request := httptest.NewRequest(http.MethodHead, "https://"+host+"/v2/kodex/agent-runner-opencode/manifests/sha256:abc", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	request.SetBasicAuth(name, "v1.1."+base64.RawURLEncoding.EncodeToString(signature))
	if got := decidePullAuthorizationForProfiles(request, certificate, host, profiles); got.failure != "" {
		t.Fatalf("optional node read denied: %s", got.failure)
	}
	if got := decidePullAuthorization(request, certificate, host); got.failure != "node_repository" {
		t.Fatal("default node grants optional runtime")
	}
	delete(profiles, "kodex-node-pull-installer")
	if got := decidePullAuthorizationForProfiles(request, certificate, host, profiles); got.failure != "node_repository" {
		t.Fatal("missing process policy granted node read")
	}
	profiles, _ = pullProfilesForRuntime("opencode")
	request.RemoteAddr = "192.0.2.2:1234"
	if got := decidePullAuthorizationForProfiles(request, certificate, host, profiles); got.failure != "node_identity" {
		t.Fatal("foreign node IP accepted")
	}
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Del("Authorization")
	if got := decidePullAuthorizationForProfiles(request, certificate, host, profiles); got.failure != "node_identity" {
		t.Fatal("missing node credential accepted")
	}
}

// TestOptionalRuntimeActualAuthorizationRetainsCredentialAndRepositoryChecks exercises the real decision path.
// TestOptionalRuntimeActualAuthorizationRetainsCredentialAndRepositoryChecks проверяет настоящий путь допуска и отказов.
func TestOptionalRuntimeActualAuthorizationRetainsCredentialAndRepositoryChecks(t *testing.T) {
	t.Parallel()
	const host = "registry.example.invalid"
	profiles, _ := pullProfilesForRuntime("opencode")
	profile := profiles["kodex-buildkit-base-pull"]
	profile.configFile = writeDockerConfig(t, map[string]string{host: base64.StdEncoding.EncodeToString([]byte("synthetic-user:synthetic-password"))})
	profiles["kodex-buildkit-base-pull"] = profile
	certificate := &x509.Certificate{Subject: pkix.Name{CommonName: "kodex-buildkit-base-pull"}}
	cases := []struct {
		method, path, user, password string
		allowed                      bool
	}{
		{http.MethodHead, "/v2/kodex/agent-runner-opencode/manifests/sha256:abc", "synthetic-user", "synthetic-password", true},
		{http.MethodGet, "/v2/kodex/agent-runner-opencode/blobs/sha256:abc", "synthetic-user", "synthetic-password", true},
		{http.MethodHead, "/v2/kodex/agent-runner-opencode/manifests/sha256:abc", "synthetic-user", "wrong-password", false},
		{http.MethodHead, "/v2/kodex/agent-runner-opencode/manifests/sha256:abc", "", "", false},
		{http.MethodPost, "/v2/kodex/agent-runner-opencode/blobs/uploads/", "synthetic-user", "synthetic-password", false},
		{http.MethodGet, "/v2/kodex/agent-runner-opencode/tags/list", "synthetic-user", "synthetic-password", false},
		{http.MethodGet, "/v2/kodex/agent-runner-opencode-other/manifests/sha256:abc", "synthetic-user", "synthetic-password", false},
		{http.MethodGet, "/v2/kodex/control-plane/manifests/sha256:abc", "synthetic-user", "synthetic-password", false},
	}
	for _, test := range cases {
		request := httptest.NewRequest(test.method, "https://"+host+test.path, nil)
		if test.user != "" {
			request.SetBasicAuth(test.user, test.password)
		}
		got := decidePullAuthorizationForProfiles(request, certificate, host, profiles)
		if (got.failure == "") != test.allowed {
			t.Fatalf("authorization mismatch: method=%s path=%s failure=%s", test.method, test.path, got.failure)
		}
	}
	defaultProfiles := pullProfiles()
	defaultProfile := defaultProfiles["kodex-buildkit-base-pull"]
	defaultProfile.configFile = profile.configFile
	defaultProfiles["kodex-buildkit-base-pull"] = defaultProfile
	request := httptest.NewRequest(http.MethodHead, "https://"+host+cases[0].path, nil)
	request.SetBasicAuth("synthetic-user", "synthetic-password")
	if decidePullAuthorizationForProfiles(request, certificate, host, defaultProfiles).failure != "profile_repository" {
		t.Fatal("default runtime permits optional repository")
	}
}

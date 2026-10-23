package restore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The operator-credential unit checks a local TLS TokenReview exchange with distinct synthetic controller/operator tokens, exact audience and service-account bindings, plus wrong-audience rejection. It bypasses New and uses the test server's trusted client, not a real Kubernetes authenticator.
// Проверка operator credential выполняет локальный TLS TokenReview exchange с разными синтетическими controller и operator tokens, точными audience и service-account bindings и отказом неверной audience. New обходится и используется доверенный клиент тестового сервера, а не реальный Kubernetes authenticator.
func TestVerifyOperatorCredentialПроверяетExactAudienceИServiceAccount(
	t *testing.T,
) {
	t.Parallel()
	const (
		// operatorToken is the synthetic credential placed in the TokenReview body, not the HTTP bearer.
		// operatorToken — синтетический credential тела TokenReview, а не HTTP bearer.
		operatorToken = "operator-projected-token"
		// serverToken is the separate synthetic controller bearer read from the test-owned token file.
		// serverToken — отдельный синтетический controller bearer из принадлежащего тесту token file.
		serverToken = "controller-kubernetes-token"
		// audience fixes the exact expected application audience in both request and fake response.
		// audience фиксирует точную прикладную аудиторию запроса и фиктивного ответа.
		audience = "urn:kodex:internal-rpc-authority-restore-controller"
	)
	server := httptest.NewTLSServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path !=
				"/apis/authentication.k8s.io/v1/tokenreviews" ||
				request.Header.Get("Authorization") != "Bearer "+serverToken {
				http.Error(response, "forbidden", http.StatusForbidden)
				return
			}
			var body tokenReviewRequest
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil ||
				body.Spec.Token != operatorToken ||
				len(body.Spec.Audiences) != 1 ||
				body.Spec.Audiences[0] != audience {
				http.Error(response, "invalid", http.StatusBadRequest)
				return
			}
			response.WriteHeader(http.StatusCreated)
			_, _ = response.Write([]byte(
				`{"apiVersion":"authentication.k8s.io/v1","kind":"TokenReview","status":{"authenticated":true,"audiences":["` +
					audience +
					`"],"user":{"username":"system:serviceaccount:kodex-system:internal-rpc-authority-restore-operator","uid":"service-account-uid","extra":{"authentication.kubernetes.io/pod-name":["internal-rpc-authority-restore-operator-test"],"authentication.kubernetes.io/pod-uid":["pod-bound-token-uid"]}},"error":""}}`,
			))
		},
	))
	defer server.Close()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(serverToken), 0o400); err != nil {
		t.Fatal(err)
	}
	store := &Store{
		config: Config{Address: server.URL, TokenFile: tokenFile},
		client: server.Client(),
	}
	credential, err := store.VerifyOperatorCredential(
		t.Context(),
		operatorToken,
		audience,
	)
	if err != nil {
		t.Fatalf("VerifyOperatorCredential() error = %v", err)
	}
	if credential.Subject !=
		"system:serviceaccount:kodex-system:internal-rpc-authority-restore-operator" ||
		credential.Audience != audience ||
		credential.TokenDigestSHA256 == "" {
		t.Fatalf("credential binding is incomplete: %#v", credential)
	}
	if _, err := store.VerifyOperatorCredential(
		t.Context(),
		operatorToken,
		"urn:kodex:wrong-audience",
	); err == nil {
		t.Fatal("wrong TokenReview audience accepted")
	}
}

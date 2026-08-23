package restore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyOperatorCredentialПроверяетExactAudienceИServiceAccount(
	t *testing.T,
) {
	t.Parallel()
	const (
		operatorToken = "operator-projected-token"
		serverToken   = "controller-kubernetes-token"
		audience      = "urn:mattercodex:internal-rpc-authority-restore-controller"
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
					`"],"user":{"username":"system:serviceaccount:mattercodex-system:internal-rpc-authority-restore-operator","uid":"service-account-uid","extra":{"authentication.kubernetes.io/pod-name":["internal-rpc-authority-restore-operator-test"],"authentication.kubernetes.io/pod-uid":["pod-bound-token-uid"]}},"error":""}}`,
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
		"system:serviceaccount:mattercodex-system:internal-rpc-authority-restore-operator" ||
		credential.Audience != audience ||
		credential.TokenDigestSHA256 == "" {
		t.Fatalf("credential binding is incomplete: %#v", credential)
	}
	if _, err := store.VerifyOperatorCredential(
		t.Context(),
		operatorToken,
		"urn:mattercodex:wrong-audience",
	); err == nil {
		t.Fatal("wrong TokenReview audience accepted")
	}
}

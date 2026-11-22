package kubernetestarget

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestObserveIdentityPinsActualClusterAndSubject proves health never infers RBAC or a synthetic subject.
// A foreign cluster stops the workflow before any subject review and never substitutes an identity.
// TestObserveIdentityPinsActualClusterAndSubject доказывает, что health не выводит RBAC либо synthetic subject.
// Чужой cluster останавливает workflow до subject review без подмены identity.
func TestObserveIdentityPinsActualClusterAndSubject(t *testing.T) {
	for _, scenario := range []string{"valid", "foreign-cluster", "anonymous", "denied", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			reviews := 0
			credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				if request.URL.Path == "/api/v1/namespaces/kube-system" && request.Method == http.MethodGet {
					uid := "629c395b-32aa-425b-814d-5cd9a9a42da6"
					if scenario == "foreign-cluster" {
						uid = "de5b4cc1-ae20-4d79-b614-a3ed79a74c1c"
					}
					_, _ = fmt.Fprintf(writer, `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"kube-system","uid":%q,"resourceVersion":"7"},"status":{"phase":"Active"}}`, uid)
					return
				}
				if request.URL.Path != "/apis/authentication.k8s.io/v1/selfsubjectreviews" || request.Method != http.MethodPost {
					t.Error("health reached a resource permission or unregistered route")
					writer.WriteHeader(http.StatusNotFound)
					return
				}
				reviews++
				if scenario == "denied" {
					writer.WriteHeader(http.StatusForbidden)
					return
				}
				if scenario == "malformed" {
					_, _ = writer.Write([]byte(`{"kind":"SelfSubjectReview","status":null}`))
					return
				}
				subject := subjectFixture()
				if scenario == "anonymous" {
					subject.Username = "system:anonymous"
				}
				_ = json.NewEncoder(writer).Encode(authenticationv1.SelfSubjectReview{
					TypeMeta: metav1.TypeMeta{APIVersion: "authentication.k8s.io/v1", Kind: "SelfSubjectReview"},
					Status:   authenticationv1.SelfSubjectReviewStatus{UserInfo: subject}})
			}))
			digest, err := credential.ObserveIdentity(context.Background())
			if scenario == "valid" {
				if err != nil || digest != subjectPin(t) || reviews != 1 || calls.Load() != 2 {
					t.Fatal("actual selected cluster and subject were not pinned")
				}
			} else if err == nil || digest != "" {
				t.Fatal("unproven health returned an authenticated subject")
			}
			if scenario == "foreign-cluster" && (reviews != 0 || calls.Load() != 1) {
				t.Fatal("foreign cluster reached subject review")
			}
		})
	}
}

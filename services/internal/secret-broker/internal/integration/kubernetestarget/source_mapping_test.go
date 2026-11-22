package kubernetestarget

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// TestSourceMappingUsesOnlySealedReachableRoute joins the actual parser and TLS client.
// A synthetic loopback kubeconfig alias is metadata only; the request carries the selected identity
// exclusively to the operator-pinned route, and a foreign ClusterUID fails the setup preflight.
// TestSourceMappingUsesOnlySealedReachableRoute соединяет actual parser и TLS client.
// Synthetic loopback kubeconfig alias — лишь metadata; selected identity передаётся только
// operator-pinned route, а чужой ClusterUID закрывает setup preflight.
func TestSourceMappingUsesOnlySealedReachableRoute(t *testing.T) {
	for _, scenario := range []string{"exact", "unspecified-source", "foreign-cluster", "unregistered-alias", "changed-ca", "redirect"} {
		t.Run(scenario, func(t *testing.T) {
			clusterUID := "629c395b-32aa-425b-814d-5cd9a9a42da6"
			handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodGet || request.URL.Path != "/api/v1/namespaces/kube-system" ||
					request.URL.RawQuery != "" || strings.Contains(request.Host, "127.0.0.1") {
					t.Error("cluster proof escaped its registered reachable namespace route")
				}
				if scenario == "redirect" {
					writer.Header().Set("Location", "https://127.0.0.1:40123/api/v1/namespaces/kube-system")
					writer.WriteHeader(http.StatusTemporaryRedirect)
					return
				}
				observed := clusterUID
				if scenario == "foreign-cluster" {
					observed = "de5b4cc1-ae20-4d79-b614-a3ed79a74c1c"
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(writer, `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"kube-system","uid":%q,"resourceVersion":"7"},"status":{"phase":"Active"}}`, observed)
			})
			selected, calls := transportFixtureWithHandler(t, false, handler)
			config, _, _ := credentialFixture(t)
			config.CurrentContext, config.Contexts[0].Name = selected.target.Context, selected.target.Context
			config.Clusters[0].Cluster.Server = "https://127.0.0.1:40123"
			if scenario == "unspecified-source" {
				config.Clusters[0].Cluster.Server = "https://0.0.0.0:40123"
			}
			config.Clusters[0].Cluster.Authority = base64.StdEncoding.EncodeToString(selected.ca)
			config.Users[0].User.Certificate, config.Users[0].User.Key = "", ""
			config.Users[0].User.Token = selected.token
			target := selected.target
			target.KubeconfigSourceOrigins = []string{config.Clusters[0].Cluster.Server}
			if scenario == "unregistered-alias" {
				target.KubeconfigSourceOrigins = []string{"https://127.0.0.1:40124"}
			}
			if scenario == "changed-ca" {
				target.CertificateAuthorityHash = strings.Repeat("f", 64)
			}
			parsed, err := ParseCredential(encodeCredential(t, config), target, time.Now().UTC())
			if scenario == "unregistered-alias" || scenario == "changed-ca" {
				if err == nil || calls.Load() != 0 {
					t.Fatal("unregistered alias or mismatched CA reached a transport")
				}
				return
			}
			if err != nil {
				t.Fatal("explicit sealed source mapping rejected the synthetic credential")
			}
			// Retain a detached snapshot even when caller-owned metadata slices are reused.
			// Сохраняется detached snapshot при повторном использовании caller metadata slices.
			target.KubeconfigSourceOrigins[0] = "https://foreign.synthetic.invalid"
			if !managedkubernetes.AllowsKubeconfigSourceOrigin(parsed.target.KubeconfigSourceOrigins, config.Clusters[0].Cluster.Server) {
				t.Fatal("caller slice mutation changed the validated source mapping")
			}
			err = parsed.VerifyClusterIdentity(context.Background())
			if (scenario == "exact" || scenario == "unspecified-source") != (err == nil) || calls.Load() != 1 {
				t.Fatal("cluster proof accepted a foreign/redirected target or retried its route")
			}
		})
	}
}

// TestClusterIdentityPreflightRejectsIncompleteNamespace keeps unknown status and parser errors closed.
// A denied selected identity is never replaced by broker or platform credentials.
// TestClusterIdentityPreflightRejectsIncompleteNamespace закрывает unknown status и parser errors.
// Denied selected identity не заменяется broker либо platform credentials.
func TestClusterIdentityPreflightRejectsIncompleteNamespace(t *testing.T) {
	valid := `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"kube-system","uid":"629c395b-32aa-425b-814d-5cd9a9a42da6","resourceVersion":"7"},"status":{"phase":"Active"}}`
	for _, body := range []string{"null", `{}`, valid + `{}`, strings.Replace(valid, `"uid":`, `"uid":"foreign","uid":`, 1),
		strings.Replace(valid, `"Active"`, `"Terminating"`, 1), strings.Replace(valid, `"kube-system"`, `"foreign"`, 1),
		strings.Replace(valid, `"resourceVersion":"7"`, `"resourceVersion":""`, 1),
		strings.Replace(valid, `"resourceVersion":"7"`, `"resourceVersion":"7","deletionTimestamp":"2026-11-14T00:00:00Z"`, 1),
		strings.Repeat(" ", maximumClusterIdentityBytes+1), "forbidden"} {
		credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			if body == "forbidden" {
				writer.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = writer.Write([]byte(body))
		}))
		if credential.VerifyClusterIdentity(context.Background()) == nil || calls.Load() != 1 {
			t.Fatal("incomplete or denied cluster proof crossed its selected-identity boundary")
		}
	}
}

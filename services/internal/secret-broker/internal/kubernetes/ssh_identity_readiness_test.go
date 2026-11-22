package kubernetes

import (
	"crypto/tls"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// TestSSHIdentityStorageReadinessUsesPrivateTLS checks the real namespace/Secret GET path with a trusted ephemeral CA; absent/deleting/foreign namespaces and forbidden or malformed responses never become readiness or trigger writes.
// TestSSHIdentityStorageReadinessUsesPrivateTLS проверяет actual namespace/Secret GET path с trusted ephemeral CA; absent/deleting/foreign namespaces и forbidden либо malformed responses не дают readiness и не вызывают writes.
func TestSSHIdentityStorageReadinessUsesPrivateTLS(t *testing.T) {
	for _, scenario := range []string{"active", "absent", "deleting", "foreign", "malformed", "namespace forbidden", "secret forbidden", "probe exists"} {
		t.Run(scenario, func(t *testing.T) {
			var reads, writes atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes.Add(1)
					w.WriteHeader(http.StatusForbidden)
					return
				}
				reads.Add(1)
				if r.TLS == nil || r.TLS.Version != tls.VersionTLS13 || r.Header.Get("Authorization") != "Bearer synthetic-readiness" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/namespaces/kodex-ssh-identities":
					if scenario == "absent" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					if scenario == "namespace forbidden" {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					if scenario == "malformed" {
						_, _ = io.WriteString(w, "{")
						return
					}
					name, deletion := "kodex-ssh-identities", ""
					if scenario == "foreign" {
						name = "foreign"
					}
					if scenario == "deleting" {
						deletion = `,"deletionTimestamp":"2026-01-01T00:00:00Z"`
					}
					_, _ = io.WriteString(w, `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"`+name+`","uid":"synthetic-namespace","resourceVersion":"1"`+deletion+`},"status":{"phase":"Active"}}`)
				case "/api/v1/namespaces/kodex-ssh-identities/secrets/ssh-identity-readiness-probe":
					status := http.StatusNotFound
					if scenario == "secret forbidden" {
						status = http.StatusForbidden
					}
					if scenario == "probe exists" {
						status = http.StatusOK
					}
					w.WriteHeader(status)
				default:
					w.WriteHeader(http.StatusForbidden)
				}
			}))
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
			server.StartTLS()
			t.Cleanup(server.Close)
			ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL, BearerToken: "synthetic-readiness", Timeout: time.Second, TLSClientConfig: rest.TLSClientConfig{CAData: ca}})
			if err != nil {
				t.Fatal("create private TLS fixture")
			}
			runtimeStore, err := New(client, "kodex-runtime")
			if err != nil {
				t.Fatal(err)
			}
			store, err := runtimeStore.SSHIdentityStore()
			if err != nil || runtimeStore.namespace != "kodex-runtime" {
				t.Fatal("purpose store changed runtime namespace")
			}
			err = store.CheckSSHIdentityStorage(t.Context())
			if (err == nil) != (scenario == "active") || writes.Load() != 0 {
				t.Fatal("readiness outcome or read-only contract changed")
			}
			wantReads := int32(1)
			if scenario == "active" || scenario == "secret forbidden" || scenario == "probe exists" {
				wantReads = 2
			}
			if reads.Load() != wantReads {
				t.Fatal("unexpected readiness API calls")
			}
		})
	}
}

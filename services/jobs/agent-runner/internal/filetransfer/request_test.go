package filetransfer

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCatalogDescriptorRequiresExactBoundSelectors(t *testing.T) {
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, ProjectRef: "prj_fixture01", LeaseRef: "lease_fixture01", LeaseFence: "fence", LeaseGeneration: 1, FileCatalog: &runtimecontract.RuntimeFileCatalog{Ref: "vfc_fixture01", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{"PROJECT"}}}
	path := "/v1/executions/lease_fixture01/artifacts/art_fixture01?purpose=PROJECT&entry_ref=vfe_fixture01&revision=1&digest=sha256:" + strings.Repeat("b", 64)
	if got, err := CatalogRequest(input, httptest.NewRequest(http.MethodGet, path, nil)); err != nil || got != "sha256:"+strings.Repeat("b", 64) {
		t.Fatalf("exact descriptor: %v", err)
	}
	for name, change := range map[string]func(*http.Request, *runtimecontract.RunnerInput){
		"foreign lease": func(r *http.Request, _ *runtimecontract.RunnerInput) {
			r.URL.Path = strings.ReplaceAll(r.URL.Path, "lease_fixture01", "lease_foreign01")
		},
		"path escape":  func(r *http.Request, _ *runtimecontract.RunnerInput) { r.URL.Path += "/../art_otherref" },
		"encoded path": func(r *http.Request, _ *runtimecontract.RunnerInput) { r.URL.RawPath = r.URL.Path },
		"absolute": func(r *http.Request, _ *runtimecontract.RunnerInput) {
			r.URL.Scheme = "https"
			r.URL.Host = "foreign.invalid"
		},
		"post":               func(r *http.Request, _ *runtimecontract.RunnerInput) { r.Method = http.MethodPost },
		"body":               func(r *http.Request, _ *runtimecontract.RunnerInput) { r.ContentLength = 1 },
		"chunked body":       func(r *http.Request, _ *runtimecontract.RunnerInput) { r.TransferEncoding = []string{"chunked"} },
		"extra selector":     func(r *http.Request, _ *runtimecontract.RunnerInput) { r.URL.RawQuery += "&token=hidden" },
		"duplicate selector": func(r *http.Request, _ *runtimecontract.RunnerInput) { r.URL.RawQuery += "&revision=1" },
		"noncanonical revision": func(r *http.Request, _ *runtimecontract.RunnerInput) {
			r.URL.RawQuery = strings.ReplaceAll(r.URL.RawQuery, "revision=1", "revision=01")
		},
		"wrong purpose": func(r *http.Request, _ *runtimecontract.RunnerInput) {
			r.URL.RawQuery = strings.ReplaceAll(r.URL.RawQuery, "purpose=PROJECT", "purpose=SKILL")
		},
		"unknown digest": func(r *http.Request, _ *runtimecontract.RunnerInput) {
			r.URL.RawQuery = strings.ReplaceAll(r.URL.RawQuery, "sha256:", "sha512:")
		},
		"no catalog": func(_ *http.Request, i *runtimecontract.RunnerInput) { i.FileCatalog = nil },
		"no fence":   func(_ *http.Request, i *runtimecontract.RunnerInput) { i.LeaseFence = "" },
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			copy := input
			change(r, &copy)
			if _, err := CatalogRequest(copy, r); err == nil {
				t.Fatal("invalid descriptor accepted")
			}
		})
	}
}

func TestFileClientSeparatesHeaderAndBodyBudget(t *testing.T) {
	transport := &http.Transport{ResponseHeaderTimeout: 30 * time.Second}
	client := NewClient(transport)
	defer client.CloseIdleConnections()
	if transport.ResponseHeaderTimeout != 30*time.Second || client.Transport.(*http.Transport) == transport || client.Timeout != TotalTimeout || client.Transport.(*http.Transport).ResponseHeaderTimeout != HeaderTimeout || HeaderTimeout <= runtimecontract.MaximumArtifactTransferDuration {
		t.Fatal("file client reused command deadline")
	}
	if client.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("file redirect accepted")
	}
}

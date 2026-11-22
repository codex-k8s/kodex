package s3store

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// deleteWireObservation records only synthetic empty-body wire semantics and signature metadata.
// deleteWireObservation фиксирует только synthetic empty-body wire semantics и signature metadata.
type deleteWireObservation struct {
	protocol                   int
	length                     int64
	transfer                   []string
	payloadHash, signedHeaders string
}

// TestExactDeletePreservesEmptySignedEntity compares actual TLS HTTP/1 and HTTP/2 requests against
// ordinary Delete: empty body, exact version, signed header names and payload hash remain equal.
// HTTP/1 keeps CL0 without chunking; HTTP/2 uses an unsigned unknown length with empty DATA/EOF.
// TestExactDeletePreservesEmptySignedEntity сравнивает actual TLS HTTP/1 и HTTP/2 requests с ordinary
// Delete: empty body, exact version, signed header names и payload hash сохраняются. HTTP/1
// сохраняет CL0 без chunking; HTTP/2 передаёт unsigned unknown length с empty DATA/EOF.
func TestExactDeletePreservesEmptySignedEntity(t *testing.T) {
	for _, h2 := range []bool{false, true} {
		name := "HTTP1"
		if h2 {
			name = "HTTP2"
		}
		t.Run(name, func(t *testing.T) {
			observations := make(chan deleteWireObservation, 2)
			endpoint := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(io.LimitReader(r.Body, 2))
				_ = r.Body.Close()
				auth := r.Header.Get("Authorization")
				_, tail, hasSigned := strings.Cut(auth, "SignedHeaders=")
				signed, _, _ := strings.Cut(tail, ",")
				if err != nil || len(body) != 0 || r.Method != http.MethodDelete || r.URL.Query().Get("versionId") != "exact-version" || !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") || !hasSigned || signed == "" || r.Header.Get("X-Amz-Content-Sha256") == "" {
					t.Error("empty exact DELETE changed signed request semantics")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				observations <- deleteWireObservation{protocol: r.ProtoMajor, length: r.ContentLength, transfer: r.TransferEncoding, payloadHash: r.Header.Get("X-Amz-Content-Sha256"), signedHeaders: signed}
				w.WriteHeader(http.StatusNoContent)
			}))
			endpoint.EnableHTTP2 = h2
			endpoint.StartTLS()
			defer endpoint.Close()
			store, err := New(t.Context(), Config{Endpoint: endpoint.URL, Region: "us-east-1", Bucket: "synthetic", AccessKeyID: "synthetic-key", SecretKey: "synthetic-secret", UsePathStyle: true, HTTPClient: endpoint.Client()})
			if err != nil {
				t.Fatal("synthetic S3 setup failed")
			}
			if err := store.Delete(t.Context(), "synthetic/signed-empty", "exact-version"); err != nil {
				t.Fatal("ordinary synthetic DELETE failed")
			}
			if err := store.DeleteOnce(t.Context(), "synthetic/signed-empty", "exact-version"); err != nil {
				t.Fatal("exact synthetic DELETE failed")
			}
			before, after := <-observations, <-observations
			wantProtocol := 1
			if h2 {
				wantProtocol = 2
			}
			if before.protocol != wantProtocol || after.protocol != wantProtocol || before.length != 0 ||
				len(before.transfer) != 0 || len(after.transfer) != 0 || before.payloadHash != after.payloadHash || before.signedHeaders != after.signedHeaders {
				t.Fatal("exact deletion changed TLS wire framing or signed empty payload")
			}
			if (!h2 && after.length != 0) || (h2 && (after.length != -1 || strings.Contains(after.signedHeaders, "content-length"))) {
				t.Fatal("empty delete entity changed a signed content length or enabled chunking")
			}
		})
	}
}

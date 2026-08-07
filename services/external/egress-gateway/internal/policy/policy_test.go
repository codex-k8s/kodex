package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

const validPolicy = `{
  "apiVersion":"mattercodex.io/v1alpha1",
  "kind":"EgressGatewayPolicy",
  "metadata":{"name":"egress-gateway","revision":"2026-08-07.1"},
  "spec":{
    "dns":{"minimumTTLSeconds":30,"maximumTTLSeconds":300,"maximumCacheEntries":32,"maximumQueries":12,"maximumCnameDepth":8,"maximumRecords":64,"maximumMessageBytes":16384,"queryTimeoutMilliseconds":2000},
    "limits":{"maximumHeaderBytes":16384,"maximumClientHelloBytes":65536,"maximumConnections":256,"maximumConnectionsPerSource":32,"headerTimeoutMilliseconds":5000,"clientHelloTimeoutMilliseconds":5000,"dialTimeoutMilliseconds":5000,"idleTimeoutMilliseconds":300000,"writeTimeoutMilliseconds":5000,"shutdownTimeoutMilliseconds":20000},
    "destinations":[{"hostname":"api.openai.com","port":443},{"hostname":"github.com","port":443}]
  }
}`

func TestLoadRequiresImmutableRevisionAndDigest(t *testing.T) {
	var document Document
	decoder := newStrictDecoder(strings.NewReader(validPolicy))
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	if err := validate(&document); err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	digestValue := sha256.Sum256(canonical)
	digest := hex.EncodeToString(digestValue[:])
	active, err := Load([]byte(validPolicy), "2026-08-07.1", digest)
	if err != nil {
		t.Fatal(err)
	}
	if !active.Allows("api.openai.com", 443) || active.Allows("example.com", 443) || active.Allows("api.openai.com", 80) {
		t.Fatal("unexpected exact allowlist result")
	}
	for _, test := range []struct{ revision, digest string }{{"other", digest}, {"2026-08-07.1", strings.Repeat("0", 64)}} {
		if _, err := Load([]byte(validPolicy), test.revision, test.digest); err == nil {
			t.Fatal("expected fail-closed revision or digest mismatch")
		}
	}
}

func TestLoadRejectsUnknownDuplicateAndPartialFields(t *testing.T) {
	tests := []string{
		strings.Replace(validPolicy, `"kind":"EgressGatewayPolicy"`, `"kind":"EgressGatewayPolicy","kind":"EgressGatewayPolicy"`, 1),
		strings.Replace(validPolicy, `"kind":"EgressGatewayPolicy"`, `"kind":"EgressGatewayPolicy","unknown":true`, 1),
		strings.Replace(validPolicy, `"revision":"2026-08-07.1"`, `"revision":""`, 1),
		strings.Replace(validPolicy, `"port":443`, `"port":80`, 1),
	}
	for _, value := range tests {
		if _, err := Load([]byte(value), "2026-08-07.1", strings.Repeat("0", 64)); err == nil {
			t.Fatal("expected invalid policy to fail closed")
		}
	}
}

func TestNormalizeHostnameRejectsIPWildcardAndMalformed(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "[::1]", "*.example.com", "example", "-bad.example", "user@example.com", "exa_mple.com", " api.openai.com"} {
		if _, err := NormalizeHostname(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
	value, err := NormalizeHostname("API.OpenAI.COM.")
	if err != nil || value != "api.openai.com" {
		t.Fatalf("unexpected normalization: %q, %v", value, err)
	}
}

func newStrictDecoder(reader *strings.Reader) *json.Decoder {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	return decoder
}

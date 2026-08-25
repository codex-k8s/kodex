package internalrpcauth

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDecodeAuthorityProofTrustDocumentAcceptsCompleteCanonicalContract(t *testing.T) {
	t.Parallel()

	document := validAuthorityProofTrustDocument(t)
	raw, err := CanonicalJSON(document)
	if err != nil {
		t.Fatalf("создать canonical trust document: %v", err)
	}
	decoded, err := DecodeAuthorityProofTrustDocument(raw)
	if err != nil {
		t.Fatalf("полный canonical trust document отклонён: %v", err)
	}
	if decoded.SourceRevision != document.SourceRevision ||
		decoded.SourceDigest != document.SourceDigest ||
		len(decoded.Keys) != 1 {
		t.Fatalf("trust document декодирован с потерей provenance: %+v", decoded)
	}
}

func TestDecodeAuthorityProofTrustDocumentRejectsSchemaAndHistoryDrift(t *testing.T) {
	t.Parallel()

	tests := map[string]func(AuthorityProofTrustDocument) []byte{
		"unknown field": func(document AuthorityProofTrustDocument) []byte {
			raw, err := json.Marshal(document)
			if err != nil {
				t.Fatalf("закодировать trust document: %v", err)
			}
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatalf("декодировать trust document: %v", err)
			}
			fields["legacy"] = true
			return mustCanonicalTrustValue(t, fields)
		},
		"missing source digest": func(document AuthorityProofTrustDocument) []byte {
			document.SourceDigest = ""
			return mustCanonicalTrustValue(t, document)
		},
		"gapped history": func(document AuthorityProofTrustDocument) []byte {
			document.History[0].Revision = 0
			return mustCanonicalTrustValue(t, document)
		},
		"multiple current keys": func(document AuthorityProofTrustDocument) []byte {
			key, err := GenerateES256Key("control-plane-proof-g8")
			if err != nil {
				t.Fatalf("создать второй тестовый ключ: %v", err)
			}
			publicJWK, err := MarshalPublicJWK(key.PublicOnly())
			if err != nil {
				t.Fatalf("закодировать второй тестовый JWK: %v", err)
			}
			second := document.Keys[0]
			second.Generation = 8
			second.JWK = json.RawMessage(publicJWK)
			document.Keys = append(document.Keys, second)
			return mustCanonicalTrustValue(t, document)
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeAuthorityProofTrustDocument(
				mutate(validAuthorityProofTrustDocument(t)),
			); err == nil {
				t.Fatal("некорректный trust document был принят")
			}
		})
	}
}

func validAuthorityProofTrustDocument(t *testing.T) AuthorityProofTrustDocument {
	t.Helper()
	key, err := GenerateES256Key("control-plane-proof-g7")
	if err != nil {
		t.Fatalf("создать тестовый ключ: %v", err)
	}
	publicJWK, err := MarshalPublicJWK(key.PublicOnly())
	if err != nil {
		t.Fatalf("закодировать тестовый JWK: %v", err)
	}
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	predecessor := TrustRevisionDigest{
		Revision: 1, DigestSHA256: strings.Repeat("a", 64),
	}
	return AuthorityProofTrustDocument{
		Version: ContractVersion, Purpose: AuthorityProofTrustPurpose,
		SourceRevision: 2, SourceDigest: strings.Repeat("b", 64),
		Predecessor: predecessor, History: []TrustRevisionDigest{predecessor},
		Keys: []AuthorityProofTrustKey{{
			Issuer:     "spiffe://kodex.local/ns/kodex-system/sa/control-plane",
			Generation: 7, Status: "CURRENT", Purpose: "AUTHORITY_PROOF",
			Audiences: []string{"urn:kodex:internal-rpc-authority-issuer:control-api-gateway"},
			NotBefore: now.Add(-time.Minute).Unix(), NotAfter: now.Add(time.Hour).Unix(),
			JWK: json.RawMessage(publicJWK),
		}},
	}
}

func mustCanonicalTrustValue(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := CanonicalJSON(value)
	if err != nil {
		t.Fatalf("создать canonical JSON: %v", err)
	}
	return raw
}

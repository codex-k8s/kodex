package main

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRegistryProxyTimeoutsBoundLargeBlobStreams pins this command's header, active-stream and idle budgets.
// Shared constructor/mTLS/redirect mechanics are tested once in httpserver/streamproxy, not copied here.
// TestRegistryProxyTimeoutsBoundLargeBlobStreams закрепляет бюджеты заголовков, активного потока и простоя этой команды.
// Общие конструкторы, mTLS и перенаправления проверяются один раз в httpserver/streamproxy, без копии здесь.
func TestRegistryProxyTimeoutsBoundLargeBlobStreams(t *testing.T) {
	t.Parallel()
	if registryHeaderTimeout != 5*time.Second || registryStreamTimeout != 15*time.Minute || registryIdleTimeout != 30*time.Second {
		t.Fatal("registry transfer budgets changed")
	}
}

// TestAllowedRegistryWriteIsClosed accepts representative staging upload paths
// and rejects foreign/nested repositories, tags, catalog and deletion.
// TestAllowedRegistryWriteIsClosed допускает примеры staging-загрузок и запрещает
// чужие/вложенные репозитории, теги, каталог и удаление.
func TestAllowedRegistryWriteIsClosed(t *testing.T) {
	t.Parallel()
	for _, request := range []struct{ method, path string }{
		{"POST", "/v2/staging/role-images/blobs/uploads/"},
		{"PATCH", "/v2/staging/role-images/blobs/uploads/id"},
		{"PUT", "/v2/staging/role-images/blobs/uploads/id"},
		{"PUT", "/v2/staging/role-images/manifests/build-1"},
		{"HEAD", "/v2/staging/role-images/blobs/sha256:abc"},
		{"PUT", "/v2/staging/readiness/manifests/userns-probe"},
	} {
		if !allowedRegistryWrite(request.method, request.path) {
			t.Fatalf("expected OCI staging operation rejected: %s %s", request.method, request.path)
		}
	}
	for _, path := range []string{
		"/v2/other/manifests/latest", "/v2/staging/role-images/tags/list", "/v2/_catalog",
		"/v2/staging/role-images/foreign/manifests/latest",
	} {
		if allowedRegistryWrite("PUT", path) {
			t.Fatalf("out-of-scope write accepted: %s", path)
		}
	}
	if allowedRegistryWrite("DELETE", "/v2/staging/role-images/manifests/digest") {
		t.Fatal("delete was accepted")
	}
}

// TestEvidenceAuthorizationIsClosedByIdentityMethodAndRepository separates
// admission writes from promotion reads and denies foreign identities or targets.
// TestEvidenceAuthorizationIsClosedByIdentityMethodAndRepository разделяет
// запись допуска и чтение продвижения, запрещая чужие идентичности и назначения.
func TestEvidenceAuthorizationIsClosedByIdentityMethodAndRepository(t *testing.T) {
	t.Parallel()
	sequence := []struct{ method, path string }{
		{http.MethodPost, "/v2/evidence/role-image-admission/blobs/uploads/"},
		{http.MethodPatch, "/v2/evidence/role-image-admission/blobs/uploads/id"},
		{http.MethodPut, "/v2/evidence/role-image-admission/blobs/uploads/id?digest=sha256:abc"},
		{http.MethodPut, "/v2/evidence/role-image-admission/manifests/artifact-id"},
		{http.MethodGet, "/v2/evidence/role-image-admission/manifests/sha256:abc"},
	}
	for _, request := range sequence {
		if !allowedEvidenceRequest("image-admission", request.method, request.path) {
			t.Fatalf("OCI evidence operation was rejected: %s %s", request.method, request.path)
		}
	}
	for _, request := range []struct{ commonName, method, path string }{
		{"kodex-image-signer", http.MethodPut, "/v2/evidence/role-image-admission/manifests/latest"},
		{"image-promotion", http.MethodPut, "/v2/evidence/role-image-admission/manifests/latest"},
		{"image-admission", http.MethodDelete, "/v2/evidence/role-image-admission/manifests/sha256:abc"},
		{"image-admission", http.MethodPut, "/v2/staging/role-images/manifests/latest"},
		{"image-admission", http.MethodPut, "/v2/evidence/role-image-admission/foreign/manifests/latest"},
	} {
		if allowedEvidenceRequest(request.commonName, request.method, request.path) {
			t.Fatalf("out-of-scope evidence authority accepted: %+v", request)
		}
	}
	if !allowedEvidenceRequest("image-promotion", http.MethodGet, "/v2/evidence/role-image-admission/manifests/sha256:abc") {
		t.Fatal("promotion exact evidence read was rejected")
	}
}

// TestEvidenceApplicationCredentialIsExact injects dummy files and requires the
// canonical case-sensitive Basic value, rejecting missing or altered credentials.
// TestEvidenceApplicationCredentialIsExact подставляет условные файлы и требует
// канонический регистрозависимый Basic, отклоняя отсутствующие и изменённые реквизиты.
func TestEvidenceApplicationCredentialIsExact(t *testing.T) {
	t.Parallel()
	reader := func(path string) ([]byte, error) {
		switch {
		case strings.HasSuffix(path, ".username"):
			return []byte("probe-user\n"), nil
		case strings.HasSuffix(path, ".password"):
			return []byte("probe-password\n"), nil
		default:
			return nil, errors.New("unexpected path")
		}
	}
	authorization := "Basic " + base64.StdEncoding.EncodeToString(
		[]byte("probe-user:probe-password"),
	)
	if !authorizedEvidenceCredentialWithReader(
		"kodex-image-registry-evidence-probe",
		authorization,
		reader,
	) {
		t.Fatal("exact evidence probe credential was rejected")
	}
	for _, invalid := range []string{
		"", "basic " + strings.TrimPrefix(authorization, "Basic "), authorization + "x",
	} {
		if authorizedEvidenceCredentialWithReader(
			"kodex-image-registry-evidence-probe",
			invalid,
			reader,
		) {
			t.Fatal("invalid evidence probe credential was accepted")
		}
	}
}

// TestEvidenceApplicationCredentialDenialChallengesOnlyMissingCredential checks
// the 401 challenge versus 403 refusal and the same non-sensitive response body.
// TestEvidenceApplicationCredentialDenialChallengesOnlyMissingCredential проверяет
// различие 401 с запросом реквизитов и 403, а также одинаковый безопасный текст ответа.
func TestEvidenceApplicationCredentialDenialChallengesOnlyMissingCredential(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		authorization    []string
		wantStatus       int
		wantAuthenticate string
	}{
		{
			name:             "missing credential receives challenge",
			wantStatus:       http.StatusUnauthorized,
			wantAuthenticate: evidenceBasicAuthenticationChallenge,
		},
		{
			name:          "wrong credential remains forbidden",
			authorization: []string{"Basic invalid"},
			wantStatus:    http.StatusForbidden,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			writeEvidenceCredentialDenial(recorder, test.authorization)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if got := recorder.Header().Get("WWW-Authenticate"); got != test.wantAuthenticate {
				t.Fatalf("WWW-Authenticate = %q, want %q", got, test.wantAuthenticate)
			}
			if body := recorder.Body.String(); body != "request forbidden\n" {
				t.Fatalf("response body exposes unexpected detail: %q", body)
			}
		})
	}
}

// TestNormalizeMethodHasBoundedCardinality maps arbitrary/newline input to OTHER
// while preserving GET, preventing uncontrolled diagnostic method values.
// TestNormalizeMethodHasBoundedCardinality заменяет произвольный ввод/перенос строки
// на OTHER, сохраняя GET и не допуская неограниченных диагностических методов.
func TestNormalizeMethodHasBoundedCardinality(t *testing.T) {
	t.Parallel()
	if normalizeMethod("UNBOUNDED\nINPUT") != "OTHER" {
		t.Fatal("unbounded method was preserved in diagnostics")
	}
	if normalizeMethod(http.MethodGet) != http.MethodGet {
		t.Fatal("known method was not preserved")
	}
}

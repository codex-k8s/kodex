package platformworkergrant

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/google/uuid"
)

func TestInstanceGrantRotationAndSameSecondRestart(t *testing.T) {
	key, err := internalrpcauth.GenerateES256Key("runtime-controller-platform-worker-g3")
	if err != nil {
		t.Fatal(err)
	}
	configuration := config{WorkloadID: "runtime-controller", InstanceID: uuid.NewString(), OutputFile: filepath.Join(t.TempDir(), "grant.jws")}
	now := time.Now().UTC().Truncate(time.Second)
	if err := rotate(configuration, key, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	first := readTestClaims(t, configuration.OutputFile, key)
	if first.Version != 2 || first.InstanceID != configuration.InstanceID || first.CredentialGeneration != 3 {
		t.Fatal("instance binding differs")
	}
	if err := rotate(configuration, key, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	if repeated := readTestClaims(t, configuration.OutputFile, key); repeated != first {
		t.Fatal("same-second restart changed envelope")
	}
	if err := rotate(configuration, key, func() time.Time { return now.Add(time.Minute) }); err != nil {
		t.Fatal(err)
	}
	next := readTestClaims(t, configuration.OutputFile, key)
	if next.InstanceID != first.InstanceID || next.Revision <= first.Revision || next.JTI == first.JTI {
		t.Fatal("instance refresh differs")
	}
	configuration.InstanceID = uuid.NewString()
	if readCurrent(configuration, key, now.Add(time.Minute)) == nil {
		t.Fatal("foreign Pod grant accepted")
	}
}

func TestGrantReadinessChecksActualExpiryBetweenRefreshTicks(t *testing.T) {
	key, err := internalrpcauth.GenerateES256Key("email-bridge-platform-worker-g1")
	if err != nil {
		t.Fatal(err)
	}
	configuration := config{WorkloadID: "email-bridge", InstanceID: uuid.NewString(), OutputFile: filepath.Join(t.TempDir(), "grant.jws")}
	now := time.Now().UTC().Truncate(time.Second)
	if err := rotate(configuration, key, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	readiness := serviceruntime.NewReadiness()
	readiness.Set(true, "grant_refresh_deferred")
	stamp := now.Add(grantTTL - time.Second)
	server := technicalServer(":0", readiness, func() bool { return readCurrent(configuration, key, stamp) == nil })
	probe := func(path string, expected int) {
		t.Helper()
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != expected {
			t.Fatalf("probe %s: %d", path, response.Code)
		}
	}
	probe("/readyz", http.StatusNoContent)
	stamp = now.Add(grantTTL)
	probe("/readyz", http.StatusServiceUnavailable)
	probe("/livez", http.StatusNoContent)
	stamp = now.Add(time.Minute)
	if err := writeAtomic(configuration.OutputFile, []byte("corrupt-synthetic-grant")); err != nil {
		t.Fatal(err)
	}
	probe("/readyz", http.StatusServiceUnavailable)
	probe("/healthz", http.StatusNoContent)
}

func TestGrantInstanceValidationAndLegacyDefault(t *testing.T) {
	for _, value := range []string{uuid.Nil.String(), "caller-controlled", "ABCDEF00-0000-4000-8000-000000000001"} {
		if validInstanceID(value) {
			t.Fatal("noncanonical or nil instance accepted")
		}
	}
	if !validInstanceID("") || !validInstanceID(uuid.NewString()) {
		t.Fatal("supported instance rejected")
	}
}

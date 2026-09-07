package authorityproof

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/google/uuid"
)

func TestVerifyWorkerGrantInstanceCompatibility(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	key, err := internalrpcauth.GenerateES256Key("runtime-controller-platform-worker-g1")
	if err != nil {
		t.Fatal(err)
	}
	producer := testProducer("runtime-controller", "PLATFORM_WORKER_GRANT")
	service := &Service{workerKeys: map[string]internalrpcauth.ES256Key{"runtime-controller": key.PublicOnly()}, now: func() time.Time { return now }}
	base := workerGrantClaims{Version: 2, InstanceID: uuid.NewString(), Issuer: producer.ApplicationCredentialIssuer,
		Audience: producer.ApplicationCredentialAudience, Subject: "kodex-system-subject", CallerSPIFFEID: producer.CallerSPIFFEID,
		WorkloadID: "runtime-controller", OrganizationID: "kodex-installation", Revision: uint64(now.Unix()),
		CredentialGeneration: 1, AuthorityABIVersion: internalrpcauth.AuthorityABIVersion, JTI: uuid.NewString(),
		IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: now.Add(workerGrantTTL).Unix()}
	for _, test := range []struct {
		name    string
		change  func(*workerGrantClaims)
		allowed bool
	}{
		{"v2", func(*workerGrantClaims) {}, true},
		{"legacy", func(c *workerGrantClaims) { c.Version = 1; c.InstanceID = ""; c.Revision = 7 }, true},
		{"missing instance", func(c *workerGrantClaims) { c.InstanceID = "" }, false},
		{"nil instance", func(c *workerGrantClaims) { c.InstanceID = uuid.Nil.String() }, false},
		{"malformed instance", func(c *workerGrantClaims) { c.InstanceID = "caller-instance" }, false},
		{"v1 instance injection", func(c *workerGrantClaims) { c.Version = 1 }, false},
		{"unknown format", func(c *workerGrantClaims) { c.Version = 3 }, false},
		{"revision time mismatch", func(c *workerGrantClaims) { c.Revision++ }, false},
		{"expired", func(c *workerGrantClaims) { c.ExpiresAt = now.Add(-time.Minute).Unix() }, false},
		{"generation mismatch", func(c *workerGrantClaims) { c.CredentialGeneration++ }, false},
		{"foreign workload", func(c *workerGrantClaims) { c.WorkloadID = "email-bridge" }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := base
			test.change(&claims)
			compact, err := internalrpcauth.SignCanonicalJSON(claims, key, internalrpcauth.ProtectedHeaderExpectation{Type: workerGrantType, KeyID: key.KeyID})
			if err != nil {
				t.Fatal(err)
			}
			actual, err := service.verifyWorkerGrant(compact, producer)
			if (err == nil) != test.allowed {
				t.Fatalf("grant acceptance: allowed=%v err=%v", test.allowed, err)
			}
			if err == nil && actual.InstanceID != claims.InstanceID {
				t.Fatal("signed instance was lost")
			}
		})
	}
}

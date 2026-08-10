package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestAuthorityReceiptRejectsCrossExecutionReplay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	execution := entity.Execution{ID: uuid.NewString(), Version: 7, Fence: 11}
	if err := recordAuthorityReceipt(ctx, client, "runtime-test", execution, "snapshot", strings.Repeat("a", 64), nil); err != nil {
		t.Fatalf("record exact receipt: %v", err)
	}
	rejoined, err := rejoinAuthorityReceipt(
		ctx, client, "runtime-test", execution, "snapshot", strings.Repeat("a", 64),
	)
	if err != nil || !rejoined {
		t.Fatalf("rejoin exact receipt: rejoined=%v err=%v", rejoined, err)
	}
	if _, err := rejoinAuthorityReceipt(
		ctx, client, "runtime-test", execution, "snapshot", strings.Repeat("b", 64),
	); err == nil {
		t.Fatal("changed request digest reused an execution receipt")
	}
	other := execution
	other.ID = uuid.NewString()
	rejoined, err = rejoinAuthorityReceipt(
		ctx, client, "runtime-test", other, "snapshot", strings.Repeat("a", 64),
	)
	if err != nil || rejoined {
		t.Fatalf("another execution observed a foreign receipt: rejoined=%v err=%v", rejoined, err)
	}
}

func TestS3CredentialBackendRequiresExactProfile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile, backend string
		ok               bool
	}{
		{profile: "production", backend: "vault-aws", ok: true},
		{profile: directProductionPrototypeProfile, backend: "direct-production-s3-sts", ok: true},
		{profile: directProductionPrototypeProfile, backend: "vault-aws"},
		{profile: "production", backend: "direct-production-s3-sts"},
		{profile: directProductionPrototypeProfile, backend: "static"},
	}
	for _, test := range tests {
		_, err := selectS3CredentialBackend(test.profile, test.backend)
		if (err == nil) != test.ok {
			t.Fatalf("selectS3CredentialBackend(%q, %q) error=%v, want ok=%v", test.profile, test.backend, err, test.ok)
		}
	}
}

func TestExactS3PolicyBindsRestoreVersionAndForbidsBroadActions(t *testing.T) {
	t.Setenv("RUNTIME_S3_BUCKET", "runtime-bucket")
	t.Setenv("RUNTIME_S3_REGION", "region")
	t.Setenv("RUNTIME_S3_KMS_KEY_ARN", "arn:aws:kms:region:account:key/key-id")
	execution := entity.Execution{
		ID: uuid.NewString(), OrganizationID: uuid.NewString(), ProjectID: uuid.NewString(),
		SessionID: uuid.NewString(), RestoreSourceExecutionID: uuid.NewString(),
	}
	objectKey := strings.Join([]string{"runtime", execution.OrganizationID, execution.ProjectID,
		execution.SessionID, execution.RestoreSourceExecutionID, "archive.tar.gz"}, "/")
	execution.RestoreSourceArchiveReference = "s3://runtime-bucket/" + objectKey + "?versionId=exact-version"
	execution.RestoreSourceArchiveObjectKey = objectKey
	execution.RestoreSourceArchiveVersionID = "exact-version"
	execution.RestoreSourceArchiveKMSKeyARN = "arn:aws:kms:region:account:key/key-id"
	execution.RestoreSourceArchiveObjectLockMode = "COMPLIANCE"
	execution.RestoreSourceArchiveRetainUntil = time.Now().UTC().Add(time.Hour)
	execution.RestoreSourceProofReference = "proof://restore"
	execution.RestoreSourceProofSHA256 = strings.Repeat("a", 64)
	execution.RestoreSourceProvenanceSHA256 = strings.Repeat("b", 64)
	policy, sourceExecutionID, err := exactS3Policy(execution, "restore")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if sourceExecutionID != execution.RestoreSourceExecutionID ||
		!strings.Contains(text, `"s3:VersionId":"exact-version"`) ||
		!strings.Contains(text, "/archive.tar.gz") ||
		!strings.Contains(text, "/restore-proof.json") ||
		strings.Contains(text, `"Action":"s3:*","Effect":"Allow"`) ||
		!strings.Contains(text, `"s3:ListBucket"`) || !strings.Contains(text, `"s3:DeleteObject"`) {
		t.Fatal("restore policy is not exact and fail-closed")
	}
}

func TestExactS3PolicyUsesCurrentArchiveForInitialRestoreProof(t *testing.T) {
	t.Setenv("RUNTIME_S3_BUCKET", "runtime-bucket")
	t.Setenv("RUNTIME_S3_REGION", "region")
	t.Setenv("RUNTIME_S3_KMS_KEY_ARN", "arn:aws:kms:region:account:key/key-id")
	execution := entity.Execution{
		ID: uuid.NewString(), OrganizationID: uuid.NewString(), ProjectID: uuid.NewString(),
		SessionID: uuid.NewString(), ArchiveReference: "s3://runtime-bucket/runtime/archive.tar.gz?versionId=current-version",
	}
	policy, sourceExecutionID, err := exactS3Policy(execution, "restore")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if sourceExecutionID != execution.ID || !strings.Contains(string(raw), `"s3:VersionId":"current-version"`) {
		t.Fatal("initial restore proof is not bound to the current archive version")
	}
}

func TestS3CredentialProofRejectsAlreadyExistsTupleMismatch(t *testing.T) {
	execution := entity.Execution{
		ID: uuid.NewString(), OrganizationID: uuid.NewString(), ProjectID: uuid.NewString(), SessionID: uuid.NewString(),
	}
	action, policySHA256, workloadTicket := "archive", strings.Repeat("a", 64), "archive-ticket"
	workloadTicketDigest := sha256.Sum256([]byte(workloadTicket))
	immutable := true
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: s3CredentialSecretName(execution.ID, action), UID: types.UID("uid-1"), ResourceVersion: "17",
		Annotations: map[string]string{
			"runtime.mattercodex.dev/execution-id": execution.ID, "runtime.mattercodex.dev/organization-id": execution.OrganizationID,
			"runtime.mattercodex.dev/project-id": execution.ProjectID, "runtime.mattercodex.dev/session-id": execution.SessionID,
			"runtime.mattercodex.dev/source-execution-id": execution.ID, "runtime.mattercodex.dev/action": action,
			"runtime.mattercodex.dev/workload-ticket":        workloadTicket,
			"runtime.mattercodex.dev/workload-ticket-sha256": hex.EncodeToString(workloadTicketDigest[:]),
			"runtime.mattercodex.dev/inline-policy-sha256":   policySHA256,
			"runtime.mattercodex.dev/readback-sha256":        strings.Repeat("b", 64),
			"runtime.mattercodex.dev/expires-at":             time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339),
		},
	}, Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{
		"access-key-id": []byte("key"), "secret-access-key": []byte("secret"), "session-token": []byte("token"),
	}}
	proof, err := s3CredentialProof(execution, action, execution.ID, policySHA256, workloadTicket, secret)
	if err != nil || proof["s3_secret_uid"] != "uid-1" || proof["s3_secret_resource_version"] != "17" {
		t.Fatalf("exact S3 Secret proof was rejected: proof=%v err=%v", proof, err)
	}
	tampered := secret.DeepCopy()
	tampered.Annotations["runtime.mattercodex.dev/project-id"] = uuid.NewString()
	if _, err := s3CredentialProof(execution, action, execution.ID, policySHA256, workloadTicket, tampered); err == nil {
		t.Fatal("cross-project AlreadyExists Secret was accepted")
	}
}

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/google/uuid"
)

func TestExactS3PolicyBindsRestoreVersionAndForbidsBroadActions(t *testing.T) {
	t.Setenv("RUNTIME_S3_BUCKET", "runtime-bucket")
	t.Setenv("RUNTIME_S3_KMS_KEY_ARN", "arn:aws:kms:region:account:key/key-id")
	execution := entity.Execution{
		ID: uuid.NewString(), OrganizationID: uuid.NewString(), ProjectID: uuid.NewString(),
		SessionID: uuid.NewString(), RestoreSourceExecutionID: uuid.NewString(),
		RestoreSourceArchiveReference: "s3://runtime-bucket/runtime/archive.tar.gz?versionId=exact-version",
	}
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

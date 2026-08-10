// Package s3policy строит закрытые execution-scoped AWS/MinIO policy.
package s3policy

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/repository/s3credential"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
)

type Dialect string

const (
	DialectAWS   Dialect = "aws"
	DialectMinIO Dialect = "minio"
)

type Config struct {
	Bucket, Region, KMSKeyARN, KMSKeyID string
}

type Result struct {
	Raw               []byte
	SourceExecutionID string
}

func Build(execution entity.Execution, action s3credential.Action, config Config, dialect Dialect, now time.Time) (Result, error) {
	if !action.Valid() || config.Bucket == "" || config.Region == "" || config.KMSKeyARN == "" ||
		!strings.HasPrefix(config.KMSKeyARN, "arn:") || (dialect != DialectAWS && dialect != DialectMinIO) {
		return Result{}, errors.New("runtime S3 policy configuration is invalid")
	}
	if dialect == DialectMinIO && (config.KMSKeyID == "" || strings.ContainsAny(config.KMSKeyID, "\x00\r\n/*?")) {
		return Result{}, errors.New("runtime MinIO KMS key is invalid")
	}
	sourceExecutionID := execution.ID
	archiveObject := strings.Join([]string{"runtime", execution.OrganizationID, execution.ProjectID, execution.SessionID, execution.ID, "archive.tar.gz"}, "/")
	archiveARN := "arn:aws:s3:::" + config.Bucket + "/" + archiveObject
	writeARN := archiveARN
	bucketActions := []string{"s3:GetBucketVersioning", "s3:GetObjectLockConfiguration", "s3:GetEncryptionConfiguration", "s3:GetBucketPublicAccessBlock"}
	if dialect == DialectMinIO {
		bucketActions = []string{"s3:GetBucketVersioning", "s3:GetBucketObjectLockConfiguration", "s3:GetBucketEncryption", "s3:GetBucketPolicyStatus"}
	}
	statements := []any{map[string]any{"Effect": "Allow", "Action": bucketActions, "Resource": "arn:aws:s3:::" + config.Bucket}}
	if action == s3credential.ActionRestore {
		var archiveReference string
		sourceExecutionID, archiveReference = RestoreArchiveSource(execution)
		if sourceExecutionID == "" || archiveReference == "" {
			return Result{}, errors.New("runtime restore source is invalid")
		}
		archiveObject = strings.Join([]string{"runtime", execution.OrganizationID, execution.ProjectID, execution.SessionID, sourceExecutionID, "archive.tar.gz"}, "/")
		proofObject := strings.Join([]string{"runtime-restore-proof", execution.OrganizationID, execution.ProjectID, execution.SessionID, sourceExecutionID, "restore-proof.json"}, "/")
		archiveARN, writeARN = "arn:aws:s3:::"+config.Bucket+"/"+archiveObject, "arn:aws:s3:::"+config.Bucket+"/"+proofObject
		versionID := ExactVersionID(archiveReference)
		parsedReference, parseErr := url.Parse(archiveReference)
		if parseErr != nil || versionID == "" {
			return Result{}, errors.New("runtime restore archive version is invalid")
		}
		if execution.RestoreSourceExecutionID != "" &&
			(parsedReference.Scheme != "s3" || parsedReference.Host != config.Bucket ||
				strings.TrimPrefix(parsedReference.Path, "/") != archiveObject ||
				execution.RestoreSourceArchiveObjectKey != archiveObject ||
				execution.RestoreSourceArchiveVersionID != versionID ||
				execution.RestoreSourceArchiveKMSKeyARN != config.KMSKeyARN ||
				execution.RestoreSourceArchiveObjectLockMode != "COMPLIANCE" ||
				!execution.RestoreSourceArchiveRetainUntil.After(now.UTC()) ||
				execution.RestoreSourceProofReference == "" ||
				!validSHA256(execution.RestoreSourceProofSHA256) ||
				!validSHA256(execution.RestoreSourceProvenanceSHA256)) {
			return Result{}, errors.New("runtime restore archive authority is invalid")
		}
		statements = append(statements,
			map[string]any{"Effect": "Allow", "Action": []string{"s3:GetObjectVersion", "s3:GetObjectRetention"}, "Resource": archiveARN,
				"Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "true"}, "StringEquals": map[string]string{"s3:VersionId": versionID}}},
			map[string]any{"Effect": "Allow", "Action": []string{"s3:GetObject", "s3:GetObjectVersion", "s3:GetObjectRetention"}, "Resource": writeARN,
				"Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "true"}}},
		)
	} else {
		statements = append(statements, map[string]any{"Effect": "Allow", "Action": []string{"s3:GetObject", "s3:GetObjectVersion", "s3:GetObjectRetention"}, "Resource": archiveARN,
			"Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "true"}}})
	}
	writeConditions := map[string]string{"s3:x-amz-server-side-encryption": "aws:kms", "s3:object-lock-mode": "COMPLIANCE"}
	if dialect == DialectMinIO {
		writeConditions["s3:x-amz-server-side-encryption-aws-kms-key-id"] = config.KMSKeyID
	}
	statements = append(statements,
		map[string]any{"Effect": "Allow", "Action": []string{"s3:PutObject", "s3:PutObjectRetention"}, "Resource": writeARN,
			"Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "true"}, "StringEquals": writeConditions}},
	)
	if dialect == DialectAWS {
		statements = append(statements, map[string]any{"Effect": "Allow", "Action": []string{"kms:Decrypt", "kms:Encrypt", "kms:GenerateDataKey"}, "Resource": config.KMSKeyARN,
			"Condition": map[string]any{"StringEquals": map[string]any{
				"kms:ViaService": "s3." + config.Region + ".amazonaws.com", "kms:EncryptionContext:aws:s3:arn": []string{archiveARN, writeARN},
			}}})
	}
	statements = append(statements,
		map[string]any{"Effect": "Deny", "Action": []string{"s3:PutObject", "s3:PutObjectRetention"}, "Resource": writeARN,
			"Condition": map[string]any{"NumericLessThan": map[string]string{"s3:object-lock-remaining-retention-days": "90"}}},
		map[string]any{"Effect": "Deny", "Action": []string{"s3:ListBucket", "s3:DeleteObject", "s3:DeleteObjectVersion", "s3:BypassGovernanceRetention"}, "Resource": []string{"arn:aws:s3:::" + config.Bucket, "arn:aws:s3:::" + config.Bucket + "/*"}},
		map[string]any{"Effect": "Deny", "Action": "s3:*", "Resource": []string{"arn:aws:s3:::" + config.Bucket, "arn:aws:s3:::" + config.Bucket + "/*"}, "Condition": map[string]any{"Bool": map[string]string{"aws:SecureTransport": "false"}}},
	)
	raw, err := json.Marshal(map[string]any{"Version": "2012-10-17", "Statement": statements})
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		return Result{}, errors.New("encode exact runtime S3 policy")
	}
	return Result{Raw: raw, SourceExecutionID: sourceExecutionID}, nil
}

func RestoreArchiveSource(execution entity.Execution) (string, string) {
	if execution.RestoreSourceExecutionID != "" || execution.RestoreSourceArchiveReference != "" {
		return execution.RestoreSourceExecutionID, execution.RestoreSourceArchiveReference
	}
	return execution.ID, execution.ArchiveReference
}

func ExactVersionID(reference string) string {
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "s3" || parsed.Query().Get("versionId") == "" || len(parsed.Query()) != 1 {
		return ""
	}
	return parsed.Query().Get("versionId")
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

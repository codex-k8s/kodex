package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/archive"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

func TestFailureResultPreservesClosedArchiveStageWithoutPrivateCause(t *testing.T) {
	t.Parallel()
	for _, stage := range []model.FailureStage{model.FailureStageSourceIdentity, model.FailureStageSourceDigest,
		model.FailureStageObjectWrite, model.FailureStageObjectReadback, model.FailureStageUnknown} {
		t.Run(string(stage), func(t *testing.T) {
			workspace := t.TempDir()
			body := []byte("synthetic session\n")
			hash := sha256.Sum256(body)
			task := model.Task{SourceRelativePath: "session.jsonl", SourceSizeBytes: int64(len(body)), SourceSHA256: hex.EncodeToString(hash[:]), TargetObjectKey: "fixture/archive.tar"}
			if err := os.WriteFile(filepath.Join(workspace, task.SourceRelativePath), body, 0o600); err != nil {
				t.Fatal("create synthetic source")
			}
			store := &failureStageStore{Store: objectstoragetest.New(), stage: stage}
			switch stage {
			case model.FailureStageSourceIdentity:
				task.SourceSizeBytes++
			case model.FailureStageSourceDigest:
				task.SourceSHA256 = strings.Repeat("0", 64)
			}
			_, cause := archive.Snapshot(t.Context(), store, workspace, task)
			if stage == model.FailureStageUnknown {
				cause = errors.New("SENTINEL_PRIVATE: session source file digest mismatch")
			}
			if cause == nil {
				t.Fatal("fixture did not fail")
			}
			resultPath := filepath.Join(t.TempDir(), "result.json")
			if err := writeFailure(resultPath, "SESSION_ARCHIVE_WORKER_FAILED", fmt.Errorf("SENTINEL_PRIVATE: %w", cause)); err == nil {
				t.Fatal("failure changed the worker outcome")
			}
			raw, err := os.ReadFile(resultPath)
			var result model.Result
			if err != nil || json.Unmarshal(raw, &result) != nil || result.Success || result.SafeErrorCode != "SESSION_ARCHIVE_WORKER_FAILED" || result.FailureStage != stage || strings.Contains(string(raw), "SENTINEL_PRIVATE") || len(raw) > 4096 {
				t.Fatal("worker failure result lost its stage or exposed a private cause")
			}
		})
	}
}

type failureStageStore struct {
	*objectstoragetest.Store
	stage model.FailureStage
}

func (store *failureStageStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	if store.stage == model.FailureStageObjectWrite {
		return objectstorage.Receipt{}, errors.New("SENTINEL_PRIVATE_SDK")
	}
	return store.Store.Put(ctx, input)
}

func (store *failureStageStore) Get(ctx context.Context, key, version string) (objectstorage.Object, error) {
	if store.stage == model.FailureStageObjectReadback {
		return objectstorage.Object{}, errors.New("SENTINEL_PRIVATE_SDK")
	}
	return store.Store.Get(ctx, key, version)
}

func TestValidateConfigAllowsOnlyExplicitExactLocalObjectStorage(t *testing.T) {
	t.Parallel()

	base := config{
		Environment: "staging", TaskFile: "/task.json", Workspace: "/workspace", ResultFile: "/result.json",
		Endpoint: "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333", Region: "us-east-1", Bucket: "archives",
		AccessKeyFile: "/access-key", SecretKeyFile: "/secret-key", UsePathStyle: true, Timeout: time.Minute,
	}
	if err := validateConfig(base); err == nil {
		t.Fatal("worker accepted plaintext object storage without the local exception")
	}
	base.AllowInsecureLocal = true
	if err := validateConfig(base); err != nil {
		t.Fatalf("worker rejected the exact local object storage exception: %v", err)
	}
	base.Endpoint = "http://other.kodex-system.svc.cluster.local:8333"
	if err := validateConfig(base); err == nil {
		t.Fatal("worker accepted the local plaintext exception for another host")
	}
	base.Endpoint = "https://s3.example.test"
	base.AllowInsecureLocal = false
	if err := validateConfig(base); err != nil {
		t.Fatalf("worker rejected TLS object storage: %v", err)
	}
}

func TestValidateConfigRejectsLocalExceptionInProduction(t *testing.T) {
	t.Parallel()

	value := config{
		Environment: "production", TaskFile: "/task.json", Workspace: "/workspace", ResultFile: "/result.json",
		Endpoint: "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333", Region: "us-east-1", Bucket: "archives",
		AccessKeyFile: "/access-key", SecretKeyFile: "/secret-key", AllowInsecureLocal: true, Timeout: time.Minute,
	}
	if err := validateConfig(value); err == nil {
		t.Fatal("worker accepted the local plaintext exception in production")
	}
}

package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/s3store"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/archive"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

type config struct {
	Environment        string        `env:"DEPLOYMENT_ENVIRONMENT"`
	TaskFile           string        `env:"SESSION_ARCHIVE_TASK_FILE"`
	Workspace          string        `env:"SESSION_ARCHIVE_WORKSPACE"`
	ResultFile         string        `env:"SESSION_ARCHIVE_RESULT_FILE"`
	Endpoint           string        `env:"SESSION_ARCHIVE_OBJECT_STORAGE_ENDPOINT"`
	Region             string        `env:"SESSION_ARCHIVE_OBJECT_STORAGE_REGION"`
	Bucket             string        `env:"SESSION_ARCHIVE_OBJECT_STORAGE_BUCKET"`
	AccessKeyFile      string        `env:"SESSION_ARCHIVE_OBJECT_STORAGE_ACCESS_KEY_FILE"`
	SecretKeyFile      string        `env:"SESSION_ARCHIVE_OBJECT_STORAGE_SECRET_KEY_FILE"`
	UsePathStyle       bool          `env:"SESSION_ARCHIVE_OBJECT_STORAGE_USE_PATH_STYLE"`
	AllowInsecureLocal bool          `env:"SESSION_ARCHIVE_OBJECT_STORAGE_ALLOW_INSECURE_LOCAL"`
	Timeout            time.Duration `env:"SESSION_ARCHIVE_WORKER_TIMEOUT"`
}

func Run(lifecycle context.Context) error {
	configuration := config{TaskFile: "/var/run/config/kodex/session-archive/task.json", Workspace: "/workspace",
		ResultFile: "/dev/termination-log", Region: "us-east-1", Bucket: "kodex-session-archives", UsePathStyle: true,
		AccessKeyFile: "/var/run/secrets/kodex/session-archive/object-storage/access-key",
		SecretKeyFile: "/var/run/secrets/kodex/session-archive/object-storage/secret-key", Timeout: 8 * time.Minute}
	if env.ParseWithOptions(&configuration, env.Options{}) != nil || validateConfig(configuration) != nil {
		return errors.New("session archive worker configuration is invalid")
	}
	task, err := model.DecodeFile(configuration.TaskFile)
	if err != nil {
		return writeFailure(configuration.ResultFile, "SESSION_ARCHIVE_SOURCE_INVALID", err)
	}
	accessKey, err := readSecret(configuration.AccessKeyFile)
	if err != nil {
		return writeFailure(configuration.ResultFile, "SESSION_ARCHIVE_OBJECT_WRITE_FAILED", err)
	}
	secretKey, err := readSecret(configuration.SecretKeyFile)
	if err != nil {
		return writeFailure(configuration.ResultFile, "SESSION_ARCHIVE_OBJECT_WRITE_FAILED", err)
	}
	ctx, cancel := context.WithTimeout(lifecycle, configuration.Timeout)
	defer cancel()
	store, err := s3store.New(ctx, s3store.Config{Endpoint: configuration.Endpoint, Region: configuration.Region,
		Bucket: configuration.Bucket, AccessKeyID: accessKey, SecretKey: secretKey, UsePathStyle: configuration.UsePathStyle})
	if err != nil || store.Check(ctx) != nil {
		return writeFailure(configuration.ResultFile, "SESSION_ARCHIVE_OBJECT_WRITE_FAILED", errors.New("object storage is unavailable"))
	}
	var result model.Result
	switch task.Kind {
	case "SNAPSHOT":
		result, err = archive.Snapshot(ctx, store, configuration.Workspace, task)
	case "RESTORE":
		result, err = archive.Restore(ctx, store, configuration.Workspace, task)
	case "DELETE_OBJECT":
		result, err = archive.Delete(ctx, store, task)
	default:
		err = errors.New("worker task kind is unsupported")
	}
	if err != nil {
		code := "SESSION_ARCHIVE_WORKER_FAILED"
		if task.Kind == "RESTORE" {
			code = "SESSION_ARCHIVE_RESTORE_INVALID"
		}
		if task.Kind == "DELETE_OBJECT" {
			code = "SESSION_ARCHIVE_OBJECT_DELETE_FAILED"
		}
		return writeFailure(configuration.ResultFile, code, err)
	}
	return writeResult(configuration.ResultFile, result)
}

func validateConfig(value config) error {
	for _, path := range []string{value.TaskFile, value.Workspace, value.ResultFile, value.AccessKeyFile, value.SecretKeyFile} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("worker path is invalid")
		}
	}
	if value.Environment != "local" && value.Environment != "staging" && value.Environment != "production" {
		return errors.New("worker environment is invalid")
	}
	if value.Endpoint == "" || value.Region == "" || value.Bucket == "" || value.Timeout < time.Minute || value.Timeout > 15*time.Minute ||
		!validObjectStorageBoundary(value) {
		return errors.New("worker lifecycle is invalid")
	}
	return nil
}

func validObjectStorageBoundary(value config) bool {
	endpoint, err := url.Parse(value.Endpoint)
	if err != nil || endpoint == nil || endpoint.Host == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" && endpoint.Path != "/" {
		return false
	}
	localInsecure := value.AllowInsecureLocal && value.Environment == "staging" && endpoint.Scheme == "http" &&
		endpoint.Hostname() == "seaweedfs-s3.kodex-system.svc.cluster.local" && endpoint.Port() == "8333"
	return endpoint.Scheme == "https" || localInsecure
}

func readSecret(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 16<<10 {
		return "", errors.New("read object storage credential")
	}
	value := strings.TrimSpace(string(raw))
	for index := range raw {
		raw[index] = 0
	}
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("object storage credential is invalid")
	}
	return value, nil
}

func writeFailure(path, code string, cause error) error {
	writeErr := writeResult(path, model.Result{Success: false, SafeErrorCode: code})
	return errors.Join(fmt.Errorf("session archive worker failed: %w", cause), writeErr)
}

func writeResult(path string, result model.Result) error {
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > 4<<10 {
		return errors.New("encode session archive worker result")
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return errors.New("write session archive worker result")
	}
	return nil
}

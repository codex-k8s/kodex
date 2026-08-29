package model

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	FormatVersion            = 1
	MaximumTaskBytes         = 64 << 10
	MaximumSourceBytes int64 = 64 << 20
	MaximumObjectBytes int64 = 68 << 20
)

type ArchiveBinding struct {
	ArchiveRef         string `json:"archive_ref"`
	FormatVersion      uint32 `json:"format_version"`
	ObjectKey          string `json:"object_key"`
	ObjectVersion      string `json:"object_version"`
	ObjectETag         string `json:"object_etag"`
	ObjectDigest       string `json:"object_digest"`
	ObjectSizeBytes    int64  `json:"object_size_bytes"`
	SourceRelativePath string `json:"source_relative_path"`
	SourceSHA256       string `json:"source_sha256"`
	SourceSizeBytes    int64  `json:"source_size_bytes"`
}

type Task struct {
	TaskRef, Kind, OrganizationRef, ProjectRef, SessionRef string
	ProviderAccountRef, RuntimeRevisionRef                 string
	RuntimeRevisionVersion, ContentGeneration              int64
	RuntimeRevisionDigest, CodexSessionID                  string
	PVCName, InputDigest                                   string
	SourceRelativePath, SourceSHA256                       string
	SourceSizeBytes                                        int64
	TargetObjectKey, TargetObjectVersion                   string
	Attempt                                                int32
	Archive                                                *ArchiveBinding
}

type wireTask Task

func (task Task) MarshalJSON() ([]byte, error)    { return json.Marshal(wireTask(task)) }
func (task *Task) UnmarshalJSON(raw []byte) error { return json.Unmarshal(raw, (*wireTask)(task)) }

func FromProto(input *controlplanev1.SessionArchiveTask) (Task, error) {
	if input == nil {
		return Task{}, errors.New("session archive task is missing")
	}
	kinds := map[controlplanev1.SessionArchiveTaskKind]string{
		controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_SNAPSHOT:      "SNAPSHOT",
		controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_RESTORE:       "RESTORE",
		controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_DELETE_PVC:    "DELETE_PVC",
		controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_DELETE_OBJECT: "DELETE_OBJECT",
	}
	task := Task{TaskRef: input.GetTaskRef(), Kind: kinds[input.GetKind()], OrganizationRef: input.GetOrganizationRef(),
		ProjectRef: input.GetProjectRef(), SessionRef: input.GetSessionRef(), ProviderAccountRef: input.GetProviderAccountRef(),
		RuntimeRevisionRef: input.GetRuntimeRevisionRef(), RuntimeRevisionVersion: input.GetRuntimeRevisionVersion(),
		RuntimeRevisionDigest: input.GetRuntimeRevisionDigest(), CodexSessionID: input.GetCodexSessionId(),
		ContentGeneration: input.GetContentGeneration(), PVCName: input.GetPvcName(), InputDigest: input.GetInputDigest(),
		SourceRelativePath: input.GetSourceRelativePath(), SourceSHA256: input.GetSourceSha256(),
		SourceSizeBytes: input.GetSourceSizeBytes(), TargetObjectKey: input.GetTargetObjectKey(),
		TargetObjectVersion: input.GetTargetObjectVersion(), Attempt: input.GetAttempt()}
	if archive := input.GetArchive(); archive != nil {
		task.Archive = &ArchiveBinding{ArchiveRef: archive.GetArchiveRef(), FormatVersion: archive.GetFormatVersion(),
			ObjectKey: archive.GetObjectKey(), ObjectVersion: archive.GetObjectVersion(), ObjectETag: archive.GetObjectEtag(),
			ObjectDigest: archive.GetObjectDigest(), ObjectSizeBytes: archive.GetObjectSizeBytes(),
			SourceRelativePath: archive.GetSourceRelativePath(), SourceSHA256: archive.GetSourceSha256(),
			SourceSizeBytes: archive.GetSourceSizeBytes()}
	}
	return task, task.Validate()
}

func DecodeFile(path string) (Task, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > MaximumTaskBytes {
		return Task{}, errors.New("read session archive task")
	}
	var task Task
	if json.Unmarshal(raw, &task) != nil || task.Validate() != nil {
		return Task{}, errors.New("decode session archive task")
	}
	return task, nil
}

func (task Task) Validate() error {
	for _, required := range []string{task.TaskRef, task.Kind, task.OrganizationRef, task.SessionRef,
		task.ProviderAccountRef, task.RuntimeRevisionRef, task.RuntimeRevisionDigest, task.CodexSessionID,
		task.PVCName, task.InputDigest, task.SourceRelativePath, task.SourceSHA256} {
		if strings.TrimSpace(required) == "" || len(required) > 1024 {
			return errors.New("session archive task identity is invalid")
		}
	}
	if task.RuntimeRevisionVersion < 1 || task.ContentGeneration < 1 || task.Attempt < 1 ||
		task.SourceSizeBytes < 1 || task.SourceSizeBytes > MaximumSourceBytes ||
		len(task.RuntimeRevisionDigest) != 64 || len(task.SourceSHA256) != 64 || len(task.InputDigest) != 64 ||
		runtimecontract.ValidateCodexArchiveIdentity(task.CodexSessionID, task.SourceRelativePath) != nil {
		return errors.New("session archive task binding is invalid")
	}
	switch task.Kind {
	case "SNAPSHOT":
		if task.TargetObjectKey == "" || task.Archive != nil {
			return errors.New("session snapshot task is invalid")
		}
	case "RESTORE":
		if task.Archive == nil || task.Archive.Validate() != nil {
			return errors.New("session restore task is invalid")
		}
	case "DELETE_PVC":
		if task.Archive == nil || task.Archive.Validate() != nil {
			return errors.New("session PVC deletion task is invalid")
		}
	case "DELETE_OBJECT":
		if task.TargetObjectKey == "" {
			return errors.New("session object deletion task is invalid")
		}
	default:
		return errors.New("session archive task kind is invalid")
	}
	return nil
}

func (binding ArchiveBinding) Validate() error {
	if binding.ArchiveRef == "" || binding.FormatVersion != FormatVersion || binding.ObjectKey == "" ||
		binding.ObjectETag == "" || !strings.HasPrefix(binding.ObjectDigest, "sha256:") ||
		len(binding.ObjectDigest) != 71 || binding.ObjectSizeBytes < 1 || binding.ObjectSizeBytes > MaximumObjectBytes ||
		binding.SourceRelativePath == "" || len(binding.SourceSHA256) != 64 ||
		binding.SourceSizeBytes < 1 || binding.SourceSizeBytes > MaximumSourceBytes {
		return errors.New("session archive binding is invalid")
	}
	return nil
}

type Result struct {
	Success         bool   `json:"success"`
	SafeErrorCode   string `json:"safe_error_code,omitempty"`
	FormatVersion   uint32 `json:"format_version,omitempty"`
	ObjectKey       string `json:"object_key,omitempty"`
	ObjectVersion   string `json:"object_version,omitempty"`
	ObjectETag      string `json:"object_etag,omitempty"`
	ObjectDigest    string `json:"object_digest,omitempty"`
	ObjectSizeBytes int64  `json:"object_size_bytes,omitempty"`
	SourceSHA256    string `json:"source_sha256,omitempty"`
	SourceSizeBytes int64  `json:"source_size_bytes,omitempty"`
}

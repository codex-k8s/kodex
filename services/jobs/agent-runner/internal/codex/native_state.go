package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	native "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/opencode"
)

// nativeCheckpointTimeout bounds offline state operations after cancellation; it permits no inference.
// nativeCheckpointTimeout ограничивает offline state operations после отмены; inference не разрешается.
const nativeCheckpointTimeout = 20 * time.Second

// openNativeStateRoot requires the runner-created shared root and retains its exact inode.
// The command executor never mounts this provider-state tree; user repository paths cannot select it.
// openNativeStateRoot требует shared root, созданный runner, и закрепляет его точный inode.
// Command executor не монтирует это provider-state дерево; repository paths не выбирают его.
func openNativeStateRoot(input model.Input) (*os.Root, error) {
	path := filepath.Join(input.WorkspaceRoot, ".kodex/state/opencode")
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o770 || info.Mode()&os.ModeSetgid == 0 {
		return nil, runtimecontract.ErrNativeSession
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 10001 || stat.Gid != 29000 {
		return nil, runtimecontract.ErrNativeSession
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, runtimecontract.ErrNativeSession
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, runtimecontract.ErrNativeSession
	}
	return root, nil
}

// prepareNativeGeneration creates one immutable attempt intent before native mutations begin.
// Existing generation state requires reconciliation; it is never overwritten for a retry.
// prepareNativeGeneration создаёт immutable attempt intent до начала native mutations.
// Существующий generation state требует reconciliation и не перезаписывается при retry.
func prepareNativeGeneration(input model.Input) (int64, error) {
	root, err := openNativeStateRoot(input)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	if err := root.Mkdir("checkpoints", 0o750); err == nil {
		// Root.Mkdir accepts only permission bits; apply inherited runtime group mode separately.
		// Root.Mkdir принимает только permission bits; runtime group mode закрепляется отдельно.
		if root.Chmod("checkpoints", os.ModeSetgid|0o750) != nil {
			return 0, runtimecontract.ErrNativeSession
		}
	} else if !errors.Is(err, os.ErrExist) {
		return 0, runtimecontract.ErrNativeSession
	}
	info, err := root.Lstat("checkpoints")
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o750 || info.Mode()&os.ModeSetgid == 0 {
		return 0, runtimecontract.ErrNativeSession
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || stat.Gid != 29000 {
		return 0, runtimecontract.ErrNativeSession
	}
	generation := int64(1)
	if input.NativeSession != nil {
		generation = input.NativeSession.Generation + 1
	}
	if _, err := runtimecontract.NativeSessionExportPath("ses_validation", generation); err != nil {
		return 0, err
	}
	directory := "checkpoints/g" + strconv.FormatInt(generation, 10)
	if root.Mkdir(directory, 0o750) != nil {
		return 0, runtimecontract.ErrNativeSession
	}
	if root.Chmod(directory, os.ModeSetgid|0o750) != nil {
		return 0, runtimecontract.ErrNativeSession
	}
	raw, err := json.Marshal(struct {
		Schema                 string `json:"schema"`
		OrganizationRef        string `json:"organization_ref"`
		ProjectRef             string `json:"project_ref"`
		AgentRef               string `json:"agent_ref"`
		SessionRef             string `json:"session_ref"`
		TurnRef                string `json:"turn_ref"`
		ExecutionBindingDigest string `json:"execution_binding_digest"`
		Generation             int64  `json:"generation"`
	}{"kodex.native-session-intent.v1", input.OrganizationRef, input.ProjectRef, input.AgentRef, input.SessionRef, input.TurnRef, input.ExecutionBindingDigest, generation})
	if err != nil {
		return 0, runtimecontract.ErrNativeSession
	}
	file, err := root.OpenFile(directory+"/intent.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, runtimecontract.ErrNativeSession
	}
	_, writeErr := file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return 0, runtimecontract.ErrNativeSession
	}
	for _, path := range []string{directory, "checkpoints", "."} {
		file, err := root.Open(path)
		if err != nil {
			return 0, runtimecontract.ErrNativeSession
		}
		err = file.Sync()
		file.Close()
		if err != nil {
			return 0, runtimecontract.ErrNativeSession
		}
	}
	return generation, nil
}

// captureNativeCheckpoint exports only after the native writer has joined, then seals verified metadata.
// A failed export leaves private state and intent intact; a model's final text grants no capture proof.
// captureNativeCheckpoint экспортирует лишь после join native writer и запечатывает проверенные metadata.
// При ошибке private state и intent сохраняются; final text модели не даёт capture proof.
func captureNativeCheckpoint(ctx context.Context, input model.Input, server *native.Server, sessionID string, generation int64, credentials ...string) (*nativeCaptureProof, error) {
	if err := server.Stop(); err != nil {
		return nil, err
	}
	relative, err := runtimecontract.NativeSessionExportPath(sessionID, generation)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(input.WorkspaceRoot, filepath.FromSlash(relative))
	export, err := server.Export(ctx, sessionID, path, credentials...)
	if err != nil {
		return nil, err
	}
	checkpoint := runtimecontract.NativeSessionCheckpoint{SessionID: sessionID, Version: runtimecontract.OpenCodeVersion, Generation: generation, RelativePath: relative, SHA256: export.SHA256, SizeBytes: export.SizeBytes}
	if input.WorkspaceStorage != nil {
		checkpoint.WorkspaceRef = input.WorkspaceStorage.WorkspaceRef
		checkpoint.WorkspaceGeneration = input.WorkspaceStorage.Generation
	}
	if checkpoint.ValidateCompletion(input) != nil {
		return nil, runtimecontract.ErrNativeSession
	}
	// Only trusted runner/archive peers read the published export; native HOME/config remain private.
	// Published export читают только trusted runner/archive peers; native HOME/config остаются private.
	if os.Chmod(path, 0o640) != nil {
		return nil, runtimecontract.ErrNativeSession
	}
	proof := &nativeCaptureProof{Schema: nativeCaptureSchema, ExecutionBindingDigest: input.ExecutionBindingDigest, RuntimeRevisionDigest: input.RuntimeRevisionDigest, InputDigest: input.InputDigest, LeaseRef: input.LeaseRef, LeaseGeneration: input.LeaseGeneration, OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, AgentRef: input.AgentRef, SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt, Checkpoint: checkpoint}
	result := Result{NativeSession: &checkpoint}
	if verifyNativeCapture(input, result, proof, providerWriterUID) != nil {
		return nil, runtimecontract.ErrNativeSession
	}
	file, err := os.Open(filepath.Dir(path))
	if err != nil {
		return nil, runtimecontract.ErrNativeSession
	}
	err = file.Sync()
	file.Close()
	if err != nil {
		return nil, runtimecontract.ErrNativeSession
	}
	proof.sealed = true
	return proof, nil
}

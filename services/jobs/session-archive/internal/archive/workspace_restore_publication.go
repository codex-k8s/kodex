package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

const (
	// workspaceRestoreIntentFile binds a private generation to one server-selected logical restore, independent of lease retries.
	// workspaceRestoreIntentFile связывает private generation с одним server-selected restore независимо от повторов lease.
	workspaceRestoreIntentFile = "restore-intent.json"
	// workspaceRestorePreparedFile records the exact verified staging inode before its atomic name publication.
	// workspaceRestorePreparedFile фиксирует проверенный staging inode до атомарной публикации имени.
	workspaceRestorePreparedFile = "restore-prepared.json"
	// workspaceRestoreSourceFile matches the runner's protected source-checkpoint record without adding execution authority.
	// workspaceRestoreSourceFile соответствует защищённой source-записи runner без выдачи execution authority.
	workspaceRestoreSourceFile = "source.json"
	// workspaceRestoreStagingTree is private unpublished data, retained when extraction or publication is uncertain.
	// workspaceRestoreStagingTree содержит private unpublished data, сохраняемые при неопределённом исходе.
	workspaceRestoreStagingTree = ".restore-tree"
)

// workspaceRestoreIntent closes data provenance while deliberately excluding attempt-specific lease or fence material.
// Its caller separately proves current owner grant, Pod/PVC custody, stopped predecessors and trusted image/input.
// workspaceRestoreIntent замыкает происхождение данных без attempt-specific lease/fence material.
// Caller отдельно доказывает current grant, Pod/PVC custody, остановку predecessor и trusted image/input.
type workspaceRestoreIntent struct {
	Schema, TaskRef, PVCName, PVCUID, RuntimeRevisionRef, RuntimeRevisionDigest, PolicyDigest string
	ContentGeneration, RuntimeRevisionVersion                                                 int64
	Target                                                                                    runtimecontract.WorkspaceStorageBinding
	Source                                                                                    runtimecontract.WorkspaceCheckpointBinding
}

// workspaceRestorePrepared binds verification to an actual directory inode, not merely a matching pathname.
// workspaceRestorePrepared связывает проверку с фактическим directory inode, не только с именем пути.
type workspaceRestorePrepared struct {
	Schema, IntentSHA256 string
	Device, Inode        uint64
}

// workspaceRestorePublication owns a duplicate private generation descriptor and the frozen logical input.
// It neither extracts files nor resolves authority from local records; partial stages are never deleted.
// workspaceRestorePublication владеет копией private generation descriptor и frozen logical input.
// Она не извлекает файлы и не выводит authority из local records; partial stages не удаляются.
type workspaceRestorePublication struct {
	parent                int
	owner                 securefile.OwnedRecordIdentity
	intent, source        []byte
	digest                string
	reconcileOnly, closed bool
	after                 func(string) error
}

// workspaceRestoreIntentBytes validates a complete data binding before any private metadata is consumed.
// workspaceRestoreIntentBytes проверяет полный data binding до чтения private metadata.
func workspaceRestoreIntentBytes(task model.Task, pvcUID string) ([]byte, error) {
	if task.Validate() != nil || task.Kind != "RESTORE" || task.Workspace == nil || task.Workspace.Checkpoint == nil ||
		pvcUID == "" || len(pvcUID) > 128 || strings.TrimSpace(pvcUID) != pvcUID || strings.ContainsAny(pvcUID, "/\\\x00") ||
		task.Workspace.PVCUID != "" && task.Workspace.PVCUID != pvcUID ||
		task.Workspace.Checkpoint.Bytes > task.Workspace.Policy.MaximumWritableBytes ||
		task.Workspace.Checkpoint.Entries > task.Workspace.Policy.MaximumFileCount {
		return nil, errWorkspaceObjectUnconfirmed
	}
	intent := workspaceRestoreIntent{Schema: "kodex.workspace-restore-intent.v1", TaskRef: task.TaskRef,
		PVCName: task.PVCName, PVCUID: pvcUID, ContentGeneration: task.ContentGeneration,
		RuntimeRevisionRef: task.RuntimeRevisionRef, RuntimeRevisionVersion: task.RuntimeRevisionVersion,
		RuntimeRevisionDigest: task.RuntimeRevisionDigest, PolicyDigest: task.Workspace.Policy.Digest,
		Target: task.Workspace.Target, Source: *task.Workspace.Checkpoint}
	raw, err := json.Marshal(intent)
	if err != nil || len(raw) > runtimecontract.MaximumWorkspaceManifestBytes {
		return nil, errWorkspaceObjectUnconfirmed
	}
	return raw, nil
}

// newWorkspaceRestorePublication opens only an already initialized exact private generation and matching immutable intent.
// The trusted opener supplies owner IDs and concrete PVC UID; none are selected by archive filenames or source bytes.
// newWorkspaceRestorePublication открывает только подготовленное private generation с точным immutable intent.
// Trusted opener передаёт owner IDs и actual PVC UID; имена архива/source bytes их не выбирают.
func newWorkspaceRestorePublication(ctx context.Context, parent int, task model.Task, pvcUID string, owner securefile.OwnedRecordIdentity) (*workspaceRestorePublication, error) {
	raw, err := workspaceRestoreIntentBytes(task, pvcUID)
	if err != nil || owner.UID != uint32(os.Geteuid()) {
		return nil, errWorkspaceObjectUnconfirmed
	}
	actual, err := securefile.ReadOwnedRecordAt(ctx, parent, workspaceRestoreIntentFile, runtimecontract.MaximumWorkspaceManifestBytes, owner)
	if err != nil || !bytes.Equal(actual, raw) {
		return nil, errWorkspaceObjectUnconfirmed
	}
	copyFD, err := unix.FcntlInt(uintptr(parent), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, errWorkspaceObjectUnconfirmed
	}
	source, err := json.Marshal(task.Workspace.Checkpoint)
	if err != nil {
		_ = unix.Close(copyFD)
		return nil, errWorkspaceObjectUnconfirmed
	}
	digest := sha256.Sum256(raw)
	return &workspaceRestorePublication{parent: copyFD, owner: owner, intent: raw, source: source,
		digest: hex.EncodeToString(digest[:]), reconcileOnly: task.Workspace.ReconcileOnly}, nil
}

// tree observes one concrete generation directory without following links or crossing its filesystem boundary.
// tree наблюдает конкретный generation directory без ссылок и перехода на другой filesystem.
func (publication *workspaceRestorePublication) tree(name string) (unix.Stat_t, error) {
	var info, parent unix.Stat_t
	fd, err := unix.Openat2(publication.parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return info, errWorkspaceObjectUnconfirmed
	}
	statErr := unix.Fstat(fd, &info)
	parentErr := unix.Fstat(publication.parent, &parent)
	closeErr := unix.Close(fd)
	if statErr != nil || parentErr != nil || closeErr != nil || info.Uid != publication.owner.UID || info.Gid != publication.owner.GID ||
		info.Dev != parent.Dev || info.Mode&0o7777 != 0o2770 || parent.Uid != publication.owner.UID || parent.Mode&0o7777 != 0o700 {
		return info, errWorkspaceObjectUnconfirmed
	}
	return info, nil
}

// record ensures exact immutable metadata and permits only a matching readback when a previous create is already visible.
// record обеспечивает immutable metadata и допускает только matching readback при уже видимом прежнем create.
func (publication *workspaceRestorePublication) record(ctx context.Context, name string, wanted []byte) error {
	err := securefile.WriteOwnedRecordAt(ctx, publication.parent, name, wanted, runtimecontract.MaximumWorkspaceManifestBytes, publication.owner)
	if err != nil && !errors.Is(err, unix.EEXIST) {
		return errWorkspaceObjectUnconfirmed
	}
	return publication.existingRecord(ctx, name, wanted)
}

// existingRecord verifies exact sealed bytes and repeats their file and parent durability barriers without changing data.
// existingRecord проверяет точные sealed bytes и повторяет file/parent durability barriers без изменения данных.
func (publication *workspaceRestorePublication) existingRecord(ctx context.Context, name string, wanted []byte) error {
	actual, err := securefile.ReadOwnedRecordAt(ctx, publication.parent, name, runtimecontract.MaximumWorkspaceManifestBytes, publication.owner)
	if err != nil || !bytes.Equal(actual, wanted) {
		return errWorkspaceObjectUnconfirmed
	}
	fd, err := unix.Openat(publication.parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return errWorkspaceObjectUnconfirmed
	}
	syncErr := unix.Fsync(fd)
	closeErr := unix.Close(fd)
	if syncErr != nil || closeErr != nil || unix.Fsync(publication.parent) != nil || ctx.Err() != nil {
		return errWorkspaceObjectUnconfirmed
	}
	return nil
}

// checkpoint marks testable crash boundaries only after the preceding operation actually completed.
// checkpoint отмечает проверяемые crash boundaries только после завершения предыдущей операции.
func (publication *workspaceRestorePublication) checkpoint(stage string) error {
	if publication.after != nil && publication.after(stage) != nil {
		return errWorkspaceObjectUnconfirmed
	}
	return nil
}

// Commit publishes only a fully verified staging inode using no-replace rename and a final exact readback.
// The caller has closed all data writers and supplies the same verified tree descriptor; retry-only tasks cannot extract or rename.
// Commit публикует только проверенный staging inode через no-replace rename и final exact readback.
// Caller закрыл всех data writers и передаёт прежний проверенный tree descriptor; retry-only tasks не извлекают и не переименовывают.
func (publication *workspaceRestorePublication) Commit(ctx context.Context, treeFD int) error {
	if publication == nil || publication.closed || publication.reconcileOnly || ctx == nil || ctx.Err() != nil {
		return errWorkspaceObjectUnconfirmed
	}
	if publication.existingRecord(ctx, workspaceRestoreIntentFile, publication.intent) != nil {
		return errWorkspaceObjectUnconfirmed
	}
	var current unix.Stat_t
	staging, err := publication.tree(workspaceRestoreStagingTree)
	if err != nil || unix.Fstat(treeFD, &current) != nil || current.Dev != staging.Dev || current.Ino != staging.Ino || unix.Fsync(treeFD) != nil {
		return errWorkspaceObjectUnconfirmed
	}
	prepared := workspaceRestorePrepared{Schema: "kodex.workspace-restore-prepared.v1", IntentSHA256: publication.digest,
		Device: uint64(staging.Dev), Inode: staging.Ino}
	raw, err := json.Marshal(prepared)
	if err != nil || publication.record(ctx, workspaceRestoreSourceFile, publication.source) != nil || publication.checkpoint("SOURCE") != nil ||
		publication.record(ctx, workspaceRestorePreparedFile, raw) != nil || publication.checkpoint("PREPARED") != nil || ctx.Err() != nil {
		return errWorkspaceObjectUnconfirmed
	}
	if unix.Renameat2(publication.parent, workspaceRestoreStagingTree, publication.parent, "tree", unix.RENAME_NOREPLACE) != nil {
		return errWorkspaceObjectUnconfirmed
	}
	if publication.checkpoint("RENAMED") != nil || unix.Fsync(publication.parent) != nil || publication.checkpoint("SYNCED") != nil || ctx.Err() != nil {
		return errWorkspaceObjectUnconfirmed
	}
	return publication.Rejoin(ctx)
}

// Rejoin verifies an already published exact intent/source/inode and repeats only its durability barrier.
// A prepared-but-unpublished tree or partial record never authorizes a new rename or data rewrite here.
// Rejoin проверяет уже опубликованные exact intent/source/inode и повторяет только durability barrier.
// Prepared-but-unpublished tree и partial record не разрешают здесь новый rename или data rewrite.
func (publication *workspaceRestorePublication) Rejoin(ctx context.Context) error {
	if publication == nil || publication.closed || ctx == nil || ctx.Err() != nil {
		return errWorkspaceObjectUnconfirmed
	}
	for name, wanted := range map[string][]byte{workspaceRestoreIntentFile: publication.intent, workspaceRestoreSourceFile: publication.source} {
		if publication.existingRecord(ctx, name, wanted) != nil {
			return errWorkspaceObjectUnconfirmed
		}
	}
	raw, err := securefile.ReadOwnedRecordAt(ctx, publication.parent, workspaceRestorePreparedFile, runtimecontract.MaximumWorkspaceManifestBytes, publication.owner)
	var prepared workspaceRestorePrepared
	if err != nil || runtimecontract.DecodeWorkspaceStorageTaskJSON(raw, &prepared) != nil ||
		prepared.Schema != "kodex.workspace-restore-prepared.v1" || prepared.IntentSHA256 != publication.digest || prepared.Inode == 0 ||
		publication.existingRecord(ctx, workspaceRestorePreparedFile, raw) != nil {
		return errWorkspaceObjectUnconfirmed
	}
	current, err := publication.tree("tree")
	var staging unix.Stat_t
	if err != nil || uint64(current.Dev) != prepared.Device || current.Ino != prepared.Inode ||
		!errors.Is(unix.Fstatat(publication.parent, workspaceRestoreStagingTree, &staging, unix.AT_SYMLINK_NOFOLLOW), unix.ENOENT) ||
		unix.Fsync(publication.parent) != nil || ctx.Err() != nil {
		return errWorkspaceObjectUnconfirmed
	}
	return nil
}

// Close releases only the owned descriptor and leaves all prepared, published or uncertain filesystem state intact.
// Close освобождает только собственный descriptor, сохраняя prepared/published/unknown filesystem state.
func (publication *workspaceRestorePublication) Close() error {
	if publication == nil || publication.closed {
		return nil
	}
	publication.closed = true
	if unix.Close(publication.parent) != nil {
		return errWorkspaceObjectUnconfirmed
	}
	return nil
}

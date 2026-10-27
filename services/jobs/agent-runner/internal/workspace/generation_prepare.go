package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

const (
	// BoundSessionWorkspaceMount is visible only to the trusted first initializer after owner BIND.
	// BoundSessionWorkspaceMount доступен лишь trusted first initializer после owner BIND.
	BoundSessionWorkspaceMount = runtimecontract.WorkspaceStorageRootMount
	// generationMountFile stores inode evidence separately from the exact data tree.
	// generationMountFile хранит inode evidence отдельно от exact data tree.
	generationMountFile = "mount.json"
	// generationWriterFile stores only the closed authenticated owner ACK, never credentials.
	// generationWriterFile хранит только closed authenticated owner ACK, не credentials.
	generationWriterFile = "writer.json"
	// generationSourceFile binds trusted restore preparation to the exact immutable source checkpoint.
	// generationSourceFile связывает trusted restore preparation с exact immutable source checkpoint.
	generationSourceFile = "source.json"
)

// PrepareBoundGeneration creates private parents and one exact tree only after an authenticated ACK.
// The caller must complete RC owned Pod/PVC readback and owner CAS before calling any write helper.
// PrepareBoundGeneration создаёт private parents и exact tree лишь после authenticated ACK.
// Caller завершает RC owned Pod/PVC readback и owner CAS до вызова любого write helper.
func PrepareBoundGeneration(ctx context.Context, input runtimecontract.RunnerInput, ack runtimecontract.WorkspaceWriterAcknowledgement) error {
	if os.Geteuid() != 10001 || ctx.Err() != nil || !ack.Matches(input) {
		return &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	}
	root, err := unix.Open(BoundSessionWorkspaceMount, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return classify(err)
	}
	defer unix.Close(root)
	var volume unix.Stat_t
	if unix.Fstat(root, &volume) != nil || volume.Mode&unix.S_IFMT != unix.S_IFDIR || volume.Uid != 0 ||
		volume.Gid != 29000 || volume.Mode&0o070 != 0o070 {
		return &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	}
	return prepareBoundGenerationAt(ctx, root, input, ack, generationFileOwner{uid: 10001, gid: 29000})
}

// generationFileOwner supplies trusted process IDs to descriptor helpers, never runtime input fields.
// Production fixes UID10001/GID29000; isolated filesystem tests use their own unprivileged process IDs.
// generationFileOwner передаёт trusted process IDs descriptor helpers, не поля runtime input.
// Production закрепляет UID10001/GID29000; filesystem tests используют свои unprivileged process IDs.
type generationFileOwner struct{ uid, gid uint32 }

// prepareBoundGenerationAt owns descriptor-relative creation and never removes previous generations.
// Its root is trusted by the production opener; shape validation alone is insufficient to call it.
// prepareBoundGenerationAt выполняет descriptor-relative создание без удаления прежних generations.
// Production opener доказывает root; одной shape validation недостаточно для вызова.
func prepareBoundGenerationAt(ctx context.Context, root int, input runtimecontract.RunnerInput, ack runtimecontract.WorkspaceWriterAcknowledgement, owner generationFileOwner) error {
	denied := &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	if ctx.Err() != nil || uint32(os.Geteuid()) != owner.uid || !ack.Matches(input) {
		return denied
	}
	subpath, err := input.WorkspaceStorage.GenerationSubPath()
	if err != nil {
		return denied
	}
	parts := strings.Split(subpath, "/")
	current, err := unix.Openat(root, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return denied
	}
	defer func() { _ = unix.Close(current) }()
	for _, component := range parts[:len(parts)-1] {
		if ctx.Err() != nil {
			return denied
		}
		createErr := unix.Mkdirat(current, component, 0o700)
		if createErr != nil && !errors.Is(createErr, unix.EEXIST) {
			return denied
		}
		created := createErr == nil
		next, err := unix.Openat2(current, component, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
			Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
		if err != nil {
			return denied
		}
		var info unix.Stat_t
		if created && unix.Fchmod(next, 0o700) != nil || unix.Fstat(next, &info) != nil || info.Uid != owner.uid || info.Mode&0o7777 != 0o700 {
			unix.Close(next)
			return denied
		}
		if created && (unix.Fsync(next) != nil || unix.Fsync(current) != nil) {
			unix.Close(next)
			return denied
		}
		unix.Close(current)
		current = next
	}
	createErr := unix.Mkdirat(current, "tree", 0o2770)
	if createErr != nil && !errors.Is(createErr, unix.EEXIST) {
		return denied
	}
	created := createErr == nil
	tree, err := unix.Openat2(current, "tree", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return denied
	}
	defer unix.Close(tree)
	if created && (unix.Fchown(tree, -1, int(owner.gid)) != nil || unix.Fchmod(tree, 0o2770) != nil) {
		return denied
	}
	if created && (unix.Fsync(tree) != nil || unix.Fsync(current) != nil) {
		return denied
	}
	var info unix.Stat_t
	if unix.Fstat(tree, &info) != nil || info.Uid != owner.uid || info.Gid != owner.gid || info.Mode&0o7777 != 0o2770 {
		return denied
	}
	expectedAck, err := runtimecontract.EncodeWorkspaceWriterAcknowledgement(ack)
	if err != nil {
		return denied
	}
	previous, err := readGenerationRecord(current, generationWriterFile, runtimecontract.MaximumWorkspaceQuiescenceBytes, owner)
	if err == nil {
		proofRaw, proofErr := readGenerationRecord(current, generationMountFile, runtimecontract.MaximumWorkspaceMountProofBytes, owner)
		proof, decodeErr := runtimecontract.DecodeWorkspaceMountProof(proofRaw)
		if !bytes.Equal(previous, expectedAck) || proofErr != nil || decodeErr != nil || !proof.Matches(input, uint64(info.Dev), info.Ino) {
			return denied
		}
		return nil
	}
	if !errors.Is(err, unix.ENOENT) {
		return denied
	}
	if source := input.WorkspaceStorage.Source; source != nil {
		expected, err := json.Marshal(source)
		actual, readErr := readGenerationRecord(current, generationSourceFile, runtimecontract.MaximumWorkspaceManifestBytes, owner)
		if created || err != nil || readErr != nil || !bytes.Equal(expected, actual) {
			return denied
		}
	} else {
		readFD, err := unix.Openat(tree, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			return denied
		}
		file := os.NewFile(uintptr(readFD), "bound-work-tree")
		entries, err := file.Readdirnames(1)
		closeErr := file.Close()
		if len(entries) != 0 || err != io.EOF || closeErr != nil {
			return denied
		}
	}
	proof, err := runtimecontract.NewWorkspaceMountProof(input, uint64(info.Dev), info.Ino)
	if err != nil {
		return denied
	}
	proofRaw, err := runtimecontract.EncodeWorkspaceMountProof(proof)
	if err != nil {
		return denied
	}
	previousProof, readErr := readGenerationRecord(current, generationMountFile, runtimecontract.MaximumWorkspaceMountProofBytes, owner)
	if readErr == nil {
		if !bytes.Equal(previousProof, proofRaw) {
			return denied
		}
	} else if !errors.Is(readErr, unix.ENOENT) || writeGenerationRecord(current, generationMountFile, proofRaw, owner) != nil {
		return denied
	}
	if writeGenerationRecord(current, generationWriterFile, expectedAck, owner) != nil || unix.Fsync(current) != nil || ctx.Err() != nil {
		return denied
	}
	return nil
}

// readGenerationRecord accepts one owned private regular file and returns ENOENT distinctly.
// It follows no link and retains no raw operating-system error in the public failure path.
// readGenerationRecord принимает owned private regular file и отдельно возвращает ENOENT.
// Ссылки не прослеживаются; raw OS error не попадает в public failure path.
func readGenerationRecord(parent int, name string, maximum int64, owner generationFileOwner) ([]byte, error) {
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "bound-work-record")
	defer file.Close()
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Uid != owner.uid || info.Gid != owner.gid ||
		info.Nlink != 1 || info.Mode&0o7777 != 0o440 || info.Size < 1 || info.Size > maximum {
		return nil, &Denial{Reason: runtimecontract.RuntimeWorkspacePathOutsideWorkspace}
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(raw)) != info.Size {
		return nil, &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	return raw, nil
}

// writeGenerationRecord creates one synced immutable local record without replacing any old bytes.
// Its callers already hold the authenticated BIND; failed partial records are preserved, not erased.
// writeGenerationRecord создаёт synced immutable local record без замены прежних bytes.
// Caller уже имеет authenticated BIND; partial records сохраняются, а не удаляются.
func writeGenerationRecord(parent int, name string, raw []byte, owner generationFileOwner) error {
	fd, err := unix.Openat(parent, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if writeFull(fd, raw) != nil || unix.Fchown(fd, -1, int(owner.gid)) != nil || unix.Fchmod(fd, 0o440) != nil || unix.Fsync(fd) != nil {
		return &Denial{Reason: runtimecontract.RuntimeWorkspaceIOError}
	}
	return nil
}

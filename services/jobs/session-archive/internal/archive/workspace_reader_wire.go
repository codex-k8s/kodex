package archive

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

const (
	// workspaceReaderWireMagic closes the local read-only FD protocol without a general command envelope.
	// workspaceReaderWireMagic замыкает local read-only FD protocol без общего command envelope.
	workspaceReaderWireMagic = "KWR1"
	// workspaceReaderHelloMagic binds the same task/root before any stream or object write starts.
	// workspaceReaderHelloMagic связывает same task/root до начала stream или object write.
	workspaceReaderHelloMagic = "KWH1"
	// workspaceReaderDirectoryMagic identifies a separate read-only directory FD operation; KWR1 still grants regular files only.
	// workspaceReaderDirectoryMagic обозначает отдельную read-only directory FD operation; KWR1 выдаёт только regular files.
	workspaceReaderDirectoryMagic = "KWD1"
	// workspaceReaderMetadataMagic requests exact provider-owned metadata without granting any descriptor.
	// workspaceReaderMetadataMagic запрашивает exact provider-owned metadata без выдачи descriptor.
	workspaceReaderMetadataMagic = "KWS1"
	// workspaceReaderHeaderBytes contains magic, canonical task SHA256, root device/inode and path byte count.
	// workspaceReaderHeaderBytes содержит magic, canonical task SHA256, root device/inode и path byte count.
	workspaceReaderHeaderBytes = 4 + 32 + 8 + 8 + 2
	// workspaceReaderMaximumFrame bounds all local requests; paths remain independently limited to4096bytes.
	// workspaceReaderMaximumFrame ограничивает все local requests; paths отдельно ограничены4096bytes.
	workspaceReaderMaximumFrame = 8192
)

// errWorkspaceReaderInvalid hides private paths, task metadata and operating-system error details.
// errWorkspaceReaderInvalid скрывает private paths, task metadata и operating-system error details.
var errWorkspaceReaderInvalid = errors.New("session workspace read-only descriptor is unconfirmed")

// workspaceReaderRequest binds one relative regular-file lookup to the same immutable task and mounted root.
// This protocol transfers no storage credentials, arbitrary descriptor number or execution authority.
// workspaceReaderRequest связывает relative regular-file lookup с immutable task и mounted root.
// Протокол не передаёт storage credentials, arbitrary descriptor number или execution authority.
type workspaceReaderRequest struct {
	taskSHA256 [32]byte
	device     uint64
	inode      uint64
	path       string
	hello      bool
	kind       workspaceReaderOperation
}

// workspaceReaderOperation is a closed local read-only operation kind, never an actor or filesystem permission.
// workspaceReaderOperation — закрытый local read-only operation kind, не actor либо filesystem permission.
type workspaceReaderOperation uint8

const (
	// workspaceReaderFile retains the original regular-file-only request and response validation.
	// workspaceReaderFile сохраняет original regular-file-only request/response validation.
	workspaceReaderFile workspaceReaderOperation = iota
	// workspaceReaderDirectory permits only an exact owned read-only directory FD under the same root.
	// workspaceReaderDirectory допускает только exact owned read-only directory FD под тем же root.
	workspaceReaderDirectory
	// workspaceReaderMetadata returns only a bounded fixed-field stat observation under the same root.
	// workspaceReaderMetadata возвращает только bounded fixed-field stat observation под тем же root.
	workspaceReaderMetadata
)

// encodeWorkspaceReaderRequest preserves raw UTF-8 path bytes inside a fixed bounded frame, avoiding JSON expansion.
// encodeWorkspaceReaderRequest сохраняет UTF-8 path bytes в fixed bounded frame без JSON expansion.
func encodeWorkspaceReaderRequest(value workspaceReaderRequest) ([]byte, error) {
	if value.hello && (value.path != "" || value.kind != workspaceReaderFile) || !value.hello && !workspaceStreamPath(value.path) ||
		value.inode == 0 || value.taskSHA256 == [32]byte{} || value.kind > workspaceReaderMetadata {
		return nil, errWorkspaceReaderInvalid
	}
	raw := make([]byte, workspaceReaderHeaderBytes+len(value.path))
	if len(raw) > workspaceReaderMaximumFrame {
		return nil, errWorkspaceReaderInvalid
	}
	copy(raw[:4], workspaceReaderWireMagic)
	if value.hello {
		copy(raw[:4], workspaceReaderHelloMagic)
	} else if value.kind == workspaceReaderDirectory {
		copy(raw[:4], workspaceReaderDirectoryMagic)
	} else if value.kind == workspaceReaderMetadata {
		copy(raw[:4], workspaceReaderMetadataMagic)
	}
	copy(raw[4:36], value.taskSHA256[:])
	binary.BigEndian.PutUint64(raw[36:44], value.device)
	binary.BigEndian.PutUint64(raw[44:52], value.inode)
	binary.BigEndian.PutUint16(raw[52:54], uint16(len(value.path)))
	copy(raw[54:], value.path)
	return raw, nil
}

// decodeWorkspaceReaderRequest rejects unknown versions, trailing bytes, empty commitments and path escapes.
// decodeWorkspaceReaderRequest отклоняет unknown versions, trailing bytes, пустые commitments и path escapes.
func decodeWorkspaceReaderRequest(raw []byte) (workspaceReaderRequest, error) {
	if len(raw) < workspaceReaderHeaderBytes || len(raw) > workspaceReaderMaximumFrame ||
		!bytes.Equal(raw[:4], []byte(workspaceReaderWireMagic)) && !bytes.Equal(raw[:4], []byte(workspaceReaderHelloMagic)) &&
			!bytes.Equal(raw[:4], []byte(workspaceReaderDirectoryMagic)) && !bytes.Equal(raw[:4], []byte(workspaceReaderMetadataMagic)) ||
		int(binary.BigEndian.Uint16(raw[52:54])) != len(raw)-workspaceReaderHeaderBytes {
		return workspaceReaderRequest{}, errWorkspaceReaderInvalid
	}
	value := workspaceReaderRequest{device: binary.BigEndian.Uint64(raw[36:44]), inode: binary.BigEndian.Uint64(raw[44:52]), path: string(raw[54:]), hello: bytes.Equal(raw[:4], []byte(workspaceReaderHelloMagic))}
	copy(value.taskSHA256[:], raw[4:36])
	if bytes.Equal(raw[:4], []byte(workspaceReaderDirectoryMagic)) {
		value.kind = workspaceReaderDirectory
	} else if bytes.Equal(raw[:4], []byte(workspaceReaderMetadataMagic)) {
		value.kind = workspaceReaderMetadata
	}
	if _, err := encodeWorkspaceReaderRequest(value); err != nil {
		return workspaceReaderRequest{}, err
	}
	return value, nil
}

// workspaceReaderPeerUID obtains only Linux kernel credentials from an actual Unix connection.
// A payload UID, socket pathname or container label never substitutes for SO_PEERCRED.
// workspaceReaderPeerUID получает только Linux kernel credentials у настоящего Unix connection.
// Payload UID, socket pathname и container label не заменяют SO_PEERCRED.
func workspaceReaderPeerUID(connection *net.UnixConn) (uint32, error) {
	if connection == nil {
		return 0, errWorkspaceReaderInvalid
	}
	raw, err := connection.SyscallConn()
	if err != nil {
		return 0, errWorkspaceReaderInvalid
	}
	var credential *unix.Ucred
	var readErr error
	if raw.Control(func(fd uintptr) {
		credential, readErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}) != nil ||
		readErr != nil || credential == nil {
		return 0, errWorkspaceReaderInvalid
	}
	return credential.Uid, nil
}

// receiveWorkspaceReaderFile closes every received descriptor on any framing, ownership or identity mismatch.
// Only one read-only CLOEXEC regular-file descriptor, equal to the independently observed inode, can escape.
// receiveWorkspaceReaderFile закрывает все received descriptors при framing/ownership/identity mismatch.
// Возвращается только один read-only CLOEXEC regular-file descriptor с независимо observed inode.
func receiveWorkspaceReaderFile(connection *net.UnixConn, expected unix.Stat_t) (*os.File, error) {
	return receiveWorkspaceReaderDescriptor(connection, expected, false)
}

// receiveWorkspaceReaderDescriptor applies one closed kind's exact inode and read-only rights checks, closing all rejected descriptors.
// The regular-file wrapper always passes false; directory requests cannot widen its accepted file kinds.
// receiveWorkspaceReaderDescriptor проверяет exact inode/read-only rights закрытого kind и закрывает все rejected descriptors.
// Regular-file wrapper всегда передаёт false; directory requests не расширяют допустимые file kinds.
func receiveWorkspaceReaderDescriptor(connection *net.UnixConn, expected unix.Stat_t, directory bool) (*os.File, error) {
	if connection == nil {
		return nil, errWorkspaceReaderInvalid
	}
	var payload [2]byte
	control := make([]byte, unix.CmsgSpace(4))
	n, controlBytes, flags, _, readErr := connection.ReadMsgUnix(payload[:], control)
	descriptors, messageCount, validControl := workspaceReaderDescriptors(control[:controlBytes])
	accepted := false
	defer func() {
		if !accepted {
			for _, fd := range descriptors {
				_ = unix.Close(fd)
			}
		}
	}()
	if readErr != nil || !validControl || n != 1 || payload[0] != 1 || messageCount != 1 || len(descriptors) != 1 ||
		flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return nil, errWorkspaceReaderInvalid
	}
	fd := descriptors[0]
	var info unix.Stat_t
	fileFlags, fileErr := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	closeFlags, closeErr := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if unix.Fstat(fd, &info) != nil || !workspaceReaderEntryKind(info, directory) ||
		!workspaceTreeIdentityEqual(info, expected) || fileErr != nil || fileFlags&unix.O_ACCMODE != unix.O_RDONLY || fileFlags&unix.O_PATH != 0 ||
		closeErr != nil || closeFlags&unix.FD_CLOEXEC == 0 {
		return nil, errWorkspaceReaderInvalid
	}
	if directory {
		if offset, err := unix.Seek(fd, 0, io.SeekCurrent); err != nil || offset != 0 {
			return nil, errWorkspaceReaderInvalid
		}
	}
	accepted = true
	return os.NewFile(uintptr(fd), "workspace-read-only-source"), nil
}

// workspaceReaderDescriptors collects all kernel-delivered rights so every rejection can close its complete set.
// Unknown ancillary messages never become a second capability or an accepted protocol extension.
// workspaceReaderDescriptors собирает все kernel-delivered rights для полного закрытия при отказе.
// Unknown ancillary messages не становятся дополнительной capability или protocol extension.
func workspaceReaderDescriptors(control []byte) ([]int, int, bool) {
	messages, err := unix.ParseSocketControlMessage(control)
	valid := err == nil
	var descriptors []int
	for _, message := range messages {
		if message.Header.Level != unix.SOL_SOCKET || message.Header.Type != unix.SCM_RIGHTS {
			valid = false
			continue
		}
		fds, err := unix.ParseUnixRights(&message)
		if err != nil {
			valid = false
			continue
		}
		descriptors = append(descriptors, fds...)
	}
	return descriptors, len(messages), valid
}

// receiveWorkspaceReaderHello accepts only a descriptor-free exact handshake acknowledgement.
// Even an unexpected FD attached to an error is closed before returning the safe failure.
// receiveWorkspaceReaderHello принимает только exact handshake ACK без descriptors.
// Даже unexpected FD у error закрывается до возврата safe failure.
func receiveWorkspaceReaderHello(connection *net.UnixConn) error {
	if connection == nil {
		return errWorkspaceReaderInvalid
	}
	var payload [2]byte
	control := make([]byte, unix.CmsgSpace(4))
	n, controlBytes, flags, _, err := connection.ReadMsgUnix(payload[:], control)
	descriptors, count, valid := workspaceReaderDescriptors(control[:controlBytes])
	for _, fd := range descriptors {
		_ = unix.Close(fd)
	}
	if err != nil || !valid || count != 0 || len(descriptors) != 0 || n != 1 || payload[0] != 2 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return errWorkspaceReaderInvalid
	}
	return nil
}

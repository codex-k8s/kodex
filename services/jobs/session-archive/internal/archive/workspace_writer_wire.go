package archive

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

const (
	// workspaceWriterMagic versions a distinct writable protocol; read-only snapshot requests are never accepted.
	// workspaceWriterMagic версионирует отдельный writable protocol; read-only snapshot requests не принимаются.
	workspaceWriterMagic = "KWW1"
	// workspaceWriterHeaderBytes bounds immutable task/root commitments, one operation and its file parameters.
	// workspaceWriterHeaderBytes ограничивает immutable task/root commitments, одну operation и file parameters.
	workspaceWriterHeaderBytes = 67
	// workspaceWriterMaximumFrame leaves bounded space for the independently validated relative path.
	// workspaceWriterMaximumFrame оставляет ограниченное место для отдельно проверяемого relative path.
	workspaceWriterMaximumFrame = 8192
	// workspaceWriterHello starts the only authenticated task/root connection before any creation is allowed.
	// workspaceWriterHello начинает единственный authenticated task/root connection до любого create.
	workspaceWriterHello byte = 1
	// workspaceWriterDirectory creates one provider-owned temporary directory without final permission sealing.
	// workspaceWriterDirectory создаёт provider-owned temporary directory без final permission sealing.
	workspaceWriterDirectory byte = 2
	// workspaceWriterFile grants one exclusive write-only file and requires explicit completion before another command.
	// workspaceWriterFile выдаёт exclusive write-only file и требует explicit completion до следующей команды.
	workspaceWriterFile byte = 3
	// workspaceWriterSeal restores one provider directory's final mode after all descendants have closed.
	// workspaceWriterSeal возвращает итоговый mode provider directory после закрытия потомков.
	workspaceWriterSeal byte = 4
	// workspaceWriterComplete accepts the sole pending file only after kernel size/offset and durability checks.
	// workspaceWriterComplete принимает единственный pending file после kernel size/offset и durability checks.
	workspaceWriterComplete byte = 5
	// workspaceWriterFinish closes the provider adapter before acknowledging the end of the writable connection.
	// workspaceWriterFinish закрывает provider adapter до подтверждения завершения writable connection.
	workspaceWriterFinish byte = 6
)

// workspaceWriterRequest has no caller-supplied UID, descriptor number, absolute destination or storage credentials.
// workspaceWriterRequest не содержит caller-supplied UID, descriptor number, absolute destination или storage credentials.
type workspaceWriterRequest struct {
	taskSHA256    [32]byte
	device, inode uint64
	operation     byte
	mode          uint32
	size          int64
	path          string
}

// encodeWorkspaceWriterRequest enforces a closed command union before serializing its fixed bounded frame.
// encodeWorkspaceWriterRequest проверяет закрытый command union до сериализации bounded frame.
func encodeWorkspaceWriterRequest(value workspaceWriterRequest) ([]byte, error) {
	if value.taskSHA256 == [32]byte{} || value.inode == 0 || value.mode > 0o777 || value.size < 0 {
		return nil, errWorkspaceObjectUnconfirmed
	}
	switch value.operation {
	case workspaceWriterHello, workspaceWriterComplete, workspaceWriterFinish:
		if value.path != "" || value.mode != 0 || value.size != 0 {
			return nil, errWorkspaceObjectUnconfirmed
		}
	case workspaceWriterDirectory:
		if !workspaceStreamPath(value.path) || value.mode != 0 || value.size != 0 {
			return nil, errWorkspaceObjectUnconfirmed
		}
	case workspaceWriterFile:
		if !workspaceStreamPath(value.path) {
			return nil, errWorkspaceObjectUnconfirmed
		}
	case workspaceWriterSeal:
		if !workspaceStreamPath(value.path) || value.size != 0 {
			return nil, errWorkspaceObjectUnconfirmed
		}
	default:
		return nil, errWorkspaceObjectUnconfirmed
	}
	raw := make([]byte, workspaceWriterHeaderBytes+len(value.path))
	if len(raw) > workspaceWriterMaximumFrame {
		return nil, errWorkspaceObjectUnconfirmed
	}
	copy(raw[:4], workspaceWriterMagic)
	raw[4] = value.operation
	copy(raw[5:37], value.taskSHA256[:])
	binary.BigEndian.PutUint64(raw[37:45], value.device)
	binary.BigEndian.PutUint64(raw[45:53], value.inode)
	binary.BigEndian.PutUint32(raw[53:57], value.mode)
	binary.BigEndian.PutUint64(raw[57:65], uint64(value.size))
	binary.BigEndian.PutUint16(raw[65:67], uint16(len(value.path)))
	copy(raw[67:], value.path)
	return raw, nil
}

// decodeWorkspaceWriterRequest rejects alternate protocols, trailing bytes and invalid operation-specific fields.
// decodeWorkspaceWriterRequest отклоняет alternate protocols, trailing bytes и invalid operation-specific fields.
func decodeWorkspaceWriterRequest(raw []byte) (workspaceWriterRequest, error) {
	if len(raw) < workspaceWriterHeaderBytes || len(raw) > workspaceWriterMaximumFrame ||
		!bytes.Equal(raw[:4], []byte(workspaceWriterMagic)) || int(binary.BigEndian.Uint16(raw[65:67])) != len(raw)-workspaceWriterHeaderBytes {
		return workspaceWriterRequest{}, errWorkspaceObjectUnconfirmed
	}
	value := workspaceWriterRequest{operation: raw[4], device: binary.BigEndian.Uint64(raw[37:45]), inode: binary.BigEndian.Uint64(raw[45:53]),
		mode: binary.BigEndian.Uint32(raw[53:57]), size: int64(binary.BigEndian.Uint64(raw[57:65])), path: string(raw[67:])}
	copy(value.taskSHA256[:], raw[5:37])
	if _, err := encodeWorkspaceWriterRequest(value); err != nil {
		return workspaceWriterRequest{}, errWorkspaceObjectUnconfirmed
	}
	return value, nil
}

// receiveWorkspaceWriterRequest rejects and closes all unexpected transferred descriptors before interpreting a command.
// receiveWorkspaceWriterRequest отклоняет и закрывает все неожиданные transferred descriptors до разбора команды.
func receiveWorkspaceWriterRequest(connection *net.UnixConn) (workspaceWriterRequest, error) {
	payload := make([]byte, workspaceWriterMaximumFrame)
	control := make([]byte, unix.CmsgSpace(4))
	n, controlBytes, flags, _, err := connection.ReadMsgUnix(payload, control)
	descriptors, count, valid := workspaceReaderDescriptors(control[:controlBytes])
	for _, fd := range descriptors {
		_ = unix.Close(fd)
	}
	if err != nil || !valid || count != 0 || len(descriptors) != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return workspaceWriterRequest{}, errWorkspaceObjectUnconfirmed
	}
	return decodeWorkspaceWriterRequest(payload[:n])
}

// sendWorkspaceWriterRequest emits one descriptor-free command; an uncertain send never receives an automatic retry.
// sendWorkspaceWriterRequest отправляет descriptor-free command; uncertain send не повторяется автоматически.
func sendWorkspaceWriterRequest(connection *net.UnixConn, request workspaceWriterRequest) error {
	raw, err := encodeWorkspaceWriterRequest(request)
	if err != nil || connection == nil {
		return errWorkspaceObjectUnconfirmed
	}
	n, control, err := connection.WriteMsgUnix(raw, nil, nil)
	if err != nil || n != len(raw) || control != 0 {
		return errWorkspaceObjectUnconfirmed
	}
	return nil
}

// receiveWorkspaceWriterFile accepts only one new write-only regular inode matching an independently observed destination.
// All delivered descriptors are closed on rejection, including truncated or malformed ancillary messages.
// receiveWorkspaceWriterFile принимает только новый write-only regular inode с независимо observed destination.
// При отказе закрываются все delivered descriptors, включая truncated/malformed ancillary messages.
func receiveWorkspaceWriterFile(connection *net.UnixConn, rootFD int, name string, identity workspaceRestoreIdentity) (*os.File, error) {
	var payload [2]byte
	control := make([]byte, unix.CmsgSpace(4))
	n, controlBytes, flags, _, err := connection.ReadMsgUnix(payload[:], control)
	descriptors, count, valid := workspaceReaderDescriptors(control[:controlBytes])
	accepted := false
	defer func() {
		if !accepted {
			for _, fd := range descriptors {
				_ = unix.Close(fd)
			}
		}
	}()
	if err != nil || !valid || count != 1 || len(descriptors) != 1 || n != 1 || payload[0] != 1 ||
		flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || !workspaceStreamPath(name) {
		return nil, errWorkspaceObjectUnconfirmed
	}
	fd := descriptors[0]
	var current, expected, root unix.Stat_t
	fileFlags, fileErr := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	closeFlags, closeErr := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	pathFD, pathErr := unix.Openat2(rootFD, name, &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if pathErr != nil {
		return nil, errWorkspaceObjectUnconfirmed
	}
	defer unix.Close(pathFD)
	if unix.Fstat(rootFD, &root) != nil || unix.Fstat(pathFD, &expected) != nil || unix.Fstat(fd, &current) != nil ||
		current.Dev != root.Dev || current.Uid != identity.provider || current.Gid != identity.group || current.Nlink != 1 ||
		current.Mode&unix.S_IFMT != unix.S_IFREG || current.Mode&0o7777 != 0o600 || current.Size != 0 ||
		!workspaceTreeIdentityEqual(current, expected) || fileErr != nil || fileFlags&unix.O_ACCMODE != unix.O_WRONLY ||
		fileFlags&(unix.O_PATH|unix.O_APPEND) != 0 || closeErr != nil || closeFlags&unix.FD_CLOEXEC == 0 {
		return nil, errWorkspaceObjectUnconfirmed
	}
	accepted = true
	return os.NewFile(uintptr(fd), "workspace-restore-writer"), nil
}

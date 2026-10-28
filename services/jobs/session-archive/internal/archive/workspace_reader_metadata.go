package archive

import (
	"encoding/binary"
	"net"

	"golang.org/x/sys/unix"
)

// workspaceReaderMetadataBytes fixes the complete stat observation frame, excluding paths, arbitrary fields and descriptors.
// workspaceReaderMetadataBytes фиксирует полный stat observation frame без paths/arbitrary fields/descriptors.
const workspaceReaderMetadataBytes = 77

// workspaceReaderEntryKind keeps the two read-only descriptor grants separate and excludes hard-linked or special regular files.
// workspaceReaderEntryKind разделяет два read-only descriptor grants, исключая hard-linked/special regular files.
func workspaceReaderEntryKind(info unix.Stat_t, directory bool) bool {
	if directory {
		return info.Mode&unix.S_IFMT == unix.S_IFDIR && info.Mode&0o5000 == 0 && info.Nlink > 0
	}
	return info.Mode&unix.S_IFMT == unix.S_IFREG && info.Mode&0o7000 == 0 && info.Nlink == 1 && info.Size >= 0
}

// encodeWorkspaceReaderMetadata serializes only metadata used by the scanner's immutable identity comparison in a fixed byte order.
// encodeWorkspaceReaderMetadata кодирует только metadata scanner immutable identity comparison в fixed byte order.
func encodeWorkspaceReaderMetadata(info unix.Stat_t) ([]byte, error) {
	if !workspaceReaderEntryKind(info, info.Mode&unix.S_IFMT == unix.S_IFDIR) || info.Ino == 0 || info.Size < 0 ||
		info.Mtim.Nsec < 0 || info.Mtim.Nsec >= 1e9 || info.Ctim.Nsec < 0 || info.Ctim.Nsec >= 1e9 {
		return nil, errWorkspaceReaderInvalid
	}
	raw := make([]byte, workspaceReaderMetadataBytes)
	raw[0] = 3
	binary.BigEndian.PutUint64(raw[1:9], uint64(info.Dev))
	binary.BigEndian.PutUint64(raw[9:17], info.Ino)
	binary.BigEndian.PutUint64(raw[17:25], uint64(info.Nlink))
	binary.BigEndian.PutUint32(raw[25:29], info.Mode)
	binary.BigEndian.PutUint32(raw[29:33], info.Uid)
	binary.BigEndian.PutUint32(raw[33:37], info.Gid)
	binary.BigEndian.PutUint64(raw[37:45], uint64(info.Size))
	binary.BigEndian.PutUint64(raw[45:53], uint64(info.Mtim.Sec))
	binary.BigEndian.PutUint64(raw[53:61], uint64(info.Mtim.Nsec))
	binary.BigEndian.PutUint64(raw[61:69], uint64(info.Ctim.Sec))
	binary.BigEndian.PutUint64(raw[69:77], uint64(info.Ctim.Nsec))
	return raw, nil
}

// decodeWorkspaceReaderMetadata rejects unknown lengths, versions and unsupported inode kinds before returning a stat observation.
// decodeWorkspaceReaderMetadata отклоняет unknown lengths/versions и unsupported inode kinds до возврата stat observation.
func decodeWorkspaceReaderMetadata(raw []byte) (unix.Stat_t, error) {
	if len(raw) != workspaceReaderMetadataBytes || raw[0] != 3 {
		return unix.Stat_t{}, errWorkspaceReaderInvalid
	}
	info := unix.Stat_t{Dev: binary.BigEndian.Uint64(raw[1:9]), Ino: binary.BigEndian.Uint64(raw[9:17]),
		Nlink: binary.BigEndian.Uint64(raw[17:25]), Mode: binary.BigEndian.Uint32(raw[25:29]),
		Uid: binary.BigEndian.Uint32(raw[29:33]), Gid: binary.BigEndian.Uint32(raw[33:37]), Size: int64(binary.BigEndian.Uint64(raw[37:45])),
		Mtim: unix.Timespec{Sec: int64(binary.BigEndian.Uint64(raw[45:53])), Nsec: int64(binary.BigEndian.Uint64(raw[53:61]))},
		Ctim: unix.Timespec{Sec: int64(binary.BigEndian.Uint64(raw[61:69])), Nsec: int64(binary.BigEndian.Uint64(raw[69:77]))}}
	if _, err := encodeWorkspaceReaderMetadata(info); err != nil {
		return unix.Stat_t{}, errWorkspaceReaderInvalid
	}
	return info, nil
}

// receiveWorkspaceReaderMetadata closes every unexpected ancillary FD and accepts only one complete descriptor-free stat frame.
// receiveWorkspaceReaderMetadata закрывает все unexpected ancillary FD и принимает только complete descriptor-free stat frame.
func receiveWorkspaceReaderMetadata(connection *net.UnixConn) (unix.Stat_t, error) {
	if connection == nil {
		return unix.Stat_t{}, errWorkspaceReaderInvalid
	}
	raw := make([]byte, workspaceReaderMetadataBytes+1)
	control := make([]byte, unix.CmsgSpace(4))
	n, controlBytes, flags, _, err := connection.ReadMsgUnix(raw, control)
	descriptors, count, valid := workspaceReaderDescriptors(control[:controlBytes])
	for _, fd := range descriptors {
		_ = unix.Close(fd)
	}
	if err != nil || !valid || count != 0 || len(descriptors) != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return unix.Stat_t{}, errWorkspaceReaderInvalid
	}
	return decodeWorkspaceReaderMetadata(raw[:n])
}

// sendWorkspaceReaderMetadata observes only a provider-owned inode opened beneath the exact immutable root with no symlink or mount traversal.
// It sends no O_PATH descriptor: that temporary handle is closed in the provider process.
// sendWorkspaceReaderMetadata наблюдает только provider-owned inode под exact immutable root без symlink/mount traversal.
// O_PATH descriptor не передаётся: temporary handle закрывается в provider process.
func sendWorkspaceReaderMetadata(connection *net.UnixConn, rootFD int, name string, sourceUID, sourceGID uint32) error {
	if connection == nil || rootFD < 0 || !workspaceStreamPath(name) {
		return errWorkspaceReaderInvalid
	}
	fd, err := unix.Openat2(rootFD, name, &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return errWorkspaceReaderInvalid
	}
	defer unix.Close(fd)
	var info, root unix.Stat_t
	if unix.Fstat(fd, &info) != nil || unix.Fstat(rootFD, &root) != nil || info.Dev != root.Dev || info.Uid != sourceUID || info.Gid != sourceGID {
		return errWorkspaceReaderInvalid
	}
	raw, err := encodeWorkspaceReaderMetadata(info)
	if err != nil {
		return errWorkspaceReaderInvalid
	}
	n, controls, err := connection.WriteMsgUnix(raw, nil, nil)
	if err != nil || n != len(raw) || controls != 0 {
		return errWorkspaceReaderInvalid
	}
	return nil
}

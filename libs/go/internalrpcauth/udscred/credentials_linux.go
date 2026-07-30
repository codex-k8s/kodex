//go:build linux

package udscred

import (
	"context"
	"errors"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc/credentials"
)

const authType = "uds-peercred"

var ErrPeerIdentity = errors.New("UDS peer identity rejected")

type Identity struct {
	UID uint32
	GID uint32
	PID int32
}

type AuthInfo struct {
	Identity Identity
}

func (AuthInfo) AuthType() string {
	return authType
}

type Credentials struct {
	expectedPeerUID uint32
	expectedPeerGID uint32
}

func New(expectedPeerUID, expectedPeerGID uint32) credentials.TransportCredentials {
	return &Credentials{
		expectedPeerUID: expectedPeerUID,
		expectedPeerGID: expectedPeerGID,
	}
}

func (credentialsValue *Credentials) ClientHandshake(
	_ context.Context,
	_ string,
	connection net.Conn,
) (net.Conn, credentials.AuthInfo, error) {
	return credentialsValue.handshake(connection)
}

func (credentialsValue *Credentials) ServerHandshake(
	connection net.Conn,
) (net.Conn, credentials.AuthInfo, error) {
	return credentialsValue.handshake(connection)
}

func (credentialsValue *Credentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: authType}
}

func (credentialsValue *Credentials) Clone() credentials.TransportCredentials {
	return New(credentialsValue.expectedPeerUID, credentialsValue.expectedPeerGID)
}

func (*Credentials) OverrideServerName(string) error {
	return errors.New("server name is not applicable to UDS peer credentials")
}

func (credentialsValue *Credentials) handshake(
	connection net.Conn,
) (net.Conn, credentials.AuthInfo, error) {
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return nil, nil, fmt.Errorf("%w: connection is not Unix", ErrPeerIdentity)
	}
	raw, err := unixConnection.SyscallConn()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: inspect Unix connection", ErrPeerIdentity)
	}
	var peer *unix.Ucred
	var socketErr error
	if err := raw.Control(func(fileDescriptor uintptr) {
		peer, socketErr = unix.GetsockoptUcred(
			int(fileDescriptor),
			unix.SOL_SOCKET,
			unix.SO_PEERCRED,
		)
	}); err != nil {
		return nil, nil, fmt.Errorf("%w: inspect peer credentials", ErrPeerIdentity)
	}
	if socketErr != nil || peer == nil {
		return nil, nil, fmt.Errorf("%w: read SO_PEERCRED", ErrPeerIdentity)
	}
	if peer.Uid != credentialsValue.expectedPeerUID ||
		peer.Gid != credentialsValue.expectedPeerGID {
		return nil, nil, fmt.Errorf("%w: unexpected uid or gid", ErrPeerIdentity)
	}
	identity := Identity{UID: peer.Uid, GID: peer.Gid, PID: peer.Pid}
	return connection, AuthInfo{Identity: identity}, nil
}

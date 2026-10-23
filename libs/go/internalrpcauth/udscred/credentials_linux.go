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

// authType names this local gRPC transport credential mechanism; it is not
// a signed application identity or an authorization decision.
// RU: authType называет механизм локальной gRPC-транспортной проверки, но не
// является подписанной прикладной идентичностью либо решением о доступе.
const authType = "uds-peercred"

// ErrPeerIdentity classifies a rejected or unreadable Unix peer identity
// without exposing the expected or observed UID/GID in the error.
// RU: ErrPeerIdentity обозначает отклонённую либо нечитаемую идентичность Unix
// peer, не раскрывая ожидаемые или фактические UID/GID в ошибке.
var ErrPeerIdentity = errors.New("UDS peer identity rejected")

// Identity carries kernel-reported UID, GID and PID of one verified Unix
// peer; callers must still enforce application authorization separately.
// RU: Identity содержит сообщённые ядром UID, GID и PID проверенного Unix
// peer; прикладные полномочия вызывающий код проверяет отдельно.
type Identity struct {
	UID uint32
	GID uint32
	PID int32
}

// AuthInfo exposes the verified Unix peer identity to gRPC transport
// consumers without turning that identity into a bearer credential.
// RU: AuthInfo передаёт проверенную Unix-идентичность пользователям gRPC
// транспорта, не превращая её в bearer-полномочие.
type AuthInfo struct {
	Identity Identity
}

// AuthType identifies the local peer-credential transport to gRPC; the
// returned value does not attest to any domain-level permission.
// RU: AuthType сообщает gRPC имя локального транспортного механизма; это
// значение не удостоверяет никаких доменных полномочий.
func (AuthInfo) AuthType() string {
	return authType
}

// Credentials pins the exact expected UID and GID for each side of a Unix
// socket handshake; PID is reported but is not an admission condition.
// RU: Credentials закрепляет точные ожидаемые UID и GID для обеих сторон
// Unix-сокета; PID возвращается, но не служит условием допуска.
type Credentials struct {
	expectedPeerUID uint32
	expectedPeerGID uint32
}

// New builds local gRPC transport credentials for an exact peer UID/GID;
// it does not open a socket or grant application-level access.
// RU: New создаёт локальные gRPC credentials для точных UID/GID peer, не
// открывая сокет и не предоставляя прикладных прав.
func New(expectedPeerUID, expectedPeerGID uint32) credentials.TransportCredentials {
	return &Credentials{
		expectedPeerUID: expectedPeerUID,
		expectedPeerGID: expectedPeerGID,
	}
}

// ClientHandshake checks the server endpoint of an existing Unix connection
// with kernel SO_PEERCRED before returning transport authentication info.
// RU: ClientHandshake проверяет серверную сторону существующего Unix-соединения
// через SO_PEERCRED до возврата транспортной идентичности.
func (credentialsValue *Credentials) ClientHandshake(
	_ context.Context,
	_ string,
	connection net.Conn,
) (net.Conn, credentials.AuthInfo, error) {
	return credentialsValue.handshake(connection)
}

// ServerHandshake checks the client endpoint of an accepted Unix connection
// with the same exact UID/GID policy used by the client side.
// RU: ServerHandshake проверяет клиентскую сторону принятого Unix-соединения
// по той же точной политике UID/GID, что и клиентская сторона.
func (credentialsValue *Credentials) ServerHandshake(
	connection net.Conn,
) (net.Conn, credentials.AuthInfo, error) {
	return credentialsValue.handshake(connection)
}

// Info reports the transport mechanism name to gRPC without claiming TLS,
// encryption or application-level authorization.
// RU: Info сообщает gRPC имя транспортного механизма, не заявляя о TLS,
// шифровании либо прикладной авторизации.
func (credentialsValue *Credentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: authType}
}

// Clone makes a new transport-credential instance with the same pinned
// UID/GID, preserving peer admission without sharing mutable state.
// RU: Clone создаёт новый экземпляр с теми же закреплёнными UID/GID,
// сохраняя проверку peer без совместного изменяемого состояния.
func (credentialsValue *Credentials) Clone() credentials.TransportCredentials {
	return New(credentialsValue.expectedPeerUID, credentialsValue.expectedPeerGID)
}

// OverrideServerName rejects a TLS-style server-name override because this
// transport authenticates the kernel-reported Unix peer instead of a host.
// RU: OverrideServerName отклоняет переопределение имени сервера в стиле TLS:
// здесь проверяется Unix peer, сообщённый ядром, а не сетевой host.
func (*Credentials) OverrideServerName(string) error {
	return errors.New("server name is not applicable to UDS peer credentials")
}

// handshake requires a Unix connection, reads SO_PEERCRED from its file
// descriptor and returns AuthInfo only after exact UID and GID equality.
// RU: handshake требует Unix-соединение, читает SO_PEERCRED его дескриптора
// и возвращает AuthInfo только при точном совпадении UID и GID.
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

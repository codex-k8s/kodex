// Package credentialrelay передает обновленную provider credential revision
// из изолированного provider UID в execution-scoped callback boundary.
package credentialrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/callback"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

const (
	SocketPath          = "/run/kodex/credential-relay/relay.sock"
	maximumRequestBytes = 2 << 20
	maximumAckBytes     = 256
	providerUID         = 10002
	relayUID            = 10003
)

type request struct {
	LeaseRef string                                                 `json:"lease_ref"`
	Refresh  runtimecontract.RunnerProviderCredentialRefreshRequest `json:"refresh"`
}

type response struct {
	OK bool `json:"ok"`
}

type refreshCommitter interface {
	CommitProviderCredentialRefresh(context.Context, model.Input, runtimecontract.RunnerProviderCredentialRefreshRequest) error
}

func Commit(ctx context.Context, input model.Input, refresh runtimecontract.RunnerProviderCredentialRefreshRequest) error {
	if err := validateRequest(input, request{LeaseRef: input.LeaseRef, Refresh: refresh}); err != nil {
		return err
	}
	raw, err := json.Marshal(request{LeaseRef: input.LeaseRef, Refresh: refresh})
	if err != nil || len(raw) == 0 || len(raw) > maximumRequestBytes {
		clear(raw)
		return errors.New("encode provider credential relay request")
	}
	defer clear(raw)
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", SocketPath)
	if err != nil {
		return errors.New("provider credential relay is unavailable")
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	if _, err := connection.Write(raw); err != nil {
		return errors.New("write provider credential relay request")
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok || unixConnection.CloseWrite() != nil {
		return errors.New("seal provider credential relay request")
	}
	ack, err := io.ReadAll(io.LimitReader(connection, maximumAckBytes+1))
	if err != nil || len(ack) == 0 || len(ack) > maximumAckBytes {
		return errors.New("read provider credential relay acknowledgement")
	}
	defer clear(ack)
	decoder := json.NewDecoder(bytes.NewReader(ack))
	decoder.DisallowUnknownFields()
	var result response
	if decoder.Decode(&result) != nil || !decodeEOF(decoder) || !result.OK {
		return errors.New("provider credential relay rejected request")
	}
	return nil
}

func Serve(ctx context.Context, input model.Input) error {
	if os.Geteuid() != relayUID {
		return errors.New("provider credential relay UID is invalid")
	}
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return errors.New("disable provider credential relay process inspection")
	}
	client, err := callback.New(input)
	if err != nil {
		return errors.New("create provider credential relay callback")
	}
	defer client.Close()
	if err := os.MkdirAll(filepath.Dir(SocketPath), 0o770); err != nil {
		return errors.New("create provider credential relay socket directory")
	}
	_ = os.Remove(SocketPath)
	listener, err := net.Listen("unix", SocketPath)
	if err != nil {
		return errors.New("listen provider credential relay socket")
	}
	defer listener.Close()
	if err := os.Chown(SocketPath, -1, 29000); err != nil || os.Chmod(SocketPath, 0o660) != nil {
		return errors.New("protect provider credential relay socket")
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("accept provider credential relay request")
		}
		_ = serveConnection(ctx, input, connection, client)
		_ = connection.Close()
	}
}

func serveConnection(ctx context.Context, input model.Input, connection net.Conn, committer refreshCommitter) error {
	uid, err := peerUID(connection)
	if err != nil || !authorizedProviderUID(uid) {
		return errors.New("provider credential relay peer is unauthorized")
	}
	payload, err := decodeRequest(connection)
	if err != nil {
		return err
	}
	defer clear(payload.Refresh.Authentication)
	if err := validateRequest(input, payload); err != nil {
		return err
	}
	if err := committer.CommitProviderCredentialRefresh(ctx, input, payload.Refresh); err != nil {
		return errors.New("provider credential refresh callback failed")
	}
	if err := json.NewEncoder(connection).Encode(response{OK: true}); err != nil {
		return errors.New("write provider credential relay acknowledgement")
	}
	return nil
}

func decodeRequest(reader io.Reader) (request, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maximumRequestBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maximumRequestBytes {
		clear(raw)
		return request{}, errors.New("provider credential relay request exceeds its bound")
	}
	defer clear(raw)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload request
	if decoder.Decode(&payload) != nil || !decodeEOF(decoder) {
		clear(payload.Refresh.Authentication)
		return request{}, errors.New("provider credential relay request is invalid")
	}
	return payload, nil
}

func validateRequest(input model.Input, payload request) error {
	if payload.LeaseRef != input.LeaseRef || payload.Refresh.Validate() != nil ||
		payload.Refresh.RuntimeRevisionDigest != input.RuntimeRevisionDigest ||
		payload.Refresh.PreviousCredentialRevisionRef != input.ProviderCredentialRef ||
		payload.Refresh.PreviousContentSHA256 != input.ProviderCredentialSHA256 {
		return errors.New("provider credential relay binding is invalid")
	}
	return nil
}

func peerUID(connection net.Conn) (uint32, error) {
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return 0, errors.New("provider credential relay transport is invalid")
	}
	raw, err := unixConnection.SyscallConn()
	if err != nil {
		return 0, errors.New("inspect provider credential relay peer")
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil || controlErr != nil || credential == nil {
		return 0, errors.New("inspect provider credential relay peer")
	}
	return credential.Uid, nil
}

func authorizedProviderUID(uid uint32) bool {
	return uid == providerUID
}

func decodeEOF(decoder *json.Decoder) bool {
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

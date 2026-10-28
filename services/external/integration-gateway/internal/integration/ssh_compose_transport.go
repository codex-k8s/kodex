package integration

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"golang.org/x/crypto/ssh"
)

// SSHComposeRenewal obtains only a freshly committed permit for the same protected deployment.
// SSHComposeRenewal получает лишь fresh committed permit того же защищённого deployment.
type SSHComposeRenewal func(context.Context) ([]byte, error)

// RunSSHCompose performs the fixed forced command over an exact literal host and full host-key pin.
// RunSSHCompose выполняет fixed forced command через exact literal host и полный host-key pin.
func RunSSHCompose(ctx context.Context, target sshcompose.Target, key, permitJSON []byte, bundle *sshcompose.Bundle, renew SSHComposeRenewal) (sshcompose.Response, error) {
	return runSSHCompose(ctx, target, key, permitJSON, bundle, renew, (&net.Dialer{Timeout: 5 * time.Second}).DialContext)
}

// runSSHCompose keeps the production exact-address protocol while permitting an isolated loopback transport fixture.
// runSSHCompose сохраняет production exact-address протокол и позволяет изолированную loopback transport fixture.
func runSSHCompose(ctx context.Context, target sshcompose.Target, key, permitJSON []byte, bundle *sshcompose.Bundle, renew SSHComposeRenewal, dial func(context.Context, string, string) (net.Conn, error)) (sshcompose.Response, error) {
	if dial == nil || target.Validate() != nil || len(key) == 0 || len(key) > 16384 {
		return sshcompose.Response{}, sshcompose.ErrorInvalid
	}
	var permit sshcompose.SignedPermit
	if sshcompose.Decode(permitJSON, &permit, 16384) != nil {
		return sshcompose.Response{}, sshcompose.ErrorInvalid
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return sshcompose.Response{}, sshcompose.ErrorInvalid
	}
	public, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(target.HostKey))
	if err != nil || len(rest) != 0 || public.Type() != ssh.KeyAlgoED25519 {
		return sshcompose.Response{}, sshcompose.ErrorInvalid
	}
	address := net.JoinHostPort(target.Address, strconv.Itoa(target.Port))
	config := &ssh.ClientConfig{User: target.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, Timeout: 5 * time.Second, HostKeyAlgorithms: []string{ssh.KeyAlgoED25519},
		HostKeyCallback: func(hostname string, remote net.Addr, presented ssh.PublicKey) error {
			if hostname != address || remote.String() != address || subtle.ConstantTimeCompare(public.Marshal(), presented.Marshal()) != 1 {
				return sshcompose.ErrorInvalid
			}
			return nil
		}}
	connection, err := dial(ctx, "tcp", address)
	if err != nil {
		return sshcompose.Response{}, sshcompose.ErrorInvalid
	}
	defer connection.Close()
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = connection.Close(); close(closed) })
	defer func() {
		if !stop() {
			<-closed
		}
	}()
	if connection.SetDeadline(time.Now().Add(5*time.Second)) != nil {
		return sshcompose.Response{}, sshcompose.ErrorInvalid
	}
	secure, channels, requests, err := ssh.NewClientConn(connection, address, config)
	if err != nil {
		return sshcompose.Response{}, sshcompose.ErrorInvalid
	}
	client := ssh.NewClient(secure, channels, requests)
	defer client.Close()
	if connection.SetDeadline(time.Time{}) != nil {
		return sshcompose.Response{}, sshcompose.ErrorInvalid
	}
	session, err := client.NewSession()
	if err != nil {
		return sshcompose.Response{}, sshcompose.ErrorInvalid
	}
	defer session.Close()
	input, err := session.StdinPipe()
	if err != nil {
		return sshcompose.Response{}, sshcompose.ErrorInvalid
	}
	defer input.Close()
	output, err := session.StdoutPipe()
	if err != nil {
		return sshcompose.Response{}, sshcompose.ErrorInvalid
	}
	session.Stderr = io.Discard
	if session.Start("kodex-ssh-compose-v1") != nil {
		return sshcompose.Response{}, sshcompose.ErrorInvalid
	}
	if json.NewEncoder(input).Encode(sshcompose.Request{Permit: permit, Bundle: bundle}) != nil {
		return sshcompose.Response{}, &UnknownOutcomeError{}
	}
	renewContext, cancelRenew := context.WithCancel(ctx)
	defer cancelRenew()
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		if renew == nil {
			return
		}
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-renewContext.Done():
				return
			case <-ticker.C:
				bounded, cancel := context.WithTimeout(renewContext, 3*time.Second)
				raw, err := renew(bounded)
				cancel()
				var signed sshcompose.SignedPermit
				if err != nil || sshcompose.Decode(raw, &signed, 16384) != nil || json.NewEncoder(input).Encode(sshcompose.Request{Permit: signed}) != nil {
					_ = connection.Close()
					return
				}
			}
		}
	}()
	raw, readErr := io.ReadAll(io.LimitReader(output, 16385))
	cancelRenew()
	_ = input.Close()
	if readErr != nil || len(raw) > 16384 {
		_ = connection.Close()
	}
	<-renewDone
	if readErr != nil || session.Wait() != nil || ctx.Err() != nil {
		return sshcompose.Response{}, &UnknownOutcomeError{}
	}
	var response sshcompose.Response
	if sshcompose.Decode(bytes.TrimSpace(raw), &response, 16384) != nil || response.Version != sshcompose.ProtocolVersion || response.TargetRef != target.Ref || !response.Ready {
		return sshcompose.Response{}, &UnknownOutcomeError{}
	}
	return response, nil
}

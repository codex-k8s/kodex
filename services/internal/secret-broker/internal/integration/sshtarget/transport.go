package sshtarget

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/crypto/ssh"
)

// Client owns bounded SSH transport inside the broker; authorization, secret selection, lease renewal and recipe classification remain outside this private provider adapter.
// Client владеет bounded SSH transport внутри broker; authorization, выбор secret, lease renewal и recipe classification остаются вне этого приватного provider adapter.
type Client struct {
	dial func(context.Context, string, string) (net.Conn, error)
}

// Run executes one already owner-authorized command with a broker-held key, modern exact host pin and bounded combined output; it never opens PTY, forwarding, environment or agent channels.
// Run выполняет одну уже разрешённую owner команду с broker-held key, modern exact host pin и bounded combined output; PTY, forwarding, environment либо agent channels не открываются.
func (client Client) Run(ctx context.Context, target Target, privateKey []byte, command Command) (Result, error) {
	return client.run(ctx, target, privateKey, command, nil, nil)
}

// run keeps the low-level transport shared while allowing the qualified execute entrypoint to require current numeric identity on the same authenticated connection before the business command starts.
// run сохраняет общий low-level transport; qualified execute entrypoint требует current numeric identity на том же authenticated connection до старта business command.
func (client Client) run(ctx context.Context, target Target, privateKey []byte, command Command, identity *managedssh.TargetQualification, beforeStart func() error) (Result, error) {
	if ctx == nil || target.Validate() != nil || len(privateKey) == 0 || len(privateKey) > 16384 || len(command.Text) == 0 || len(command.Text) > managedssh.MaximumCommand || !utf8.ValidString(command.Text) || strings.ContainsRune(command.Text, 0) || len(command.Input) > 65536 || command.MaximumOutput < 1 || command.MaximumOutput > 32768 {
		return Result{}, ErrRejected
	}
	if identity != nil && (identity.Validate(time.Now().Unix()) != nil || identity.Mode != managedssh.ExecuteTransport || identity.Address != target.Address || identity.Port != target.Port || identity.User != target.User) {
		return Result{}, ErrRejected
	}
	command.Input = bytes.Clone(command.Input)
	defer clear(command.Input)
	pin, err := target.hostPin()
	if err != nil {
		return Result{}, ErrRejected
	}
	signer, err := authenticationSigner(privateKey)
	if err != nil {
		return Result{}, ErrRejected
	}
	operation, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	address := net.JoinHostPort(target.Address, strconv.Itoa(int(target.Port)))
	configuration := &ssh.ClientConfig{User: target.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyAlgorithms: pin.algorithms,
		HostKeyCallback: func(hostname string, remote net.Addr, presented ssh.PublicKey) error {
			if hostname != address || remote.String() != address || !pin.matches(presented) {
				return ErrRejected
			}
			return nil
		}}
	dial := client.dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	}
	connection, err := dial(operation, "tcp", address)
	if err != nil {
		return Result{}, &Failure{cause: err}
	}
	defer connection.Close()
	closed := make(chan struct{})
	stop := context.AfterFunc(operation, func() { _ = connection.Close(); close(closed) })
	defer func() {
		if !stop() {
			<-closed
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	if limit, exists := operation.Deadline(); exists && limit.Before(deadline) {
		deadline = limit
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return Result{}, &Failure{cause: err}
	}
	secure, channels, requests, err := ssh.NewClientConn(connection, address, configuration)
	if err != nil {
		return Result{}, &Failure{cause: err}
	}
	remote := ssh.NewClient(secure, channels, requests)
	defer func() { _ = remote.Close(); _ = remote.Wait() }()
	if err := connection.SetDeadline(time.Time{}); err != nil {
		return Result{}, &Failure{cause: err}
	}
	if identity != nil {
		if err := freshNumericIdentity(remote, connection.Close, *identity); err != nil {
			// Fixed read probes cannot dispatch the requested business command; their failure is definite no-effect for that invocation.
			// Fixed read probes не отправляют запрошенную business command; их failure означает definite no-effect этой invocation.
			return Result{}, ErrRejected
		}
		if identity.ValidUntil <= time.Now().Unix() || operation.Err() != nil {
			return Result{}, ErrRejected
		}
	}
	session, err := remote.NewSession()
	if err != nil {
		return Result{}, &Failure{cause: err}
	}
	defer session.Close()
	capture := &outputCapture{maximum: command.MaximumOutput, close: connection.Close}
	session.Stdout = captureWriter{capture: capture}
	session.Stderr = captureWriter{capture: capture, stderr: true}
	session.Stdin = bytes.NewReader(command.Input)
	if beforeStart != nil {
		if err := beforeStart(); err != nil {
			return Result{}, ErrRejected
		}
	}
	if operation.Err() != nil || identity != nil && identity.ValidUntil <= time.Now().Unix() {
		return Result{}, ErrRejected
	}
	// Start transmits the command before its acknowledgement; even a Start error can follow a completed external effect.
	// Start передаёт команду до получения acknowledgement; даже ошибка Start может следовать за завершённым внешним эффектом.
	if err := session.Start(command.Text); err != nil {
		return Result{}, &Failure{cause: err, unknown: true}
	}
	err = session.Wait()
	result := capture.result()
	if containsPrivateKey(result.Stdout, privateKey) || containsPrivateKey(result.Stderr, privateKey) {
		return Result{}, &Failure{cause: ErrRejected, unknown: true}
	}
	if operation.Err() != nil {
		return result, &Failure{cause: operation.Err(), unknown: true}
	}
	if result.Truncated {
		return result, &Failure{cause: ErrOutputLimit, unknown: true}
	}
	if err == nil {
		result.SessionExited = true
		return result, nil
	}
	var exit *ssh.ExitError
	if errors.As(err, &exit) && exit.ExitStatus() >= 0 && exit.ExitStatus() <= 255 {
		result.ExitStatus = exit.ExitStatus()
		result.SessionExited = true
		return result, nil
	}
	return result, &Failure{cause: err, unknown: true}
}

// authenticationSigner limits user authentication signatures as well as host keys; a legacy server cannot make an RSA credential fall back to SHA-1.
// authenticationSigner ограничивает user authentication signatures наряду с host keys; legacy server не может переключить RSA credential на SHA-1.
func authenticationSigner(privateKey []byte) (ssh.Signer, error) {
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, ErrRejected
	}
	algorithmSigner, supported := signer.(ssh.AlgorithmSigner)
	algorithms := hostKeyAlgorithms(signer.PublicKey())
	if !supported || len(algorithms) == 0 {
		return nil, ErrRejected
	}
	restricted, err := ssh.NewSignerWithAlgorithms(algorithmSigner, algorithms)
	if err != nil {
		return nil, ErrRejected
	}
	return restricted, nil
}

// hostKeyAlgorithms permits exact modern raw host/signing keys and RSA SHA-2 only; certificates, DSA, SHA-1 and undersized RSA fail closed.
// hostKeyAlgorithms допускает exact modern raw host/signing keys и только RSA SHA-2; certificates, DSA, SHA-1 и короткий RSA закрыто отклоняются.
func hostKeyAlgorithms(public ssh.PublicKey) []string {
	switch public.Type() {
	case ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		return []string{public.Type()}
	case ssh.KeyAlgoRSA:
		if key, ok := public.(ssh.CryptoPublicKey); ok {
			if rsaKey, ok := key.CryptoPublicKey().(*rsa.PublicKey); ok && rsaKey.N.BitLen() >= 3072 && rsaKey.N.BitLen() <= 8192 {
				return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
			}
		}
	}
	return nil
}

// containsPrivateKey refuses selected-key bytes, canonical encodings, private PEM lines and private-key markers before a result leaves broker custody; it is no authority to publish other remote secrets.
// containsPrivateKey запрещает selected-key bytes, canonical encodings, private PEM lines и private-key markers до выхода результата из broker custody; это не полномочие публиковать другие remote secrets.
func containsPrivateKey(output, privateKey []byte) bool {
	if bytes.Contains(output, []byte("PRIVATE KEY-----")) {
		return true
	}
	for _, encoded := range []string{string(privateKey), base64.StdEncoding.EncodeToString(privateKey), base64.RawStdEncoding.EncodeToString(privateKey), base64.URLEncoding.EncodeToString(privateKey), hex.EncodeToString(privateKey)} {
		if bytes.Contains(output, []byte(encoded)) {
			return true
		}
	}
	for _, line := range bytes.Fields(privateKey) {
		if len(line) >= 24 && bytes.Contains(output, line) {
			return true
		}
	}
	return false
}

package sshtarget

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// fixtureBehavior selects wire outcomes of a real isolated SSH protocol peer; it never changes host accounts, firewall or SSH configuration.
// fixtureBehavior выбирает wire outcomes настоящего изолированного SSH peer; host accounts, firewall и SSH config не меняются.
type fixtureBehavior struct {
	execute                  func([]byte) fixtureExecutionResult
	command                  string
	stdout                   []byte
	stderr                   []byte
	exit                     uint32
	missing                  bool
	lostACK                  bool
	hold                     <-chan struct{}
	commands                 atomic.Int32
	publicKey                atomic.Int32
	input                    chan []byte
	echoSelectedKey          bool
	encodedEcho              bool
	hostKey                  ssh.Signer
	additionalHostKeys       []ssh.Signer
	authenticationKey        []byte
	authenticationAlgorithms []string
	identityReads            map[string]fixtureProbeReply
	identityCalls            atomic.Int32
	connections              atomic.Int32
}

// fixtureExecutionResult carries one joined test process result or a deliberate lost response after its real effect; it supplies no owner authority.
// fixtureExecutionResult содержит результат joined test process либо deliberate lost response после actual effect; owner authority не выдаётся.
type fixtureExecutionResult struct {
	stdout, stderr []byte
	exit           uint32
	dropResponse   bool
}

// fixtureProbeReply selects only a fixed identity probe's encrypted response and failure mode; it never interprets caller input or supplies business command authority.
// fixtureProbeReply выбирает encrypted response и failure mode только fixed identity probe; caller input он не интерпретирует и business command authority не выдаёт.
type fixtureProbeReply struct {
	stdout  []byte
	stderr  []byte
	exit    uint32
	missing bool
	lostACK bool
	hold    <-chan struct{}
	started chan<- struct{}
}

// fixtureAddressConn maps only the test peer's observed endpoint to a synthetic literal target while retaining real encrypted loopback SSH traffic.
// fixtureAddressConn отображает лишь наблюдаемый endpoint test peer в synthetic literal target, сохраняя настоящий encrypted loopback SSH traffic.
type fixtureAddressConn struct {
	net.Conn
	remote net.Addr
}

// RemoteAddr exposes the synthetic exact target identity expected by the production host-key boundary; this wrapper is compiled only into tests.
// RemoteAddr открывает synthetic exact target identity для production host-key boundary; wrapper существует только в tests.
func (connection fixtureAddressConn) RemoteAddr() net.Addr { return connection.remote }

// fixturePrivateKey creates an ephemeral nonpersistent Ed25519 authentication key and returns canonical public/PEM forms without printing either value.
// fixturePrivateKey создаёт ephemeral непостоянный Ed25519 authentication key и возвращает canonical public/PEM forms, не печатая значения.
func fixturePrivateKey(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("generate isolated SSH key")
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal("create isolated SSH signer")
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal("encode isolated SSH key")
	}
	return signer, pem.EncodeToMemory(block)
}

// newWireFixture owns a complete loopback SSH server lifetime and exact-key authentication; cleanup closes and joins every accepted connection and handler.
// newWireFixture владеет полным lifetime loopback SSH server и exact-key authentication; cleanup закрывает и join все принятые соединения и handlers.
func newWireFixture(t *testing.T, behavior *fixtureBehavior) (Client, Target, []byte) {
	t.Helper()
	host, _ := fixturePrivateKey(t)
	if behavior.hostKey != nil {
		host = behavior.hostKey
	}
	user, private := fixturePrivateKey(t)
	if len(behavior.authenticationKey) != 0 {
		clear(private)
		private = append([]byte(nil), behavior.authenticationKey...)
		var err error
		user, err = ssh.ParsePrivateKey(private)
		if err != nil {
			t.Fatal("parse isolated authentication fixture")
		}
	}
	if behavior.echoSelectedKey {
		behavior.stdout = append([]byte(nil), private...)
		if behavior.encodedEcho {
			behavior.stdout = []byte(base64.StdEncoding.EncodeToString(private))
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("listen isolated SSH server")
	}
	target := Target{Address: "192.0.2.10", Port: 22, User: "fixture", HostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(host.PublicKey())))}
	configuration := &ssh.ServerConfig{PublicKeyCallback: func(metadata ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		behavior.publicKey.Add(1)
		if metadata.User() != target.User || string(key.Marshal()) != string(user.PublicKey().Marshal()) {
			return nil, ErrRejected
		}
		return &ssh.Permissions{}, nil
	}}
	configuration.AddHostKey(host)
	for _, additional := range behavior.additionalHostKeys {
		configuration.AddHostKey(additional)
	}
	configuration.PublicKeyAuthAlgorithms = behavior.authenticationAlgorithms
	var handlers sync.WaitGroup
	var mutex sync.Mutex
	var connections []net.Conn
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			mutex.Lock()
			connections = append(connections, connection)
			mutex.Unlock()
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer connection.Close()
				server, channels, requests, err := ssh.NewServerConn(connection, configuration)
				if err != nil {
					return
				}
				behavior.connections.Add(1)
				defer server.Close()
				discarded := make(chan struct{})
				go func() { ssh.DiscardRequests(requests); close(discarded) }()
				defer func() { _ = server.Close(); <-discarded }()
				for next := range channels {
					if next.ChannelType() != "session" {
						_ = next.Reject(ssh.Prohibited, "channel rejected")
						continue
					}
					channel, calls, err := next.Accept()
					if err != nil {
						return
					}
					serveFixtureSession(channel, calls, server, behavior)
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-accepted
		mutex.Lock()
		for _, connection := range connections {
			_ = connection.Close()
		}
		mutex.Unlock()
		handlers.Wait()
		clear(private)
	})
	client := Client{dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != "192.0.2.10:22" {
			return nil, ErrRejected
		}
		connection, err := (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
		if err != nil {
			return nil, err
		}
		return fixtureAddressConn{Conn: connection, remote: &net.TCPAddr{IP: net.ParseIP(target.Address), Port: int(target.Port)}}, nil
	}}
	return client, target, private
}

// serveFixtureSession handles only one exec request and produces deliberate ACK-loss/overflow/exit evidence without interpreting a shell string.
// serveFixtureSession обрабатывает лишь один exec request и формирует deliberate ACK-loss/overflow/exit evidence, не интерпретируя shell string.
func serveFixtureSession(channel ssh.Channel, calls <-chan *ssh.Request, server *ssh.ServerConn, behavior *fixtureBehavior) {
	defer channel.Close()
	expected := behavior.command
	if expected == "" {
		expected = "fixture-command"
	}
	for request := range calls {
		var command struct{ Command string }
		if request.Type != "exec" || ssh.Unmarshal(request.Payload, &command) != nil {
			_ = request.Reply(false, nil)
			continue
		}
		if probe, ok := behavior.identityReads[command.Command]; ok {
			behavior.identityCalls.Add(1)
			if probe.lostACK {
				_ = server.Close()
				return
			}
			_ = request.Reply(true, nil)
			if probe.started != nil {
				probe.started <- struct{}{}
			}
			if probe.hold != nil {
				<-probe.hold
			}
			_, _ = channel.Write(probe.stdout)
			_, _ = channel.Stderr().Write(probe.stderr)
			if !probe.missing {
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{probe.exit}))
			}
			return
		}
		if command.Command != expected {
			_ = request.Reply(false, nil)
			continue
		}
		behavior.commands.Add(1)
		if behavior.lostACK {
			_ = server.Close()
			return
		}
		_ = request.Reply(true, nil)
		if behavior.hold != nil {
			<-behavior.hold
		}
		input, _ := io.ReadAll(io.LimitReader(channel, 65537))
		if behavior.input != nil {
			behavior.input <- input
		}
		if behavior.execute != nil {
			result := behavior.execute(input)
			clear(input)
			defer clear(result.stdout)
			defer clear(result.stderr)
			if result.dropResponse {
				_ = server.Close()
				return
			}
			_, _ = channel.Write(result.stdout)
			_, _ = channel.Stderr().Write(result.stderr)
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{result.exit}))
			return
		}
		_, _ = channel.Write(behavior.stdout)
		_, _ = channel.Stderr().Write(behavior.stderr)
		if !behavior.missing {
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{behavior.exit}))
		}
		return
	}
}

// TestRunPreservesExactInputAndSessionExit checks genuine encrypted SSH, exact-key auth, stdin bytes and both output streams without claiming remote process-tree join.
// TestRunPreservesExactInputAndSessionExit проверяет настоящий encrypted SSH, exact-key auth, stdin bytes и оба output streams, не утверждая remote process-tree join.
func TestRunPreservesExactInputAndSessionExit(t *testing.T) {
	behavior := &fixtureBehavior{stdout: []byte("output"), stderr: []byte("diagnostic"), exit: 7, input: make(chan []byte, 1)}
	client, target, private := newWireFixture(t, behavior)
	result, err := client.Run(t.Context(), target, private, Command{Text: "fixture-command", Input: []byte("exact input"), MaximumOutput: 32768})
	if err != nil || !result.SessionExited || result.ExitStatus != 7 || string(result.Stdout) != "output" || string(result.Stderr) != "diagnostic" || result.Truncated || string(<-behavior.input) != "exact input" || behavior.commands.Load() != 1 {
		t.Fatal("SSH session evidence lost exact input or bounded output")
	}
}

// TestStartLostACKRemainsUnknown prevents a known-no-effect classification when the peer executes before acknowledging exec.
// TestStartLostACKRemainsUnknown запрещает known-no-effect классификацию, когда peer исполнил команду до exec acknowledgement.
func TestStartLostACKRemainsUnknown(t *testing.T) {
	behavior := &fixtureBehavior{lostACK: true}
	client, target, private := newWireFixture(t, behavior)
	result, err := client.Run(t.Context(), target, private, Command{Text: "fixture-command", MaximumOutput: 32768})
	if !errors.Is(err, ErrUnknown) || result.SessionExited || behavior.commands.Load() != 1 || err.Error() != ErrUnknown.Error() {
		t.Fatal("lost exec acknowledgement did not preserve one ambiguous effect")
	}
}

// TestWrongHostKeyFailsBeforeUserAuthentication proves no credential authentication or command occurs after a mismatched exact host pin.
// TestWrongHostKeyFailsBeforeUserAuthentication доказывает отсутствие credential authentication либо команды после mismatch exact host pin.
func TestWrongHostKeyFailsBeforeUserAuthentication(t *testing.T) {
	behavior := &fixtureBehavior{}
	client, target, private := newWireFixture(t, behavior)
	other, _ := fixturePrivateKey(t)
	target.HostKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(other.PublicKey())))
	_, err := client.Run(t.Context(), target, private, Command{Text: "fixture-command", MaximumOutput: 32768})
	if !errors.Is(err, ErrRejected) || errors.Is(err, ErrUnknown) || behavior.publicKey.Load() != 0 || behavior.commands.Load() != 0 {
		t.Fatal("host-key mismatch crossed authentication boundary")
	}
}

// TestOutputOverflowAndMissingExitStayUnknown distinguishes bounded response capture from proof that the remote effect stopped.
// TestOutputOverflowAndMissingExitStayUnknown отличает bounded response capture от доказательства остановки remote effect.
func TestOutputOverflowAndMissingExitStayUnknown(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(strconv.FormatBool(missing), func(t *testing.T) {
			behavior := &fixtureBehavior{stdout: []byte("123456"), stderr: []byte("abcdef"), missing: missing}
			client, target, private := newWireFixture(t, behavior)
			maximum := 10
			if missing {
				maximum = 100
			}
			result, err := client.Run(t.Context(), target, private, Command{Text: "fixture-command", MaximumOutput: maximum})
			if !errors.Is(err, ErrUnknown) || result.SessionExited || len(result.Stdout)+len(result.Stderr) > maximum || result.Truncated == missing || behavior.commands.Load() != 1 {
				t.Fatal("malformed or overflowed SSH result became completed")
			}
		})
	}
}

// TestCancellationDoesNotClaimRemoteJoin verifies cancellation closes local transport while a peer-side action can still outlive the SSH channel.
// TestCancellationDoesNotClaimRemoteJoin проверяет закрытие local transport при cancel, хотя действие peer может пережить SSH channel.
func TestCancellationDoesNotClaimRemoteJoin(t *testing.T) {
	release := make(chan struct{})
	behavior := &fixtureBehavior{hold: release}
	client, target, private := newWireFixture(t, behavior)
	var releaseOnce sync.Once
	releasePeer := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releasePeer)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	joined := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		releasePeer()
		select {
		case <-joined:
		case <-time.After(2 * time.Second):
			t.Error("cancelled SSH fixture caller did not join")
		}
	})
	go func() {
		defer close(joined)
		_, err := client.Run(ctx, target, private, Command{Text: "fixture-command", MaximumOutput: 32768})
		done <- err
	}()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for behavior.commands.Load() == 0 {
		select {
		case <-timer.C:
			cancel()
			releasePeer()
			t.Fatal("isolated exec was not dispatched")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrUnknown) {
			releasePeer()
			t.Fatal("cancelled dispatched command lost ambiguity")
		}
	case <-time.After(2 * time.Second):
		releasePeer()
		t.Fatal("cancelled local SSH transport did not join")
	}
	releasePeer()
}

// TestSelectedPrivateKeyEchoNeverLeavesBroker verifies encoded and plain selected-key output is withheld even after a successful remote exit status.
// TestSelectedPrivateKeyEchoNeverLeavesBroker проверяет удержание encoded и plain selected-key output даже после успешного remote exit status.
func TestSelectedPrivateKeyEchoNeverLeavesBroker(t *testing.T) {
	for _, encoded := range []bool{false, true} {
		t.Run(strconv.FormatBool(encoded), func(t *testing.T) {
			behavior := &fixtureBehavior{echoSelectedKey: true, encodedEcho: encoded}
			client, target, private := newWireFixture(t, behavior)
			result, err := client.Run(t.Context(), target, private, Command{Text: "fixture-command", MaximumOutput: 32768})
			if !errors.Is(err, ErrUnknown) || len(result.Stdout) != 0 || len(result.Stderr) != 0 || result.SessionExited {
				t.Fatal("selected private-key echo escaped broker custody")
			}
		})
	}
}

// TestRSAAuthenticationRejectsSHA1Fallback exercises real client/server negotiation rather than merely checking an allowed-algorithm slice.
// TestRSAAuthenticationRejectsSHA1Fallback проверяет настоящую client/server negotiation, а не только содержимое allowed-algorithm slice.
func TestRSAAuthenticationRejectsSHA1Fallback(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal("generate isolated RSA authentication key")
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal("encode isolated RSA authentication key")
	}
	raw := pem.EncodeToMemory(block)
	defer clear(raw)
	for _, algorithm := range []string{ssh.KeyAlgoRSA, ssh.KeyAlgoRSASHA256} {
		t.Run(algorithm, func(t *testing.T) {
			behavior := &fixtureBehavior{authenticationKey: raw, authenticationAlgorithms: []string{algorithm}}
			client, target, selected := newWireFixture(t, behavior)
			result, err := client.Run(t.Context(), target, selected, Command{Text: "fixture-command", MaximumOutput: 32768})
			if algorithm == ssh.KeyAlgoRSA {
				if !errors.Is(err, ErrRejected) || errors.Is(err, ErrUnknown) || behavior.commands.Load() != 0 {
					t.Fatal("RSA authentication used a legacy SHA-1 fallback")
				}
			} else if err != nil || !result.SessionExited || behavior.commands.Load() != 1 {
				t.Fatal("modern RSA SHA-2 authentication failed")
			}
		})
	}
}

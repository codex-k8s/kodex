package integration

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"golang.org/x/crypto/ssh"
)

// sshFixtureConnection preserves the owner's logical exact address over an isolated loopback socket.
// sshFixtureConnection сохраняет owner logical exact address поверх изолированного loopback socket.
type sshFixtureConnection struct {
	net.Conn
	remote net.Addr
}

// RemoteAddr exposes only the synthetic target; the private dial fixture verifies the requested destination first.
// RemoteAddr выдаёт лишь synthetic target, а private dial fixture прежде проверяет запрошенное назначение.
func (connection sshFixtureConnection) RemoteAddr() net.Addr { return connection.remote }

// sshFixtureRunner counts readiness admission and refuses all mutation commands.
// sshFixtureRunner учитывает readiness admission и отклоняет все mutation commands.
type sshFixtureRunner struct {
	inspected, writes atomic.Int32
	allowWrite        bool
}

// Rootless models admitted Docker readiness without Docker, host operations or external connections.
// Rootless моделирует Docker readiness без Docker, host operations и внешних подключений.
func (runner *sshFixtureRunner) Rootless(context.Context) bool { runner.inspected.Add(1); return true }

// Run models only the fixed compose command in memory; no subprocess or Docker is started.
// Run моделирует только fixed compose command в памяти; subprocess и Docker не запускаются.
func (runner *sshFixtureRunner) Run(_ context.Context, args []string) error {
	if !runner.allowWrite || len(args) != 14 || args[0] != "docker" || args[1] != "compose" || args[6] != "up" || args[7] != "--no-build" {
		return sshcompose.ErrorInvalid
	}
	runner.writes.Add(1)
	return nil
}

// TestSSHComposeTransportPinsIdentityAndJoinsCancellation exercises real SSH framing with synthetic keys over loopback.
// TestSSHComposeTransportPinsIdentityAndJoinsCancellation проверяет реальный SSH framing с synthetic keys через loopback.
func TestSSHComposeTransportPinsIdentityAndJoinsCancellation(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong-host-key", "wrong-user", "wrong-target-response", "oversized-response", "lost-response", "cancel"} {
		t.Run(scenario, func(t *testing.T) { runSSHComposeTransportScenario(t, scenario, "INSPECT") })
	}
}

// runSSHComposeTransportScenario exercises the production SSH framing and helper with isolated keys and no real host or Docker effect.
// runSSHComposeTransportScenario проверяет production SSH framing и helper с isolated keys без настоящего host/Docker effect.
func runSSHComposeTransportScenario(t *testing.T, scenario, purpose string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, hostPrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(hostPrivate)
	host, err := ssh.NewSignerFromKey(hostPrivate)
	if err != nil {
		t.Fatal(err)
	}
	_, clientPrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(clientPrivate)
	client, err := ssh.NewSignerFromKey(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	key := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	defer clear(key)
	clear(privateDER)
	ownerPublic, ownerPrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(ownerPrivate)
	target := sshcompose.Target{Ref: "target_fixture", ProjectRef: "project_fixture", Repository: "example/project", Address: "192.0.2.10", Port: 22, User: "deployer", HostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(host.PublicKey()))), PublishedPort: 8080, RuntimeImage: "docker.io/library/python@sha256:" + strings.Repeat("a", 64)}
	now := time.Now()
	permit := sshcompose.Permit{Version: sshcompose.ProtocolVersion, Purpose: purpose, Target: target, InvocationRef: "invocation_fixture", RootRunRef: "root_fixture", SessionRef: "session_fixture", TurnRef: "turn_fixture", Attempt: 1, LeaseRef: "lease_fixture", Generation: 1, EffectKey: "effect_fixture", InputDigest: strings.Repeat("b", 64), IssuedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(), Deadline: now.Add(time.Minute).Unix()}

	var bundle *sshcompose.Bundle
	if purpose != "INSPECT" {
		manifest, err := json.Marshal(sshcompose.Manifest{Image: target.RuntimeImage, Command: []string{"python", "server.py"}, ContainerPort: 8080, HealthPath: "/health"})
		if err != nil {
			t.Fatal(err)
		}
		files := []sshcompose.File{}
		for path, raw := range map[string][]byte{"kodex-deploy.json": manifest, "server.py": []byte("print('isolated fixture')\n")} {
			files = append(files, sshcompose.File{Path: path, Mode: "100644", BlobSHA: sshcompose.GitObjectID("blob", raw), Content: base64.StdEncoding.EncodeToString(raw)})
		}
		value := sshcompose.Bundle{Version: 1, Repository: target.Repository, MergedSHA: strings.Repeat("c", 40), TreeSHA: sshcompose.TreeID(files), Files: sshcompose.SortedFiles(files)}
		raw, _, err := value.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		permit.DeploymentRef = "deployment_fixture"
		if purpose == "DEPLOY" {
			permit.DeploymentRef = permit.InvocationRef
		}
		permit.DeploymentInputDigest = permit.InputDigest
		permit.MergedSHA = value.MergedSHA
		permit.TreeSHA = value.TreeSHA
		permit.BundleSHA256 = sshcompose.Digest(raw)
		if purpose == "DEPLOY" {
			bundle = &value
		}
	}
	signed, err := sshcompose.Sign(permit, ownerPrivate, now)
	if err != nil {
		t.Fatal(err)
	}
	rawPermit, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	runner := &sshFixtureRunner{allowWrite: purpose == "DEPLOY"}
	helper := sshcompose.Helper{Target: target, RootDirectory: t.TempDir(), OwnerKeys: map[string]ed25519.PublicKey{sshcompose.Digest(ownerPublic): ownerPublic}, Runner: runner}

	helper.Health = func(_ context.Context, port int, path string) bool {
		return port == target.PublishedPort && path == "/health"
	}
	if purpose == "STATUS" {
		journal, err := sshcompose.OpenJournal(helper.RootDirectory + "/journal")
		if err != nil {
			t.Fatal(err)
		}
		deployPermit := permit
		deployPermit.Purpose = "DEPLOY"
		deployPermit.InvocationRef = deployPermit.DeploymentRef
		receipt, fresh, err := journal.Reserve(deployPermit, target.RuntimeImage)
		if err != nil || !fresh {
			t.Fatal("seed native durable journal")
		}
		if _, err = journal.Complete(receipt, "SUCCEEDED", "", true); err != nil {
			t.Fatal(err)
		}
		if err = journal.Close(); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		defer close(serverDone)
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
		configuration := &ssh.ServerConfig{PublicKeyCallback: func(metadata ssh.ConnMetadata, presented ssh.PublicKey) (*ssh.Permissions, error) {
			if metadata.User() != "deployer" || string(presented.Marshal()) != string(client.PublicKey().Marshal()) {
				return nil, sshcompose.ErrorInvalid
			}
			return nil, nil
		}}
		configuration.AddHostKey(host)
		secure, channels, requests, err := ssh.NewServerConn(connection, configuration)
		if err != nil {
			serverDone <- err
			return
		}
		defer secure.Close()
		requestDone := make(chan struct{})
		go func() { defer close(requestDone); ssh.DiscardRequests(requests) }()
		defer func() { _ = secure.Close(); <-requestDone }()
		channelRequest, ok := <-channels
		if !ok || channelRequest.ChannelType() != "session" {
			serverDone <- sshcompose.ErrorInvalid
			return
		}
		channel, commands, err := channelRequest.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer channel.Close()
		request, ok := <-commands
		var command struct{ Value string }
		if !ok || request.Type != "exec" || ssh.Unmarshal(request.Payload, &command) != nil || command.Value != "kodex-ssh-compose-v1" {
			serverDone <- sshcompose.ErrorInvalid
			return
		}
		if err = request.Reply(true, nil); err != nil {
			serverDone <- err
			return
		}
		switch scenario {
		case "cancel":
			_, err = io.Copy(io.Discard, channel)
		case "oversized-response":
			_, err = channel.Write([]byte(strings.Repeat("x", 17000)))
		case "wrong-target-response":
			err = json.NewEncoder(channel).Encode(sshcompose.Response{Version: sshcompose.ProtocolVersion, TargetRef: "target_foreign", Ready: true})
		case "lost-response":
			err = helper.Serve(ctx, io.NopCloser(channel), io.Discard)
		default:
			if purpose == "DEPLOY" {
				// Forced-command stdin — отдельный pipe. Close прерывает reader, не закрывая stdout SSH channel.
				stdin, writer := io.Pipe()
				copied := make(chan struct{})
				go func() { defer close(copied); defer writer.Close(); _, _ = io.Copy(writer, channel) }()
				defer func() { _ = channel.Close(); _ = secure.Close(); <-copied }()
				err = helper.Serve(ctx, stdin, channel)
			} else {
				err = helper.Serve(ctx, io.NopCloser(channel), channel)
			}
		}
		_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		_ = channel.CloseWrite()
		serverDone <- err
	}()
	defer func() {
		_ = listener.Close()
		cancel()
		for range serverDone {
		}
	}()
	if scenario == "wrong-host-key" {
		wrong, err := ssh.NewPublicKey(clientPrivate.Public())
		if err != nil {
			t.Fatal(err)
		}
		target.HostKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(wrong)))
	}
	if scenario == "wrong-user" {
		target.User = "foreign"
	}
	callContext := ctx
	if scenario == "cancel" {
		var stop context.CancelFunc
		callContext, stop = context.WithTimeout(ctx, 50*time.Millisecond)
		defer stop()
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != "192.0.2.10:22" {
			return nil, sshcompose.ErrorInvalid
		}
		connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
		if err != nil {
			return nil, err
		}
		return sshFixtureConnection{Conn: connection, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 22}}, nil
	}
	response, err := runSSHCompose(callContext, target, key, rawPermit, bundle, nil, dial)
	if scenario == "valid" {
		expectedInspections := int32(1)
		if purpose == "STATUS" {
			expectedInspections = 0
		}
		expectedWrites := int32(0)
		if purpose == "DEPLOY" {
			expectedWrites = 1
		}
		if err != nil || !response.Ready || runner.inspected.Load() != expectedInspections || runner.writes.Load() != expectedWrites {
			t.Fatalf("isolated SSH helper failed: %v", err)
		}

		if purpose != "INSPECT" && (response.Receipt == nil || response.Receipt.State != "SUCCEEDED" || !response.Receipt.HealthOK || response.Receipt.MergedSHA != permit.MergedSHA || response.Receipt.BundleSHA256 != permit.BundleSHA256) {
			t.Fatal("typed SSH operation lost exact durable receipt")
		}
	} else if err == nil {
		t.Fatal("invalid SSH path accepted")
	}
	if (scenario == "wrong-host-key" || scenario == "wrong-user") && runner.inspected.Load() != 0 {
		t.Fatal("identity failure reached helper")
	}
}

// TestSSHComposeTypedCatalog exercises every shipped SSH capability on the real
// isolated SSH/helper protocol; readiness and health are synthetic, no Docker runs.
// TestSSHComposeTypedCatalog проверяет каждую shipped SSH capability через настоящий
// isolated SSH/helper protocol; readiness и health синтетические, Docker не запускается.
func TestSSHComposeTypedCatalog(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition, found := definitions["ssh-compose"]
	if !found || len(definition.Spec.Capabilities) != 3 {
		t.Fatal("closed SSH package missing")
	}
	for _, capability := range definition.Spec.Capabilities {
		t.Run(capability.Operation, func(t *testing.T) { testSSHComposeCatalogOperation(t, definition, capability) })
	}
}

// testSSHComposeCatalogOperation rejects drifted public registration before running the typed private consumer protocol.
// testSSHComposeCatalogOperation отклоняет изменённую public registration до запуска typed private consumer protocol.
func testSSHComposeCatalogOperation(t *testing.T, definition integrationpackage.Package, capability integrationpackage.Capability) {
	t.Helper()
	purpose := map[string]string{"ssh.compose.inspect": "INSPECT", "ssh.compose.deploy": "DEPLOY", "ssh.compose.status": "STATUS"}[capability.Operation]
	if definition.Metadata.Key != "ssh-compose" || definition.Spec.Adapter != "SSH_COMPOSE" || capability.Key != capability.Operation || purpose == "" || (purpose == "DEPLOY" && (capability.Risk != "WRITE" || capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" || capability.Execution.LeaseProfile != "SSH_COMPOSE_V1")) || (purpose != "DEPLOY" && (capability.Risk != "READ" || capability.ApprovalPolicy != "NONE")) {
		t.Fatal("SSH catalog typed route changed")
	}
	runSSHComposeTransportScenario(t, "valid", purpose)
}

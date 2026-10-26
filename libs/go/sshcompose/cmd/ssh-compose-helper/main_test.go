package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
)

// protocolFixtureRunner models one fixed in-memory Compose effect, never a subprocess or host mutation.
// protocolFixtureRunner моделирует один fixed Compose effect в памяти без subprocess либо host mutation.
type protocolFixtureRunner struct{}

// Rootless supplies only synthetic readiness for the closed child-process fixture.
// Rootless задаёт только synthetic readiness закрытой child-process оснастки.
func (protocolFixtureRunner) Rootless(context.Context) bool { return true }

// Run accepts only the immutable helper's exact command shape and starts no Docker.
// Run принимает только immutable command helper и не запускает Docker.
func (protocolFixtureRunner) Run(_ context.Context, args []string) error {
	if len(args) != 14 || args[0] != "docker" || args[1] != "compose" || args[6] != "up" || args[7] != "--no-build" {
		return sshcompose.ErrorInvalid
	}
	return nil
}

// protocolFixture builds deterministic synthetic public pins and signed bytes without external credentials.
// protocolFixture создаёт deterministic synthetic public pins и signed bytes без внешних credentials.
func protocolFixture(t *testing.T) (sshcompose.Helper, sshcompose.Request) {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32))
	defer clear(private)
	public := private.Public().(ed25519.PublicKey)
	host := append([]byte{0, 0, 0, 11}, []byte("ssh-ed25519")...)
	host = append(host, 0, 0, 0, 32)
	host = append(host, public...)
	target := sshcompose.Target{Ref: "ssh_stdin_fixture", ProjectRef: "prj_stdin_fixture", Repository: "example/project", Address: "192.0.2.10", Port: 22, User: "deployer", HostKey: "ssh-ed25519 " + base64.StdEncoding.EncodeToString(host), PublishedPort: 8080, RuntimeImage: "docker.io/library/python@sha256:" + strings.Repeat("a", 64)}
	manifest, err := json.Marshal(sshcompose.Manifest{Image: target.RuntimeImage, Command: []string{"python", "server.py"}, ContainerPort: 8080, HealthPath: "/health"})
	if err != nil {
		t.Fatal(err)
	}
	files := []sshcompose.File{}
	for name, raw := range map[string][]byte{"kodex-deploy.json": manifest, "server.py": []byte("print('synthetic')\n")} {
		files = append(files, sshcompose.File{Path: name, Mode: "100644", BlobSHA: sshcompose.GitObjectID("blob", raw), Content: base64.StdEncoding.EncodeToString(raw)})
	}
	bundle := sshcompose.Bundle{Version: 1, Repository: target.Repository, MergedSHA: strings.Repeat("b", 40), TreeSHA: sshcompose.TreeID(files), Files: sshcompose.SortedFiles(files)}
	raw, _, err := bundle.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	permit := sshcompose.Permit{Version: 1, Purpose: "DEPLOY", Target: target, InvocationRef: "inv_stdin_fixture", DeploymentRef: "inv_stdin_fixture", RootRunRef: "run_stdin_fixture", SessionRef: "ses_stdin_fixture", TurnRef: "trn_stdin_fixture", Attempt: 1, LeaseRef: "lea_stdin_fixture", Generation: 1, EffectKey: "eff_stdin_fixture", InputDigest: strings.Repeat("c", 64), DeploymentInputDigest: strings.Repeat("c", 64), MergedSHA: bundle.MergedSHA, TreeSHA: bundle.TreeSHA, BundleSHA256: sshcompose.Digest(raw), IssuedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(), Deadline: now.Add(time.Minute).Unix()}
	signed, err := sshcompose.Sign(permit, private, now)
	if err != nil {
		t.Fatal(err)
	}
	helper := sshcompose.Helper{Target: target, OwnerKeys: map[string]ed25519.PublicKey{sshcompose.Digest(public): public}, RootDirectory: t.TempDir(), Runner: protocolFixtureRunner{}, Health: func(context.Context, int, string) bool { return true }}
	return helper, sshcompose.Request{Permit: signed, Bundle: &bundle}
}

// TestProtocolInputCompletesBeforePeerEOF runs the actual helper input adapter in a
// child process with an OS pipe held open by its parent until the child has exited.
// TestProtocolInputCompletesBeforePeerEOF запускает настоящий input adapter helper
// в child process с OS pipe, который parent не закрывает до завершения child.
func TestProtocolInputCompletesBeforePeerEOF(t *testing.T) {
	_, request := protocolFixture(t)
	runProtocolInputFixture(t, "DEPLOY", &request)
}

// TestProtocolInputCancelBeforeFrame proves that caller cancellation joins the
// actual OS-pipe reader while its peer has sent no frame and has not closed stdin.
// TestProtocolInputCancelBeforeFrame доказывает join настоящего OS-pipe reader
// при caller cancel до первого frame и без закрытия stdin со стороны peer.
func TestProtocolInputCancelBeforeFrame(t *testing.T) { runProtocolInputFixture(t, "CANCEL", nil) }

// runProtocolInputFixture owns one bounded child and its pipe endpoints; success
// requires child exit before the parent closes its still-open writer.
// runProtocolInputFixture владеет одним bounded child и pipe endpoints; успех
// требует завершения child до закрытия остающегося открытым writer владельца.
func runProtocolInputFixture(t *testing.T, mode string, request *sshcompose.Request) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProtocolInputChild$")
	command.Env = []string{"KODEX_SSH_STDIN_FIXTURE=" + mode, "GOMAXPROCS=2", "GOENV=off", "GOWORK=off", "KUBECONFIG=/dev/null"}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	joined := false
	defer func() {
		if !joined {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	if request != nil {
		if err = json.NewEncoder(stdin).Encode(request); err != nil {
			t.Fatal("send isolated helper request")
		}
		var response sshcompose.Response
		if err = json.NewDecoder(io.LimitReader(stdout, 16384)).Decode(&response); err != nil {
			t.Fatal("helper did not return receipt before peer EOF")
		}
		if response.Receipt == nil || response.Receipt.State != "SUCCEEDED" || !response.Receipt.HealthOK || response.Receipt.DeploymentRef != "inv_stdin_fixture" {
			t.Fatal("helper lost exact receipt")
		}
	} else {
		var response map[string]bool
		if json.NewDecoder(io.LimitReader(stdout, 16384)).Decode(&response) != nil || !response["cancelled"] {
			t.Fatal("helper did not join cancelled first-frame read")
		}
	}
	err = command.Wait()
	joined = true
	if err != nil || ctx.Err() != nil {
		t.Fatal("helper did not join control reader before peer EOF")
	}
}

// TestProtocolInputChild is entered only by the explicit isolated parent fixture;
// it owns no credentials, Docker daemon or network connection.
// TestProtocolInputChild запускается только явной isolated parent fixture;
// credentials, Docker daemon и network connection отсутствуют.
func TestProtocolInputChild(t *testing.T) {
	mode := os.Getenv("KODEX_SSH_STDIN_FIXTURE")
	if mode != "DEPLOY" && mode != "CANCEL" {
		t.Skip("isolated helper child only")
	}
	helper, _ := protocolFixture(t)
	input, err := protocolInput(os.Stdin)
	if err != nil {
		t.Fatal("create pollable protocol input")
	}
	if mode == "CANCEL" {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		if err = helper.Serve(ctx, input, os.Stdout); err == nil || ctx.Err() == nil {
			t.Fatal("first-frame cancellation did not close owned input")
		}
		if json.NewEncoder(os.Stdout).Encode(map[string]bool{"cancelled": true}) != nil {
			t.Fatal("encode cancellation proof")
		}
	} else if err = helper.Serve(t.Context(), input, os.Stdout); err != nil {
		t.Fatal("serve isolated protocol")
	}
	// fd0 по-прежнему принадлежит process, закрыт только duplicate input adapter.
	if _, err = os.Stdin.Stat(); err != nil {
		t.Fatal("helper adopted original stdin descriptor")
	}
}

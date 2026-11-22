package kubernetestarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/gorilla/websocket"
)

// TestNativeCopyStreamsKeepOriginalOperation proves fixed exec argv and distinct FILE_CONTENT channels over real TLS.
// Synthetic bytes do not qualify archive validation or a remote tar process; those remain separate owner/native acceptance.
// TestNativeCopyStreamsKeepOriginalOperation доказывает fixed exec argv и отдельные FILE_CONTENT channels через настоящий TLS.
// Synthetic bytes не квалифицируют archive validation либо remote tar process; это отдельная owner/native приёмка.
func TestNativeCopyStreamsKeepOriginalOperation(t *testing.T) {
	for _, operation := range []string{"kubernetes.cp.upload", "kubernetes.cp.download"} {
		t.Run(operation, func(t *testing.T) {
			profile := managedkubernetes.CopyProfile{TransferFormat: "FILE", RemotePath: "/tmp/file name", MaximumBytes: 128 << 10, TimeoutSeconds: 30}
			if operation == "kubernetes.cp.upload" {
				profile.ArtifactRef, profile.ArtifactSHA256 = "artifact_synthetic", strings.Repeat("a", 64)
			}
			raw, _ := json.Marshal(profile)
			input := managedkubernetes.OperationInput{Version: "v1", Resource: "pods", Options: raw}
			options, _, err := podStreamInputOptions(operation, input)
			if err != nil {
				t.Fatal(err)
			}
			output := bytes.Repeat([]byte("synthetic-tar-bytes"), 3000)
			peerDone := make(chan struct{})
			credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(peerDone)
				connection := upgradeNativeExecFixture(t, w, r, PrivilegedWebSocketV5.WireProtocol())
				if connection == nil {
					return
				}
				defer connection.Close()
				if !slices.Equal(r.URL.Query()["command"], options.Command) {
					t.Error("copy command changed on wire")
					return
				}
				if operation == "kubernetes.cp.upload" {
					_, wire, err := connection.ReadMessage()
					if err != nil || !bytes.Equal(wire, append([]byte{0}, []byte("owner-qualified-input")...)) {
						t.Error("FILE_CONTENT did not map to exact stdin")
						return
					}
					_, wire, err = connection.ReadMessage()
					if err != nil || !bytes.Equal(wire, []byte{255, 0}) {
						t.Error("copy input EOF changed")
						return
					}
				}
				_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{1}, output...))
				_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{3}, []byte(`{"status":"Success"}`)...))
			}))
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, operation, streamTargetFixture(pin), options, now.Add(3*time.Second), now.Add(3*time.Second), 128<<10)
			if err != nil {
				t.Fatal("native copy handshake failed")
			}
			defer stream.Close()
			frameContext, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if stream.WriteFrame(frameContext, value.KubernetesStreamFrame{Channel: "STDIN", Payload: []byte("unapproved-channel")}) == nil {
				t.Fatal("copy accepted exec stdin channel")
			}
			if operation == "kubernetes.cp.upload" {
				if stream.WriteFrame(frameContext, value.KubernetesStreamFrame{Channel: "FILE_CONTENT", Payload: []byte("owner-qualified-input")}) != nil || stream.WriteFrame(frameContext, value.KubernetesStreamFrame{Channel: "FILE_CONTENT", EndOfInput: true}) != nil {
					t.Fatal("owner file input failed")
				}
			} else if stream.WriteFrame(frameContext, value.KubernetesStreamFrame{Channel: "FILE_CONTENT", Payload: []byte("wrong-direction")}) == nil {
				t.Fatal("download accepted input")
			}
			var received bytes.Buffer
			for {
				read, err := stream.ReadStream()
				if err != nil {
					t.Fatal("copy lost native output")
				}
				if read.ExitCode != nil {
					if *read.ExitCode != 0 {
						t.Fatal("copy changed explicit exit")
					}
					break
				}
				expected := "STDOUT"
				if operation == "kubernetes.cp.download" {
					expected = "FILE_CONTENT"
				}
				if read.Channel != expected || len(read.Payload) > managedkubernetes.MaximumFrameBytes {
					t.Fatal("copy escaped channel or frame bound")
				}
				received.Write(read.Payload)
				clear(read.Payload)
			}
			if !bytes.Equal(received.Bytes(), output) {
				t.Fatal("copy bytes changed")
			}
			<-peerDone
		})
	}
}

// TestCopyPreparationPreservesSourceAndDownloadOrigin follows the actual preparation, fresh identity and one-attempt native open.
// Canceling the unary call after handoff leaves the worker stream alive without changing the cp origin into exec authority.
// TestCopyPreparationPreservesSourceAndDownloadOrigin проходит actual preparation, fresh identity и one-attempt native open.
// Отмена unary call после handoff сохраняет worker stream, не превращая cp origin в exec authority.
func TestCopyPreparationPreservesSourceAndDownloadOrigin(t *testing.T) {
	for _, operation := range []string{"kubernetes.cp.upload", "kubernetes.cp.download"} {
		t.Run(operation, func(t *testing.T) {
			peerDone := make(chan struct{})
			factory, snapshot, child, _, wire := podStreamPreparationFixture(t, func(w http.ResponseWriter, r *http.Request) {
				defer close(peerDone)
				if r.URL.Path != "/exec/example/selected/11111111-1111-4111-8111-111111111111/first" || r.URL.Query().Get("command") != "tar" {
					t.Error("copy changed selected Pod route")
					w.WriteHeader(403)
					return
				}
				upgrader := websocket.Upgrader{Subprotocols: []string{PrivilegedWebSocketV5.WireProtocol()}}
				connection, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer connection.Close()
				_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
				_ = connection.SetWriteDeadline(time.Now().Add(3 * time.Second))
				_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{1}, []byte("bounded-copy-output")...))
				_, _, _ = connection.ReadMessage()
			})
			profile := managedkubernetes.CopyProfile{TransferFormat: "FILE", RemotePath: "/tmp/result", MaximumBytes: 65536, TimeoutSeconds: 30}
			if operation == "kubernetes.cp.upload" {
				profile.ArtifactRef, profile.ArtifactSHA256 = "artifact_synthetic", strings.Repeat("a", 64)
			}
			input := snapshot.Input
			input.Options, _ = json.Marshal(profile)
			raw, err := managedkubernetes.EncodeOperationInput(operation, input)
			if err != nil {
				t.Fatal(err)
			}
			_, canonical, err := managedkubernetes.DecodeOperationInput(operation, raw)
			if err != nil {
				t.Fatal(err)
			}
			snapshot.Operation, snapshot.Input, snapshot.NormalizedInput = operation, input, canonical
			child.Operation, child.Input, child.NormalizedInput = operation, input, bytes.Clone(canonical)
			digest := sha256.Sum256(canonical)
			child.InputDigest = hex.EncodeToString(digest[:])
			prepared, err := factory.PrepareKubernetesPodStream(t.Context(), snapshot, child)
			if err != nil {
				t.Fatal("prepare exact copy child", err)
			}
			defer prepared.Close()
			binding, admission := podStreamAdmissionFixture(child)
			if operation == "kubernetes.cp.upload" {
				admission.Stream.InputChannels = []string{"FILE_CONTENT"}
			} else {
				admission.Stream.OutputChannels = []string{"FILE_CONTENT", "STDERR", "CONTROL"}
			}
			attempt, cancel := context.WithCancel(t.Context())
			defer cancel()
			stream, receipt, err := prepared.Open(t.Context(), attempt, binding, admission)
			if err != nil {
				t.Fatal("open exact copy child", err)
			}
			defer stream.Close()
			cancel()
			prepared.Close()
			read, err := stream.ReadStream()
			expected := "STDOUT"
			if operation == "kubernetes.cp.download" {
				expected = "FILE_CONTENT"
			}
			if err != nil || read.Channel != expected || string(read.Payload) != "bounded-copy-output" || receipt.Validate() != nil || receipt.Origin.Operation != operation || wire.streamCalls.Load() != 1 {
				t.Fatal("copy lost original authority or lifetime")
			}
			clear(read.Payload)
			_ = stream.Close()
			select {
			case <-peerDone:
			case <-time.After(time.Second):
				t.Fatal("copy peer did not join")
			}
		})
	}
}

// TestCopyRequestRejectsCommandDrift refuses arbitrary commands, mixed channels and budget drift before network access.
// TestCopyRequestRejectsCommandDrift отклоняет arbitrary commands, mixed channels и budget drift до network access.
func TestCopyRequestRejectsCommandDrift(t *testing.T) {
	credential, calls := transportFixture(t, false)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	profile := managedkubernetes.CopyProfile{TransferFormat: "FILE", RemotePath: "/tmp/file", MaximumBytes: 1024, TimeoutSeconds: 30}
	command, err := profile.NativeCommand("kubernetes.cp.download")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PodStreamOptions){
		func(o *PodStreamOptions) { o.Command = []string{"sh", "-c", "true"} },
		func(o *PodStreamOptions) { o.Stdin = true },
		func(o *PodStreamOptions) { o.Copy.RemotePath = "/different" },
		func(o *PodStreamOptions) { o.Copy.ArtifactRef = "artifact_injected" },
		func(o *PodStreamOptions) { o.Copy.MaximumBytes = 0 },
	} {
		options := PodStreamOptions{Copy: profile, Command: slices.Clone(command), Stdout: true, Stderr: true}
		change(&options)
		if stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.cp.download", streamTargetFixture(pin), options, now.Add(time.Second), now.Add(time.Second), 4096); err == nil || stream != nil {
			t.Fatal("changed copy profile opened transport")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid copy contacted upstream")
	}
}

// TestNativeCopyEnforcesRequestedByteCeiling refuses bytes beyond the request even when owner transport credit is larger.
// TestNativeCopyEnforcesRequestedByteCeiling отклоняет bytes сверх request даже при большем owner transport credit.
func TestNativeCopyEnforcesRequestedByteCeiling(t *testing.T) {
	credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection := upgradeNativeExecFixture(t, w, r, PrivilegedWebSocketV5.WireProtocol())
		if connection == nil {
			return
		}
		defer connection.Close()
		_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{1}, bytes.Repeat([]byte("x"), 2048)...))
	}))
	profile := managedkubernetes.CopyProfile{TransferFormat: "FILE", RemotePath: "/tmp/file", MaximumBytes: 1024, TimeoutSeconds: 30}
	command, _ := profile.NativeCommand("kubernetes.cp.download")
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.cp.download", streamTargetFixture(pin), PodStreamOptions{Copy: profile, Command: command, Stdout: true, Stderr: true}, now.Add(time.Second), now.Add(time.Second), 4096)
	if err != nil {
		t.Fatal("bounded copy did not open")
	}
	defer stream.Close()
	read, err := stream.ReadStream()
	if err == nil || len(read.Payload) != 0 || read.ExitCode != nil {
		t.Fatal("copy exceeded requested byte ceiling")
	}
}

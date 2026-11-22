package kubernetestarget

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetport "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
)

// copyFinalArchiveFixture constructs an approved FILE whose tar terminator precedes transport EOF.
// It returns only synthetic owned content and never reads a host file or live credential.
// copyFinalArchiveFixture строит approved FILE, чей tar terminator предшествует transport EOF.
// Возвращает лишь synthetic owned content, не читает host file либо live credential.
func copyFinalArchiveFixture(t *testing.T) (managedkubernetes.CopyProfile, managedkubernetes.CopyArchiveContent, PodStreamOptions) {
	t.Helper()
	body := []byte("owner-approved synthetic file")
	digest := sha256.Sum256(body)
	profile := managedkubernetes.CopyProfile{ArtifactRef: "artifact_synthetic", ArtifactSHA256: hex.EncodeToString(digest[:]), TransferFormat: "FILE", RemotePath: "/tmp/selected", MaximumBytes: 64 << 10, TimeoutSeconds: 30}
	content, err := managedkubernetes.NormalizeCopyUpload(profile, body)
	if err != nil {
		t.Fatal("normalize synthetic file")
	}
	command, err := profile.NativeCommand("kubernetes.cp.upload")
	if err != nil {
		t.Fatal("build fixed copy command")
	}
	return profile, content, PodStreamOptions{Copy: profile, Command: command, Stdin: true, Stdout: true, Stderr: true}
}

// consumeCopyTarFixture observes archive EOF without waiting for stdin FIN, like a normal extractor.
// It verifies the approved payload before returning; transport EOF is checked separately by each peer.
// consumeCopyTarFixture наблюдает archive EOF до stdin FIN, как обычный extractor.
// Проверяет approved payload до возврата; transport EOF каждый peer проверяет отдельно.
func consumeCopyTarFixture(t *testing.T, source io.Reader) {
	t.Helper()
	archive := tar.NewReader(source)
	header, err := archive.Next()
	if err != nil || header.Name != "selected" {
		t.Error("copy archive header changed")
		return
	}
	body, err := io.ReadAll(archive)
	if err != nil || string(body) != "owner-approved synthetic file" {
		t.Error("copy archive body changed")
		return
	}
	if _, err := archive.Next(); err != io.EOF {
		t.Error("copy archive has no bounded terminator")
	}
}

// TestNativeCopyFinalFrameJoinsTarEarlyExit exercises real TLS and both pinned protocols.
// A peer emits success upon archive EOF before consuming stdin FIN; one admitted final frame
// must complete its data and close before normal terminal teardown. A second EOF is rejected.
// TestNativeCopyFinalFrameJoinsTarEarlyExit проверяет real TLS и оба pinned protocol.
// Peer выдаёт success при archive EOF до stdin FIN; один admitted final frame обязан закончить
// data и close до normal terminal teardown. Второй EOF отклоняется.
func TestNativeCopyFinalFrameJoinsTarEarlyExit(t *testing.T) {
	for _, protocol := range []PrivilegedProtocol{PrivilegedWebSocketV5, PrivilegedSPDYV4} {
		t.Run(string(protocol), func(t *testing.T) {
			_, content, options := copyFinalArchiveFixture(t)
			defer clear(content.Bytes)
			var credential Credential
			var peerDone <-chan struct{}
			if protocol == PrivilegedWebSocketV5 {
				done := make(chan struct{})
				peerDone = done
				credential, _ = transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					defer close(done)
					connection := upgradeNativeExecFixture(t, w, request, protocol.WireProtocol())
					if connection == nil {
						return
					}
					defer connection.Close()
					_, wire, err := connection.ReadMessage()
					if err != nil || len(wire) < 2 || wire[0] != 0 || !bytes.Equal(wire[1:], content.Bytes) {
						t.Error("final admitted body changed")
						return
					}
					consumeCopyTarFixture(t, bytes.NewReader(wire[1:]))
					_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{3}, []byte(`{"status":"Success"}`)...))
					_, wire, err = connection.ReadMessage()
					if err != nil || !bytes.Equal(wire, []byte{255, 0}) {
						t.Error("final admitted close was lost after tar exit")
					}
				}))
			} else {
				var peer *spdyPeerFixture
				credential, peer = newSPDYPeerFixture(t, false, protocol.WireProtocol(), 4, func(streams map[string]httpstream.Stream) {
					consumeCopyTarFixture(t, streams[corev1.StreamTypeStdin])
					_, _ = streams[corev1.StreamTypeError].Write([]byte(`{"status":"Success"}`))
					_ = streams[corev1.StreamTypeError].Close()
					_ = streams[corev1.StreamTypeStdout].Close()
					_ = streams[corev1.StreamTypeStderr].Close()
					remaining, err := io.ReadAll(io.LimitReader(streams[corev1.StreamTypeStdin], 1))
					if err != nil || len(remaining) != 0 {
						t.Error("final admitted FIN was lost or extra data was replayed")
					}
				})
				peerDone = peer.done
			}
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			pin.Protocol = protocol
			var stream targetport.LiveStream
			var err error
			if protocol == PrivilegedWebSocketV5 {
				stream, err = credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.cp.upload", streamTargetFixture(pin), options, now.Add(3*time.Second), now.Add(time.Second), 64<<10)
			} else {
				stream, err = credential.openExecSPDY(t.Context(), t.Context(), pin, now, "kubernetes.cp.upload", streamTargetFixture(pin), options, now.Add(3*time.Second), now.Add(time.Second), 64<<10)
			}
			if err != nil {
				t.Fatal("open selected copy protocol")
			}
			defer stream.Close()
			readDone := make(chan bool, 1)
			go func() {
				read, err := stream.ReadStream()
				clear(read.Payload)
				readDone <- err == nil && read.ExitCode != nil && *read.ExitCode == 0
			}()
			frameContext, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if stream.WriteFrame(frameContext, value.KubernetesStreamFrame{Channel: "FILE_CONTENT", Payload: content.Bytes, EndOfInput: true}) != nil {
				t.Fatal("one admitted final body and close failed")
			}
			if !<-readDone {
				t.Fatal("early tar exit lost its explicit receipt")
			}
			if stream.WriteFrame(frameContext, value.KubernetesStreamFrame{Channel: "FILE_CONTENT", EndOfInput: true}) == nil {
				t.Fatal("final EOF was replayed")
			}
			_ = stream.Close()
			select {
			case <-peerDone:
			case <-time.After(time.Second):
				t.Fatal("copy peer did not join")
			}
		})
	}
}

// TestNativeCopyInterruptedFinalFrameNeverInventsExitOrReplay preserves uncertainty after partial remote consumption.
// A locally accepted write is not remote completion; the peer supplies no status and a second write stays closed.
// TestNativeCopyInterruptedFinalFrameNeverInventsExitOrReplay сохраняет uncertainty после partial remote consumption.
// Локально принятая write не является remote completion; peer не даёт status, second write остаётся закрытой.
func TestNativeCopyInterruptedFinalFrameNeverInventsExitOrReplay(t *testing.T) {
	for _, protocol := range []PrivilegedProtocol{PrivilegedWebSocketV5, PrivilegedSPDYV4} {
		t.Run(string(protocol), func(t *testing.T) {
			_, content, options := copyFinalArchiveFixture(t)
			defer clear(content.Bytes)
			var credential Credential
			var peerDone <-chan struct{}
			if protocol == PrivilegedWebSocketV5 {
				done := make(chan struct{})
				peerDone = done
				credential, _ = transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					defer close(done)
					connection := upgradeNativeExecFixture(t, w, request, protocol.WireProtocol())
					if connection == nil {
						return
					}
					defer connection.Close()
					_, reader, err := connection.NextReader()
					if err != nil {
						t.Error("missing first FILE write")
						return
					}
					var prefix [513]byte
					if _, err := io.ReadFull(reader, prefix[:]); err != nil || prefix[0] != 0 {
						t.Error("missing partial FILE body")
					}
				}))
			} else {
				var peer *spdyPeerFixture
				credential, peer = newSPDYPeerFixture(t, false, protocol.WireProtocol(), 4, func(streams map[string]httpstream.Stream) {
					var prefix [512]byte
					if _, err := io.ReadFull(streams[corev1.StreamTypeStdin], prefix[:]); err != nil {
						t.Error("missing partial FILE body")
					}
					_ = streams[corev1.StreamTypeStdin].Reset()
					_ = streams[corev1.StreamTypeError].Close()
					_ = streams[corev1.StreamTypeStdout].Close()
					_ = streams[corev1.StreamTypeStderr].Close()
				})
				peerDone = peer.done
			}
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			pin.Protocol = protocol
			var stream targetport.LiveStream
			var err error
			if protocol == PrivilegedWebSocketV5 {
				stream, err = credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.cp.upload", streamTargetFixture(pin), options, now.Add(3*time.Second), now.Add(time.Second), 64<<10)
			} else {
				stream, err = credential.openExecSPDY(t.Context(), t.Context(), pin, now, "kubernetes.cp.upload", streamTargetFixture(pin), options, now.Add(3*time.Second), now.Add(time.Second), 64<<10)
			}
			if err != nil {
				t.Fatal("open interrupted copy fixture")
			}
			defer stream.Close()
			call, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_ = stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "FILE_CONTENT", Payload: content.Bytes, EndOfInput: true})
			read, err := stream.ReadStream()
			clear(read.Payload)
			if err == nil || read.ExitCode != nil {
				t.Fatal("partial remote consumption invented successful exit")
			}
			if stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "FILE_CONTENT", Payload: content.Bytes, EndOfInput: true}) == nil {
				t.Fatal("uncertain FILE write was replayed")
			}
			_ = stream.Close()
			select {
			case <-peerDone:
			case <-time.After(time.Second):
				t.Fatal("interrupted copy peer did not join")
			}
		})
	}
}

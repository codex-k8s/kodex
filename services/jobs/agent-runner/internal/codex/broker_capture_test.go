package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func capturedRolloutFixture(t *testing.T, input model.Input) (model.Input, Result) {
	t.Helper()
	if input.WorkspaceRoot == "" {
		input.WorkspaceRoot = t.TempDir()
	}
	input.CodexHome = filepath.Join(input.WorkspaceRoot, ".kodex/state/codex-home")
	input.Mode, input.Attempt = runtimecontract.RunnerModeTurn, 3
	input.SessionRef, input.TurnRef = "ses_fixture", "turn_fixture"
	input.ExecutionBindingDigest, input.RuntimeRevisionDigest, input.InputDigest = strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	relative := ".kodex/state/codex-home/sessions/2026/10/07/rollout-00000000-0000-4000-8000-000000000001.jsonl"
	path := filepath.Join(input.WorkspaceRoot, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("{\"type\":\"session_meta\"}\n{\"type\":\"event_msg\"}\n")
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		t.Fatal("fixture source was not durably closed")
	}
	result, err := CaptureStoppedRollout(input, "00000000-0000-4000-8000-000000000001", path)
	if err != nil {
		t.Fatal("actual source capture failed")
	}
	result.Usage = runtimecontract.TokenUsage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}
	result.Outcome, result.FinalMessage = "SUCCEEDED", "synthetic final"
	return input, result
}

func TestBrokerCapturePreservesVerifiedSourceOnSuccessAndFailure(t *testing.T) {
	for _, mode := range []string{"success", "execution", "refresh read", "refresh commit", "activity"} {
		t.Run(mode, func(t *testing.T) {
			input, authPath := providerTurnFixture(t, []byte(`{"auth_mode":"chatgpt","tokens":{"refresh_token":"synthetic-old"}}`))
			input, captured := capturedRolloutFixture(t, input)
			got := captured
			var failure error
			if mode == "execution" || strings.HasPrefix(mode, "refresh") {
				got, failure = executeProviderTurn(t.Context(), input, []byte("synthetic task"), strings.Repeat("a", 64), func(context.Context, model.Input, []byte, string) (Result, error) {
					if mode == "execution" {
						return captured, errors.New("synthetic process failure")
					}
					if mode == "refresh read" {
						if err := os.Remove(authPath); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := os.WriteFile(authPath, []byte(`{"auth_mode":"chatgpt","tokens":{"refresh_token":"synthetic-new"}}`), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					return captured, nil
				}, func(context.Context, model.Input, runtimecontract.RunnerProviderCredentialRefreshRequest) error {
					return errors.New("synthetic commit failure")
				})
				if failure == nil {
					t.Fatal("failed provider became successful")
				}
			}
			var stream bytes.Buffer
			writer := &brokerFrameWriter{writer: &stream}
			var callback func(runtimecontract.RuntimeActivity) error
			if mode == "activity" {
				call := runtimecontract.NativeToolCall{CallID: "call-one", Revision: 1, Kind: runtimecontract.NativeToolKindSleep, State: runtimecontract.NativeToolStateRunning, SafeParameters: map[string]any{"requested_duration_ms": int64(25)}}
				if err := writer.activity(runtimecontract.RuntimeActivity{ToolCall: &call}); err != nil {
					t.Fatal(err)
				}
				callback = func(runtimecontract.RuntimeActivity) error { return errors.New("synthetic activity failure") }
			}
			if failure != nil {
				if err := writeProviderBrokerResultFailure(writer, got, failure); err != nil {
					t.Fatal(err)
				}
			} else if err := writer.finish(brokerResponse{Result: got, OK: true}); err != nil {
				t.Fatal(err)
			}
			result, err := readBoundProviderBrokerResponse(bytes.NewReader(stream.Bytes()), &input, uint32(os.Geteuid()), callback)
			if (err == nil) != (mode == "success") || !result.HasVerifiedRollout(input) || result.Usage != captured.Usage {
				t.Fatal("terminal lost verified source, usage or failure outcome")
			}
			if mode != "success" && (result.FinalMessage != "" || result.Outcome != "") {
				t.Fatal("failed capture confirmed a terminal result")
			}
		})
	}
}

func TestBrokerCaptureRejectsForeignMalformedAndChangedProof(t *testing.T) {
	for _, mode := range []string{"plain tuple", "unknown schema", "foreign binding", "foreign revision", "foreign input", "foreign attempt", "foreign session", "foreign turn", "foreign source", "changed digest", "changed bytes", "wrong owner", "wrong mode", "symlink", "hardlink", "legacy", "missing field", "unknown field", "duplicate field", "null proof", "missing input"} {
		t.Run(mode, func(t *testing.T) {
			input, result := capturedRolloutFixture(t, model.Input{})
			proof := *result.rolloutCapture
			response := brokerResponse{Result: result, OK: true, RolloutCapture: &proof}
			uid := uint32(os.Geteuid())
			version := providerBrokerVersion
			switch mode {
			case "plain tuple":
				response.RolloutCapture = nil
			case "unknown schema":
				proof.Schema = "future"
			case "foreign binding":
				proof.ExecutionBindingDigest = strings.Repeat("d", 64)
			case "foreign revision":
				proof.RuntimeRevisionDigest = strings.Repeat("d", 64)
			case "foreign input":
				proof.InputDigest = strings.Repeat("d", 64)
			case "foreign attempt":
				proof.Attempt++
			case "foreign session":
				proof.SessionRef = "ses_other"
			case "foreign turn":
				proof.TurnRef = "turn_other"
			case "foreign source":
				proof.SessionID = "00000000-0000-4000-8000-000000000002"
			case "changed digest":
				response.Result.ArchiveSHA256 = strings.Repeat("d", 64)
			case "changed bytes":
				if err := os.WriteFile(result.ArchivePath, []byte("different"), 0o640); err != nil {
					t.Fatal(err)
				}
			case "wrong owner":
				uid++
			case "wrong mode":
				if err := os.Chmod(result.ArchivePath, 0o660); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(result.ArchivePath, result.ArchivePath+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(result.ArchivePath+".old", result.ArchivePath); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(result.ArchivePath, result.ArchivePath+".link"); err != nil {
					t.Fatal(err)
				}
			case "legacy":
				version = 1
			}
			raw, err := json.Marshal(brokerFrame{Version: version, Sequence: 1, Kind: brokerFrameTerminal, Terminal: &response})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "missing field" {
				raw = bytes.Replace(raw, []byte(`"schema":"`+rolloutCaptureSchema+`",`), nil, 1)
			}
			if mode == "unknown field" {
				raw = bytes.Replace(raw, []byte(`"rollout_capture":{`), []byte(`"rollout_capture":{"trusted":true,`), 1)
			}
			if mode == "duplicate field" {
				raw = bytes.Replace(raw, []byte(`"rollout_capture":{`), []byte(`"rollout_capture":{"schema":"future",`), 1)
			}
			if mode == "null proof" {
				start := bytes.Index(raw, []byte(`"rollout_capture":`))
				raw = append(append([]byte(nil), raw[:start]...), []byte(`"rollout_capture":null}}`)...)
			}
			boundInput := &input
			if mode == "missing input" {
				boundInput = nil
			}
			got, err := readBoundProviderBrokerResponse(bytes.NewReader(raw), boundInput, uid)
			if err == nil || got.HasVerifiedRollout(input) || got.SessionID != "" || got.Usage.TotalTokens != 0 {
				t.Fatal("unverified capture escaped strict consumer")
			}
		})
	}
}

func TestArchiveDigestRejectsSameInodeMutation(t *testing.T) {
	input, result := capturedRolloutFixture(t, model.Input{})
	file, info, err := openProtectedFile(input.WorkspaceRoot, result.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer, err := os.OpenFile(result.ArchivePath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := writer.WriteString("appended bytes")
	syncErr, closeErr := writer.Sync(), writer.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		t.Fatal("fixture mutation failed")
	}
	if _, err := digestArchive(file, info); err == nil {
		t.Fatal("same-inode mutation was accepted against prior stat")
	}
}

func TestCaptureCannotBeIssuedFromTupleOrModifiedResult(t *testing.T) {
	input, result := capturedRolloutFixture(t, model.Input{})
	result.ArchiveSHA256 = strings.Repeat("d", 64)
	if result.HasVerifiedRollout(input) || failedProviderResult(result).SessionID != "" {
		t.Fatal("modified tuple retained capture authority")
	}
	if _, err := CaptureStoppedRollout(input, "00000000-0000-4000-8000-000000000002", result.ArchivePath); err == nil {
		t.Fatal("foreign UUID received source authority")
	}
}

// Отдельная kernel-fixture, без provider, cluster либо постоянных каталогов.
func TestBrokerCaptureNativeUIDBoundary(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native UID fixture requires disposable root test process")
	}
	for _, outcome := range []string{"success", "failure"} {
		t.Run(outcome, func(t *testing.T) { runBrokerCaptureNativeUIDBoundary(t, outcome) })
	}
}

func runBrokerCaptureNativeUIDBoundary(t *testing.T, outcome string) {
	root, err := os.MkdirTemp("", "kodex-provider-capture-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if os.Chown(root, 10002, 29000) != nil || os.Chmod(root, 0o770) != nil {
		t.Fatal("fixture ownership setup failed")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// go test может держать исходный executable в недоступном другому UID каталоге.
	// Копируем только текущий test binary в эту disposable fixture.
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal("fixture executable open failed")
	}
	fixtureExecutable := filepath.Join(root, "fixture.test")
	target, err := os.OpenFile(fixtureExecutable, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o750)
	if err != nil {
		source.Close()
		t.Fatal("fixture executable setup failed")
	}
	_, copyErr := io.Copy(target, source)
	sourceCloseErr, targetCloseErr := source.Close(), target.Close()
	if copyErr != nil || sourceCloseErr != nil || targetCloseErr != nil || os.Chown(fixtureExecutable, 0, 29000) != nil {
		t.Fatal("fixture executable copy failed")
	}
	command := func(mode string, uid uint32) *exec.Cmd {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		t.Cleanup(cancel)
		cmd := exec.CommandContext(ctx, fixtureExecutable, "-test.run=^TestBrokerCaptureNativeUIDFixture$")
		cmd.Env = []string{"KODEX_NATIVE_CAPTURE_FIXTURE=" + mode, "KODEX_NATIVE_CAPTURE_WORKSPACE=" + root, "KODEX_NATIVE_CAPTURE_OUTCOME=" + outcome}
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: 29000, Groups: []uint32{29000}}}
		return cmd
	}
	producer := command("producer", 10002)
	var diagnostics bytes.Buffer
	producer.Stdout, producer.Stderr = &diagnostics, &diagnostics
	if err := producer.Start(); err != nil {
		t.Fatal("native producer fixture start failed")
	}
	defer func() { _ = producer.Process.Kill() }()
	socket := filepath.Join(root, "provider.sock")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	consumer := command("consumer", 10001)
	if err := consumer.Run(); err != nil {
		t.Fatal("native consumer rejected exact provider capture")
	}
	if err := producer.Wait(); err != nil {
		t.Fatal("native failed-process source capture failed")
	}
}

func TestBrokerCaptureNativeUIDFixture(t *testing.T) {
	mode, root := os.Getenv("KODEX_NATIVE_CAPTURE_FIXTURE"), os.Getenv("KODEX_NATIVE_CAPTURE_WORKSPACE")
	outcome := os.Getenv("KODEX_NATIVE_CAPTURE_OUTCOME")
	if mode == "" {
		return
	}
	if filepath.Dir(root) != os.TempDir() || !strings.HasPrefix(filepath.Base(root), "kodex-provider-capture-") {
		t.Fatal("native fixture scope is invalid")
	}
	input := model.Input{WorkspaceRoot: root, CodexHome: filepath.Join(root, ".kodex/state/codex-home"), Mode: runtimecontract.RunnerModeTurn, Attempt: 3,
		SessionRef: "ses_fixture", TurnRef: "turn_fixture", ExecutionBindingDigest: strings.Repeat("a", 64), RuntimeRevisionDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64)}
	relative := ".kodex/state/codex-home/sessions/2026/10/07/rollout-00000000-0000-4000-8000-000000000001.jsonl"
	path, socket := filepath.Join(root, relative), filepath.Join(root, "provider.sock")
	if mode == "producer" {
		if os.Geteuid() != 10002 {
			t.Fatal("native writer UID changed")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal("native source directory setup failed")
		}
		if err := os.WriteFile(path, []byte("{\"type\":\"session_meta\"}\n"), 0o640); err != nil {
			t.Fatal("native source setup failed")
		}
		processMode := "rollout-nonzero"
		if outcome == "success" {
			processMode = "rollout-clean"
		}
		command := appServerPipeFixtureCommand(t, processMode)
		command.Env = append(command.Env, "KODEX_APP_SERVER_ROLLOUT_PATH="+path)
		server, err := startAppServerCommand(command, nil)
		if err != nil {
			t.Fatal("native synthetic writer start failed")
		}
		state := newProtocolState("00000000-0000-4000-8000-000000000001")
		state.threadID, state.threadPath = "00000000-0000-4000-8000-000000000001", path
		failure := server.stop(state)
		if (outcome == "success" && failure != nil) || (outcome != "success" && !errors.Is(failure, errAppServerExited)) {
			t.Fatal("native process failure changed")
		}
		result := captureFailedRollout(input, server, state, Result{Usage: runtimecontract.TokenUsage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}})
		if !result.HasVerifiedRollout(input) {
			t.Fatal("native failed process did not capture source")
		}
		listener, err := net.Listen("unix", socket)
		if err != nil {
			t.Fatal("native broker socket setup failed")
		}
		defer listener.Close()
		if os.Chmod(socket, 0o660) != nil {
			t.Fatal("native broker socket mode failed")
		}
		connection, err := listener.Accept()
		if err != nil {
			t.Fatal("native broker accept failed")
		}
		defer connection.Close()
		var deliveryErr error
		if outcome == "success" {
			result.Outcome, result.FinalMessage = "SUCCEEDED", "synthetic result"
			deliveryErr = writeBrokerTerminal(connection, brokerResponse{Result: result, OK: true})
		} else {
			deliveryErr = writeProviderBrokerResultFailure(connection, result, failure)
		}
		if deliveryErr != nil {
			t.Fatal("native broker result failed")
		}
		return
	}
	if mode != "consumer" || os.Geteuid() != 10001 {
		t.Fatal("native consumer fixture identity changed")
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal("consumer cannot stat native source")
	}
	connection, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal("consumer cannot reach native broker")
	}
	defer connection.Close()
	uid, err := brokerPeerUID(connection)
	if err != nil || uid != providerWriterUID {
		t.Fatal("consumer did not authenticate native peer")
	}
	result, err := readBoundProviderBrokerResponse(connection, &input, uid)
	wantOutcome := ""
	if outcome == "success" {
		wantOutcome = "SUCCEEDED"
	}
	if (err == nil) != (outcome == "success") || !result.HasVerifiedRollout(input) || result.Outcome != wantOutcome || result.Usage.TotalTokens != 3 {
		t.Fatal("native consumer lost failed source capture")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal("consumer lost native source")
	}
	stat := after.Sys().(*syscall.Stat_t)
	if stat.Uid != 10002 || stat.Gid != 29000 || after.Mode().Perm() != 0o640 || !os.SameFile(before, after) || before.Sys().(*syscall.Stat_t).Ctim != stat.Ctim {
		t.Fatal("read-only consumer mutated native source ABI")
	}
}

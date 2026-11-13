package opencode

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// executorLifecycleImage pins only a disposable process fixture, never an admitted runtime.
// executorLifecycleImage закрепляет disposable process fixture, не допущенный runtime.
const executorLifecycleImage = "python@sha256:eb2c62658b8d51bff0acc408504b76521d02030de56a8a3c1e830083e89b89d2"

// executorContainerFixture owns separate PID namespaces, synthetic volumes and one test binary.
// executorContainerFixture владеет отдельными PID namespaces, synthetic volumes и test binary.
type executorContainerFixture struct {
	docker               nativeDocker
	server, socket, work string
	binary               string
	clients              int
}

// newExecutorContainerFixture uses the existing local-Unix ownership guard and never pulls an
// image, mounts a repository/credential, shares PID namespaces or admits a non-test binary path.
// newExecutorContainerFixture использует прежний local-Unix ownership guard, не скачивает image,
// не монтирует repository/credentials, не разделяет PID namespaces и требует явный test binary.
func newExecutorContainerFixture(t *testing.T) *executorContainerFixture {
	t.Helper()
	endpoint := os.Getenv("DOCKER_HOST")
	if endpoint == "" {
		raw, err := exec.Command("docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}").Output()
		if err != nil {
			t.Fatal("local Docker context unavailable")
		}
		endpoint = strings.TrimSpace(string(raw))
	}
	if !strings.HasPrefix(endpoint, "unix:///") || strings.ContainsAny(endpoint, "\r\n") {
		t.Fatal("executor qualification requires local Unix Docker")
	}
	binary := os.Getenv("KODEX_TEST_EXECUTOR_BINARY")
	info, err := os.Lstat(binary)
	if err != nil || !filepath.IsAbs(binary) || filepath.Clean(binary) != binary || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 || info.Mode()&0o022 != 0 {
		t.Fatal("explicit protected disposable executor test binary required")
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	scope := "kodex-executor-drain-" + hex.EncodeToString(nonce[:])
	fixture := &executorContainerFixture{docker: nativeDocker{t: t, endpoint: endpoint, scope: scope}, server: scope + "-server", socket: scope + "-socket", work: scope + "-work", binary: binary}
	for _, volume := range []string{fixture.socket, fixture.work} {
		t.Cleanup(func() {
			owner, err := fixture.docker.command("volume", "inspect", "--format", `{{index .Labels "kodex.native-test"}}`, volume)
			if err == nil && owner == scope {
				_, _ = fixture.docker.command("volume", "rm", volume)
			}
		})
		if _, err := fixture.docker.command("volume", "create", "--label", "kodex.native-test="+scope, volume); err != nil {
			t.Fatal("create owned synthetic executor volume")
		}
	}
	initializer := scope + "-initialize"
	fixture.docker.start(initializer, append(fixture.mounts(true), "--network=none", "--read-only", "--cap-add=CHOWN", "--user=0:0", "--memory=128m", "--cpus=1", executorLifecycleImage, "python3", "-c", `import os
for p in ("/run/kodex/workspace","/workspace/work"):
 os.chmod(p,0o2770)
 os.chown(p,10002,29000)`)...)
	if code, err := fixture.docker.command("wait", initializer); err != nil || code != "0" {
		t.Fatal("initialize owned synthetic executor metadata")
	}
	fixture.docker.removeContainer(initializer)
	arguments := append(fixture.mounts(true), fixture.executable()...)
	arguments = append(arguments, "--network=none", "--read-only", "--user=10002:29000", "--memory=128m", "--cpus=1", "--tmpfs", "/tmp:rw,nosuid,nodev,size=16m,mode=0700,uid=10002,gid=29000", "-e", "KODEX_TEST_EXECUTOR_LIFECYCLE=server", executorLifecycleImage)
	fixture.docker.start(fixture.server, arguments...)
	fixture.client("INITIALIZE", "READY", false)
	return fixture
}

// mounts gives the executor both synthetic roots; role clients receive only the control socket.
// mounts передаёт executor оба synthetic roots, role clients — только control socket.
func (fixture *executorContainerFixture) mounts(includeWork bool) []string {
	result := []string{"--mount", "type=volume,src=" + fixture.socket + ",dst=/run/kodex/workspace"}
	if includeWork {
		result = append(result, "--mount", "type=volume,src="+fixture.work+",dst=/workspace/work")
	}
	return result
}

// executable mounts only the compiled fixture, read-only, including the production child path.
// executable монтирует только скомпилированный fixture read-only, включая production child path.
func (fixture *executorContainerFixture) executable() []string {
	return []string{"--mount", "type=bind,src=" + fixture.binary + ",dst=/probe/executor,readonly", "--mount", "type=bind,src=" + fixture.binary + ",dst=/usr/local/bin/kodex-agent-runner,readonly", "--entrypoint", "/probe/executor"}
}

// clientArguments fixes exact role/foreign peer identities and closed requests; it grants no
// credential or command authority beyond the same synthetic container test.
// clientArguments закрепляет exact role/foreign peer identities и закрытые requests без
// credentials либо command authority за границами того же synthetic container test.
func (fixture *executorContainerFixture) clientArguments(action, status string, failure bool, extra ...string) []string {
	user := "10001:29000"
	for _, value := range extra {
		if value == "KODEX_TEST_FOREIGN_EXECUTOR_PEER=true" {
			user = "10003:29000"
		}
	}
	arguments := append(fixture.mounts(false), fixture.executable()...)
	arguments = append(arguments, "--network=none", "--read-only", "--user="+user, "--memory=128m", "--cpus=1", "-e", "KODEX_TEST_EXECUTOR_LIFECYCLE=client", "-e", "KODEX_TEST_EXECUTOR_ACTION="+action, "-e", "KODEX_TEST_EXECUTOR_STATUS="+status)
	if failure {
		arguments = append(arguments, "-e", "KODEX_TEST_EXPECT_QUIESCE_FAILURE=true")
	}
	for _, value := range extra {
		arguments = append(arguments, "-e", value)
	}
	return append(arguments, executorLifecycleImage)
}

// client starts one independently owned role process and checks its actual protocol verdict.
// client запускает один отдельный role process и проверяет настоящий protocol verdict.
func (fixture *executorContainerFixture) client(action, status string, failure bool, extra ...string) {
	fixture.docker.t.Helper()
	fixture.clients++
	name := fixture.docker.scope + "-client-" + strconv.Itoa(fixture.clients)
	fixture.docker.start(name, fixture.clientArguments(action, status, failure, extra...)...)
	if code, err := fixture.docker.command("wait", name); err != nil || code != "0" {
		fixture.docker.t.Fatal("executor lifecycle client violated the expected closed result")
	}
	raw, err := fixture.docker.command("logs", name)
	var result struct {
		State      string `json:"state"`
		CallFailed bool   `json:"call_failed"`
		UID, GID   int
	}
	if err != nil || json.Unmarshal([]byte(raw), &result) != nil || result.CallFailed != failure || !failure && result.State != status {
		fixture.docker.t.Fatal("executor lifecycle client receipt changed")
	}
	fixture.docker.removeContainer(name)
}

// marker reads fixture state from another PID namespace, so observation cannot contaminate
// the executor's namespace-wide child cleanup; no source command is repeated.
// marker читает fixture state из другого PID namespace, не загрязняя namespace-wide child
// cleanup наблюдателем; исходная команда не повторяется.
func (fixture *executorContainerFixture) marker(work bool, name string) bool {
	volume, root := fixture.socket, "/run/kodex/workspace"
	if work {
		volume, root = fixture.work, "/workspace/work"
	}
	probe := fixture.docker.scope + "-marker"
	fixture.docker.t.Cleanup(func() { fixture.docker.removeContainer(probe) })
	_, err := fixture.docker.command("run", "--rm", "--pull=never", "--name", probe, "--label", "kodex.native-test="+fixture.docker.scope, "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=64", "--user=10001:29000", "--memory=64m", "--mount", "type=volume,src="+volume+",dst="+root+",readonly", executorLifecycleImage, "python3", "-c", `import pathlib,sys;sys.exit(not pathlib.Path(sys.argv[1]).exists())`, root+"/"+name)
	return err == nil
}

// stop signals only the owned executor and waits for its test marker, not for a business retry.
// stop посылает сигнал только owned executor и ждёт test marker, не повтор business operation.
func (fixture *executorContainerFixture) stop() {
	fixture.docker.t.Helper()
	if _, err := fixture.docker.command("kill", "--signal=TERM", fixture.server); err != nil {
		fixture.docker.t.Fatal("signal owned disposable executor")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !fixture.marker(false, "draining") {
		if time.Now().After(deadline) {
			fixture.docker.t.Fatal("executor did not enter bounded signal drain")
		}
	}
}

// joined checks that the real server returned successfully after sending the positive ACK;
// the marker is written only after its HTTP join and final descendant cleanup both complete.
// joined проверяет успешный возврат настоящего server после положительного ACK; marker
// записывается лишь после HTTP join и окончательного завершения всех потомков.
func (fixture *executorContainerFixture) joined() {
	fixture.docker.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !fixture.marker(false, "stopped") {
		if time.Now().After(deadline) {
			fixture.docker.t.Fatal("acknowledged executor did not complete its physical shutdown")
		}
	}
}

// TestNativeExecutorSIGTERMQuiescence qualifies actual PID1, fixed UIDs, separate PID namespaces,
// closed command admission, active-command cancellation, late ACK and a bounded no-ACK failure.
// TestNativeExecutorSIGTERMQuiescence проверяет actual PID1, fixed UIDs, отдельные PID namespaces,
// закрытый command admission, отмену активной команды, late ACK и bounded no-ACK failure.
func TestNativeExecutorSIGTERMQuiescence(t *testing.T) {
	if os.Getenv("KODEX_TEST_EXECUTOR_CONTAINERS") != "1" {
		t.Skip("set KODEX_TEST_EXECUTOR_CONTAINERS=1 with explicit fixture binary for local qualification")
	}
	t.Run("cancel-before-ack", func(t *testing.T) {
		fixture := newExecutorContainerFixture(t)
		fixture.stop()
		fixture.client("INITIALIZE", "", true)
		fixture.client("RUN", "", true)
		fixture.client("QUIESCE", "", true, "KODEX_TEST_FOREIGN_EXECUTOR_BINDING=true")
		fixture.client("QUIESCE", "", true, "KODEX_TEST_FOREIGN_EXECUTOR_PEER=true")
		fixture.client("QUIESCE", "QUIESCENT", false)
		fixture.joined()
	})
	t.Run("cancel-active-command", func(t *testing.T) {
		fixture := newExecutorContainerFixture(t)
		name := fixture.docker.scope + "-active-command"
		fixture.docker.start(name, fixture.clientArguments("RUN", "CANCELLED", false)...)
		deadline := time.Now().Add(5 * time.Second)
		for !fixture.marker(true, "started") {
			if time.Now().After(deadline) {
				t.Fatal("selected synthetic command did not start")
			}
		}
		fixture.stop()
		if code, err := fixture.docker.command("wait", name); err != nil || code != "0" || fixture.marker(true, "survived") {
			t.Fatal("cancelled command survived or lost its known cancelled outcome")
		}
		fixture.client("QUIESCE", "QUIESCENT", false)
		fixture.joined()
	})
	t.Run("no-ack-deadline", func(t *testing.T) {
		fixture := newExecutorContainerFixture(t)
		fixture.stop()
		deadline := time.Now().Add(90 * time.Second)
		for {
			running, err := fixture.docker.command("inspect", "--format", "{{.State.Running}}", fixture.server)
			if err != nil {
				t.Fatal("owned executor status is unknown")
			}
			if running == "false" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("no-ACK executor exceeded the fixed shutdown budget")
			}
			time.Sleep(time.Second)
		}
		if code, err := fixture.docker.command("wait", fixture.server); err != nil || code != "1" {
			t.Fatal("missing ACK became a successful executor shutdown")
		}
	})
}

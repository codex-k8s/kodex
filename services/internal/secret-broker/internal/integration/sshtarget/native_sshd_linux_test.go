package sshtarget

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// nativeSSHFixture owns one real OpenSSH inetd peer in private user/PID/network/mount namespaces with a synthetic passwd database and empty writable home.
// nativeSSHFixture владеет одним настоящим OpenSSH inetd peer в private user/PID/network/mount namespaces с synthetic passwd database и пустым writable home.
type nativeSSHFixture struct {
	client  Client
	target  Target
	private []byte
	home    string
}

// newNativeSSHFixture avoids all host login startup files and external sockets; the OpenSSH lifetime remains independent of a closed channel until test cleanup.
// newNativeSSHFixture избегает всех host login startup files и внешних sockets; lifetime OpenSSH остаётся независимым от закрытого channel до test cleanup.
func newNativeSSHFixture(t *testing.T) nativeSSHFixture {
	t.Helper()
	bubblewrap, bubbleErr := exec.LookPath("bwrap")
	sshd := "/usr/sbin/sshd"
	if _, err := os.Stat(sshd); err != nil || bubbleErr != nil || os.Getuid() == 0 {
		if os.Getenv("KODEX_MANAGED_SSH_NATIVE_REQUIRED") == "1" {
			t.Fatal("isolated non-root OpenSSH prerequisites unavailable")
		}
		t.Skip("isolated non-root OpenSSH prerequisites unavailable")
	}
	root := t.TempDir()
	fixture := filepath.Join(root, "fixture")
	etc := filepath.Join(root, "etc")
	home := filepath.Join(root, "home")
	for _, directory := range []string{fixture, etc, home} {
		if os.Mkdir(directory, 0700) != nil {
			t.Fatal("create private native SSH fixture")
		}
	}
	host, hostPrivate := fixturePrivateKey(t)
	user, userPrivate := fixturePrivateKey(t)
	t.Cleanup(func() { clear(hostPrivate); clear(userPrivate) })
	files := map[string][]byte{
		filepath.Join(fixture, "host_key"):        hostPrivate,
		filepath.Join(fixture, "authorized_keys"): ssh.MarshalAuthorizedKey(user.PublicKey()),
		filepath.Join(etc, "passwd"):              []byte(fmt.Sprintf("fixture:x:%d:%d:SSH fixture:/home/fixture:/bin/sh\n", os.Getuid(), os.Getgid())),
		filepath.Join(etc, "group"):               []byte(fmt.Sprintf("fixture:x:%d:\n", os.Getgid())),
		filepath.Join(etc, "shadow"):              []byte("fixture:synthetic-password-disabled:20000:0:99999:7:::\n"),
		filepath.Join(etc, "nsswitch.conf"):       []byte("passwd: files\ngroup: files\nshadow: files\nhosts: files\n"),
		filepath.Join(etc, "ld.so.cache"):         {},
		filepath.Join(fixture, "sshd_config"):     []byte("HostKey /fixture/host_key\nAuthorizedKeysFile /fixture/authorized_keys\nPidFile /run/sshd.pid\nUsePAM no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPubkeyAuthentication yes\nAuthenticationMethods publickey\nStrictModes yes\nPermitRootLogin no\nPermitUserRC no\nPermitTTY no\nAllowTcpForwarding no\nAllowAgentForwarding no\nX11Forwarding no\nUseDNS no\nPrintMotd no\nPrintLastLog no\nLogLevel ERROR\nAllowUsers fixture\n"),
	}
	for name, content := range files {
		if os.WriteFile(name, content, 0600) != nil {
			t.Fatal("write private native SSH fixture")
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("listen private native SSH fixture")
	}
	ctx, stop := context.WithTimeout(t.Context(), 15*time.Second)
	finished := make(chan error, 1)
	logPath := filepath.Join(root, "sshd-private.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("create private native SSH diagnostic file")
	}
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			finished <- err
			return
		}
		defer connection.Close()
		file, err := connection.(*net.TCPConn).File()
		if err != nil {
			finished <- err
			return
		}
		defer file.Close()
		arguments := []string{"--unshare-user", "--unshare-pid", "--unshare-net", "--unshare-ipc", "--unshare-uts", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--clearenv", "--ro-bind", "/usr", "/usr", "--symlink", "usr/bin", "/bin", "--symlink", "usr/sbin", "/sbin", "--symlink", "usr/lib", "/lib", "--symlink", "usr/lib64", "/lib64", "--ro-bind", etc, "/etc", "--ro-bind", "/etc/ld.so.cache", "/etc/ld.so.cache", "--ro-bind", fixture, "/fixture", "--dir", "/home", "--bind", home, "/home/fixture", "--tmpfs", "/tmp", "--tmpfs", "/run", "--proc", "/proc", "--dev", "/dev", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "HOME", "/home/fixture", "--setenv", "USER", "fixture", "--setenv", "LOGNAME", "fixture", "--setenv", "LANG", "C.UTF-8", "/bin/sh", "-c", "/usr/sbin/sshd -i -e -f /fixture/sshd_config; exec /usr/bin/sleep 30"}
		command := exec.CommandContext(ctx, bubblewrap, arguments...)
		command.Dir = "/"
		command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
		command.Stdin, command.Stdout, command.Stderr = file, file, log
		finished <- command.Run()
	}()
	t.Cleanup(func() {
		stop()
		_ = listener.Close()
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("native SSH namespace did not join")
		}
		_ = log.Close()
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			if len(data) > 4096 {
				data = data[:4096]
			}
			if !containsPrivateKey(data, hostPrivate) && !containsPrivateKey(data, userPrivate) {
				t.Logf("isolated synthetic sshd diagnostic: %q", data)
			}
		}
	})
	target := Target{Address: "192.0.2.10", Port: 22, User: "fixture", HostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(host.PublicKey())))}
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
	return nativeSSHFixture{client: client, target: target, private: userPrivate, home: home}
}

// TestNativeOpenSSHIsolatedExactExec verifies the actual installed sshd handles exact stdin, stdout/stderr and exit evidence in a private synthetic account with no host startup hooks.
// TestNativeOpenSSHIsolatedExactExec проверяет exact stdin, stdout/stderr и exit evidence actual установленного sshd в private synthetic account без host startup hooks.
func TestNativeOpenSSHIsolatedExactExec(t *testing.T) {
	fixture := newNativeSSHFixture(t)
	result, err := fixture.client.Run(t.Context(), fixture.target, fixture.private, Command{Text: "read value; printf '%s' \"$value\"; printf diagnostic >&2; exit 7", Input: []byte("native-input\n"), MaximumOutput: 32768})
	if err != nil || !result.SessionExited || result.ExitStatus != 7 || string(result.Stdout) != "native-input" || string(result.Stderr) != "diagnostic" {
		t.Fatal("isolated native SSH exec did not return exact session evidence")
	}
}

// TestNativeOpenSSHCancelDoesNotJoinDetachedEffect proves a real nonprivileged remote descendant may commit after the client channel closes; the transport must retain UNKNOWN and never resend.
// TestNativeOpenSSHCancelDoesNotJoinDetachedEffect доказывает, что real nonprivileged remote descendant может завершить effect после закрытия client channel; transport сохраняет UNKNOWN и не повторяет команду.
func TestNativeOpenSSHCancelDoesNotJoinDetachedEffect(t *testing.T) {
	fixture := newNativeSSHFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	joined := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		select {
		case <-joined:
		case <-time.After(2 * time.Second):
			t.Error("native SSH cancelled caller did not join")
		}
	})
	go func() {
		defer close(joined)
		_, err := fixture.client.Run(ctx, fixture.target, fixture.private, Command{Text: "/usr/bin/setsid /bin/sh -c 'sleep 0.3; printf late > /home/fixture/late' </dev/null >/dev/null 2>&1 & printf started > /home/fixture/started; sleep 5", MaximumOutput: 32768})
		done <- err
	}()
	waitNativeFixtureFile(t, filepath.Join(fixture.home, "started"))
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrUnknown) {
			t.Fatal("native SSH cancellation claimed a known effect outcome")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("native SSH cancellation did not join local transport")
	}
	waitNativeFixtureFile(t, filepath.Join(fixture.home, "late"))
}

// waitNativeFixtureFile observes only a known file in the test-owned home with a fixed budget; it never probes external hosts or user files.
// waitNativeFixtureFile наблюдает лишь известный file в test-owned home с фиксированным бюджетом; external hosts и user files не проверяются.
func waitNativeFixtureFile(t *testing.T, name string) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(name); err == nil {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("native SSH private effect marker unavailable")
		case <-ticker.C:
		}
	}
}

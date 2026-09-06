package providercredential

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maximumAppServerLineBytes = 1 << 20
	maximumAppServerMessages  = 10_000
	maximumAppServerRequest   = 1 << 20
	maximumAuthJSONBytes      = 1 << 20
	processShutdownTimeout    = 5 * time.Second
)

type DeviceAuthorizationSession interface {
	MaterializerAttemptRef() string
	VerificationURI() string
	UserCode() string
	Wait(context.Context) ([]byte, string, error)
	Close() error
}

type AppServer interface {
	Check(context.Context) error
	StartDeviceAuthorization(context.Context, string, string) (DeviceAuthorizationSession, error)
	ObserveModelCatalog(context.Context, []byte, string) (ModelCatalog, error)
}

type AppServerProcess struct {
	binary      string
	root        string
	catalogHTTP modelCatalogHTTPClient
}

func NewAppServerProcess(binary, root string) (*AppServerProcess, error) {
	if !filepath.IsAbs(binary) || !filepath.IsAbs(root) || filepath.Clean(binary) != binary || filepath.Clean(root) != root {
		return nil, errors.New("Codex app-server configuration is invalid")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, errors.New("create Codex app-server state root")
	}
	return &AppServerProcess{binary: binary, root: root, catalogHTTP: newModelCatalogHTTPClient()}, nil
}

func (process *AppServerProcess) Check(ctx context.Context) error {
	info, err := os.Stat(process.binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return errors.New("Codex app-server executable is unavailable")
	}
	info, err = os.Stat(process.root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("Codex app-server state root is unavailable")
	}
	return process.checkCatalogRuntime(ctx)
}

func (process *AppServerProcess) StartDeviceAuthorization(
	ctx context.Context,
	materializerAttemptRef, homeName string,
) (DeviceAuthorizationSession, error) {
	if ctx == nil || materializerAttemptRef == "" || homeName == "" || strings.ContainsAny(homeName, `/\\`) {
		return nil, errors.New("device authorization process input is invalid")
	}
	home := filepath.Join(process.root, homeName)
	if err := os.Mkdir(home, 0o700); err != nil && !os.IsExist(err) {
		return nil, errors.New("create Codex app-server state directory")
	}
	if info, err := os.Lstat(home); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("Codex app-server state directory is unsafe")
	}
	server, err := startAppServer(process.binary, home)
	if err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}
	initialized := false
	defer func() {
		if !initialized {
			_ = server.terminate()
			_ = os.RemoveAll(home)
		}
	}()
	initialize := map[string]any{
		"clientInfo": map[string]string{"name": "kodex-secret-broker", "title": "Kodex secret-broker", "version": "1"},
		"capabilities": map[string]any{
			"experimentalApi":           false,
			"optOutNotificationMethods": []string{"thread/started", "turn/started", "turn/completed"},
		},
	}
	if _, err := server.call(ctx, "initialize", initialize); err != nil {
		return nil, err
	}
	if err := server.write(map[string]any{"method": "initialized"}); err != nil {
		return nil, err
	}
	raw, err := server.call(ctx, "account/login/start", map[string]string{"type": "chatgptDeviceCode"})
	if err != nil {
		return nil, err
	}
	var response struct {
		Type            string `json:"type"`
		LoginID         string `json:"loginId"`
		UserCode        string `json:"userCode"`
		VerificationURL string `json:"verificationUrl"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Type != "chatgptDeviceCode" || response.LoginID == "" ||
		len(response.LoginID) > 256 || response.UserCode == "" || len(response.UserCode) > 128 ||
		response.VerificationURL == "" || len(response.VerificationURL) > 2000 ||
		!strings.HasPrefix(response.VerificationURL, "https://") {
		return nil, errors.New("Codex app-server returned invalid device authorization metadata")
	}
	initialized = true
	return &deviceSession{
		server: server, home: home, materializerAttemptRef: materializerAttemptRef,
		loginID: response.LoginID, verificationURI: response.VerificationURL, userCode: response.UserCode,
	}, nil
}

type wireMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type streamEvent struct {
	message wireMessage
	err     error
}

type appServer struct {
	command     *exec.Cmd
	stdin       io.WriteCloser
	messages    <-chan streamEvent
	wait        <-chan error
	diagnostics <-chan error
	nextID      int64
	closeOnce   sync.Once
	closeErr    error
	readerStop  chan struct{}
	readerDone  chan struct{}
}

func startAppServer(binary, home string) (*appServer, error) {
	command := exec.Command(binary, "app-server", "--strict-config", "--listen", "stdio://")
	command.WaitDelay = processShutdownTimeout
	command.Dir = home
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + home, "CODEX_HOME=" + home}
	command.Env = append(command.Env, "HTTP_PROXY="+providerEgressProxyURL, "HTTPS_PROXY="+providerEgressProxyURL)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, errors.New("create Codex app-server request stream")
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, errors.New("create Codex app-server response stream")
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, errors.New("create Codex app-server diagnostic stream")
	}
	if err := command.Start(); err != nil {
		return nil, errors.New("start Codex app-server process")
	}
	messages := make(chan streamEvent, 64)
	readerStop, readerDone := make(chan struct{}), make(chan struct{})
	go readAppServerMessages(stdout, messages, readerStop, readerDone)
	diagnostics := make(chan error, 1)
	go func() {
		written, copyErr := io.Copy(io.Discard, io.LimitReader(stderr, maximumAppServerLineBytes+1))
		if copyErr != nil || written > maximumAppServerLineBytes {
			diagnostics <- errors.New("Codex app-server diagnostic stream exceeded its bound")
			return
		}
		diagnostics <- nil
	}()
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	return &appServer{command: command, stdin: stdin, messages: messages, wait: wait, diagnostics: diagnostics, readerStop: readerStop, readerDone: readerDone}, nil
}

func readAppServerMessages(reader io.Reader, events chan<- streamEvent, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	defer close(events)
	send := func(event streamEvent) bool {
		select {
		case events <- event:
			return true
		case <-stop:
			return false
		}
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maximumAppServerLineBytes)
	count := 0
	for scanner.Scan() {
		count++
		if count > maximumAppServerMessages || len(scanner.Bytes()) == 0 {
			send(streamEvent{err: errors.New("Codex app-server message budget exceeded")})
			return
		}
		var message wireMessage
		if json.Unmarshal(scanner.Bytes(), &message) != nil ||
			(message.Method == "" && len(message.ID) == 0) ||
			(message.Method != "" && len(message.Result) != 0) ||
			(len(message.Result) != 0 && len(message.Error) != 0) {
			send(streamEvent{err: errors.New("Codex app-server JSON-RPC message is invalid")})
			return
		}
		if !send(streamEvent{message: message}) {
			return
		}
	}
	if scanner.Err() != nil {
		send(streamEvent{err: errors.New("read Codex app-server response stream")})
	}
}

func (server *appServer) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	server.nextID++
	id := server.nextID
	if err := server.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, context.Canceled
		case event, open := <-server.messages:
			if !open {
				return nil, errors.New("Codex app-server closed its response stream")
			}
			if event.err != nil {
				return nil, event.err
			}
			if event.message.Method != "" {
				if len(event.message.ID) != 0 {
					_ = server.write(map[string]any{"id": event.message.ID, "error": map[string]any{"code": -32000, "message": "Server requests are not authorized"}})
					if method != "account/login/start" && event.message.Method == "account/chatgptAuthTokens/refresh" {
						return nil, errModelCatalogAuthorization
					}
					return nil, errors.New("Codex app-server requested unsupported authority")
				}
				continue
			}
			var responseID int64
			if json.Unmarshal(event.message.ID, &responseID) != nil || responseID != id {
				return nil, errors.New("Codex app-server response correlation failed")
			}
			if len(event.message.Error) != 0 {
				return nil, errors.New("Codex app-server returned a protocol error")
			}
			return event.message.Result, nil
		}
	}
}

func (server *appServer) write(message any) error {
	raw, err := json.Marshal(message)
	defer clear(raw)
	if err != nil || len(raw) == 0 || len(raw) > maximumAppServerRequest {
		return errors.New("encode Codex app-server request")
	}
	raw = append(raw, '\n')
	if _, err := server.stdin.Write(raw); err != nil {
		return errors.New("write Codex app-server request")
	}
	return nil
}

func (server *appServer) terminate() error {
	server.closeOnce.Do(func() {
		close(server.readerStop)
		_ = server.stdin.Close()
		timer := time.NewTimer(processShutdownTimeout)
		defer timer.Stop()
		select {
		case err := <-server.wait:
			if err != nil {
				server.closeErr = errors.New("Codex app-server exited unsuccessfully")
			}
		case <-timer.C:
			if server.command.Process != nil {
				_ = syscall.Kill(-server.command.Process.Pid, syscall.SIGTERM)
			}
			force := time.NewTimer(processShutdownTimeout)
			select {
			case <-server.wait:
				server.closeErr = errors.New("Codex app-server required forced shutdown")
			case <-force.C:
				if server.command.Process != nil {
					_ = syscall.Kill(-server.command.Process.Pid, syscall.SIGKILL)
				}
				<-server.wait
				server.closeErr = errors.New("Codex app-server shutdown deadline exceeded")
			}
			force.Stop()
		}
		<-server.readerDone
		select {
		case diagnosticErr := <-server.diagnostics:
			server.closeErr = errors.Join(server.closeErr, diagnosticErr)
		default:
		}
	})
	return server.closeErr
}

type deviceSession struct {
	server                                *appServer
	home, materializerAttemptRef, loginID string
	verificationURI, userCode             string
	closeOnce                             sync.Once
	closeErr                              error
}

func (session *deviceSession) MaterializerAttemptRef() string { return session.materializerAttemptRef }
func (session *deviceSession) VerificationURI() string        { return session.verificationURI }
func (session *deviceSession) UserCode() string               { return session.userCode }

func (session *deviceSession) Wait(ctx context.Context) ([]byte, string, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case event, open := <-session.server.messages:
			if !open {
				return nil, "", errors.New("Codex app-server closed before device authorization completed")
			}
			if event.err != nil {
				return nil, "", event.err
			}
			if event.message.Method == "" {
				return nil, "", errors.New("Codex app-server emitted an uncorrelated response")
			}
			if len(event.message.ID) != 0 {
				_ = session.server.write(map[string]any{"id": event.message.ID, "error": map[string]any{"code": -32000, "message": "Server requests are not authorized"}})
				return nil, "", errors.New("Codex app-server requested unsupported authority")
			}
			if event.message.Method != "account/login/completed" {
				continue
			}
			var completed struct {
				LoginID string  `json:"loginId"`
				Success bool    `json:"success"`
				Error   *string `json:"error"`
			}
			if json.Unmarshal(event.message.Params, &completed) != nil || completed.LoginID != session.loginID {
				return nil, "", errors.New("Codex app-server device authorization completion is invalid")
			}
			if !completed.Success || completed.Error != nil {
				return nil, "", errors.New("Codex device authorization failed")
			}
			raw, err := session.server.call(ctx, "account/read", map[string]bool{"refreshToken": false})
			if err != nil {
				return nil, "", err
			}
			masked, err := maskedAccount(raw)
			if err != nil {
				return nil, "", err
			}
			authJSON, err := readAuthJSON(session.home)
			if err != nil {
				return nil, "", err
			}
			return authJSON, masked, nil
		}
	}
}

func (session *deviceSession) Close() error {
	session.closeOnce.Do(func() {
		session.closeErr = session.server.terminate()
		session.closeErr = errors.Join(session.closeErr, os.RemoveAll(session.home))
	})
	return session.closeErr
}

func maskedAccount(raw json.RawMessage) (string, error) {
	var response struct {
		Account struct {
			Type  string `json:"type"`
			Email string `json:"email"`
		} `json:"account"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Account.Type != "chatgpt" ||
		response.Account.Email == "" || len(response.Account.Email) > 320 {
		return "", errors.New("Codex account metadata is invalid")
	}
	local, domain, found := strings.Cut(response.Account.Email, "@")
	if !found || local == "" || domain == "" {
		return "", errors.New("Codex account metadata is invalid")
	}
	prefix := []rune(local)
	if len(prefix) > 2 {
		prefix = prefix[:2]
	}
	return string(prefix) + "***@" + domain, nil
}

func readAuthJSON(home string) ([]byte, error) {
	path := filepath.Join(home, "auth.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o077 != 0 || info.Size() < 1 || info.Size() > maximumAuthJSONBytes {
		return nil, errors.New("Codex authorization material is unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("read Codex authorization material")
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, maximumAuthJSONBytes+1))
	if err != nil || len(value) < 1 || len(value) > maximumAuthJSONBytes || !json.Valid(value) ||
		bytes.Equal(bytes.TrimSpace(value), []byte("{}")) {
		clear(value)
		return nil, errors.New("Codex authorization material is invalid")
	}
	return value, nil
}

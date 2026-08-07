// Package codexappserver реализует реальный OpenAI Codex device authorization
// по app-server JSONL protocol. Raw auth.json не покидает secret boundary call.
package codexappserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/providerauthorization"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration/egressproxy"
)

const maximumProtocolMessageBytes = 256 << 10

type (
	Config struct {
		Executable, TemporaryRoot, SSLCertificateFile, HTTPSProxy string
		Timeout, PollInterval                                     time.Duration
	}
	Client struct{ config Config }
)

func New(config Config) (*Client, error) {
	proxy, proxyErr := url.Parse(config.HTTPSProxy)
	if !filepath.IsAbs(config.Executable) || !filepath.IsAbs(config.TemporaryRoot) || !filepath.IsAbs(config.SSLCertificateFile) || proxyErr != nil || proxy.Scheme != "http" || proxy.Hostname() == "" || proxy.Port() == "" || proxy.User != nil || proxy.Path != "" || proxy.RawQuery != "" || proxy.Fragment != "" || config.Timeout < time.Minute || config.Timeout > 20*time.Minute || config.PollInterval < 100*time.Millisecond || config.PollInterval > 5*time.Second {
		return nil, errors.New("Codex app-server configuration is invalid")
	}
	return &Client{config: config}, nil
}

type (
	envelope struct {
		ID     json.RawMessage `json:"id,omitempty"`
		Method string          `json:"method,omitempty"`
		Result json.RawMessage `json:"result,omitempty"`
		Params json.RawMessage `json:"params,omitempty"`
		Error  json.RawMessage `json:"error,omitempty"`
	}
	loginStart struct {
		LoginID         string `json:"loginId"`
		VerificationURL string `json:"verificationUrl"`
		UserCode        string `json:"userCode"`
		ExpiresIn       uint64 `json:"expiresIn"`
	}
)

func (client *Client) Authorize(ctx context.Context, onCode func(providerauthorization.DeviceCode) error, cancelled func(context.Context) (bool, error)) (providerauthorization.Result, error) {
	if onCode == nil || cancelled == nil {
		return providerauthorization.Result{}, errors.New("Codex device authorization callbacks are required")
	}
	work, err := os.MkdirTemp(client.config.TemporaryRoot, "codex-device-")
	if err != nil {
		return providerauthorization.Result{}, errors.New("create Codex authorization home")
	}
	defer os.RemoveAll(work)
	if err = os.Chmod(work, 0o700); err != nil {
		return providerauthorization.Result{}, errors.New("secure Codex authorization home")
	}
	processCtx, stop := context.WithTimeout(ctx, client.config.Timeout)
	defer stop()
	command := exec.CommandContext(processCtx, client.config.Executable, "app-server", "--stdio")
	command.Env = client.environment(work)
	stdin, err := command.StdinPipe()
	if err != nil {
		return providerauthorization.Result{}, errors.New("open Codex app-server stdin")
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return providerauthorization.Result{}, errors.New("open Codex app-server stdout")
	}
	var stderr bytes.Buffer
	command.Stderr = &boundedWriter{target: &stderr, remaining: 8 << 10}
	if err = command.Start(); err != nil {
		return providerauthorization.Result{}, errors.New("start Codex app-server")
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	messages := make(chan envelope, 8)
	readErrors := make(chan error, 1)
	go readMessages(stdout, messages, readErrors)
	var writeMu sync.Mutex
	send := func(value any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		raw, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			return errors.New("encode Codex app-server request")
		}
		raw = append(raw, '\n')
		_, writeErr := stdin.Write(raw)
		if writeErr != nil {
			return errors.New("write Codex app-server request")
		}
		return nil
	}
	defer func() {
		_ = stdin.Close()
		stop()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			if command.Process != nil {
				_ = command.Process.Kill()
			}
			<-done
		}
	}()
	if err = send(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "mattercodex-integration-gateway", "version": "1"}, "capabilities": map[string]any{}}}); err != nil {
		return providerauthorization.Result{}, err
	}
	if _, err = waitResponse(processCtx, messages, readErrors, "1"); err != nil {
		return providerauthorization.Result{}, err
	}
	if err = send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return providerauthorization.Result{}, err
	}
	if err = send(map[string]any{"id": 2, "method": "account/login/start", "params": map[string]string{"type": "chatgptDeviceCode"}}); err != nil {
		return providerauthorization.Result{}, err
	}
	response, err := waitResponse(processCtx, messages, readErrors, "2")
	if err != nil {
		return providerauthorization.Result{}, err
	}
	var started loginStart
	if json.Unmarshal(response.Result, &started) != nil {
		return providerauthorization.Result{}, errors.New("Codex device authorization response is invalid")
	}
	verificationURL, verificationErr := url.Parse(started.VerificationURL)
	if verificationErr != nil || verificationURL.Scheme != "https" || verificationURL.Hostname() == "" || verificationURL.User != nil || verificationURL.Fragment != "" ||
		len(started.LoginID) > 256 || started.LoginID == "" || len(started.VerificationURL) > 2048 || started.UserCode == "" || len(started.UserCode) > 128 || started.ExpiresIn < 30 || started.ExpiresIn > 1200 {
		return providerauthorization.Result{}, errors.New("Codex device authorization response is invalid")
	}
	expiresAt := time.Now().UTC().Add(time.Duration(started.ExpiresIn) * time.Second)
	if err = onCode(providerauthorization.DeviceCode{LoginID: started.LoginID, VerificationURL: started.VerificationURL, UserCode: started.UserCode, ExpiresAt: expiresAt}); err != nil {
		return providerauthorization.Result{}, err
	}
	ticker := time.NewTicker(client.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-processCtx.Done():
			_ = cancelLogin(context.WithoutCancel(ctx), send, messages, readErrors, started.LoginID)
			return providerauthorization.Result{}, errors.New("Codex device authorization expired")
		case err = <-readErrors:
			return providerauthorization.Result{}, err
		case message := <-messages:
			if message.Method == "account/login/completed" {
				var completed struct {
					LoginID string `json:"loginId"`
					Success bool   `json:"success"`
					Error   string `json:"error"`
				}
				if json.Unmarshal(message.Params, &completed) != nil || completed.LoginID != started.LoginID {
					return providerauthorization.Result{}, errors.New("Codex login completion is invalid")
				}
				if !completed.Success {
					return providerauthorization.Result{}, errors.New("Codex device authorization denied")
				}
				return client.readResult(processCtx, send, messages, readErrors, work)
			}
		case <-ticker.C:
			closed, checkErr := cancelled(processCtx)
			if checkErr != nil {
				return providerauthorization.Result{}, checkErr
			}
			if closed {
				if err = cancelLogin(processCtx, send, messages, readErrors, started.LoginID); err != nil {
					return providerauthorization.Result{}, err
				}
				return providerauthorization.Result{}, errors.New("Codex device authorization cancelled")
			}
		}
	}
}

func cancelLogin(ctx context.Context, send func(any) error, messages <-chan envelope, readErrors <-chan error, loginID string) error {
	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := send(map[string]any{"id": 3, "method": "account/login/cancel", "params": map[string]string{"loginId": loginID}}); err != nil {
		return errors.New("write Codex login cancellation")
	}
	if _, err := waitResponse(cancelCtx, messages, readErrors, "3"); err != nil {
		return errors.New("Codex login cancellation was not confirmed")
	}
	return nil
}

func (client *Client) readResult(ctx context.Context, send func(any) error, messages <-chan envelope, readErrors <-chan error, work string) (providerauthorization.Result, error) {
	if err := send(map[string]any{"id": 4, "method": "account/read", "params": map[string]bool{"refreshToken": false}}); err != nil {
		return providerauthorization.Result{}, err
	}
	response, err := waitResponse(ctx, messages, readErrors, "4")
	if err != nil {
		return providerauthorization.Result{}, err
	}
	var account struct {
		Account *struct {
			Email    string `json:"email"`
			PlanType string `json:"planType"`
		} `json:"account"`
	}
	if json.Unmarshal(response.Result, &account) != nil || account.Account == nil {
		return providerauthorization.Result{}, errors.New("Codex account readback is invalid")
	}
	path := filepath.Join(work, "auth.json")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 128<<10 {
		return providerauthorization.Result{}, errors.New("Codex credential artifact is unavailable")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return providerauthorization.Result{}, errors.New("read Codex credential artifact")
	}
	masked := maskAccount(account.Account.Email)
	label := account.Account.PlanType
	if len(label) > 64 {
		label = label[:64]
	}
	return providerauthorization.Result{Credential: raw, MaskedAccount: masked, MaskedLabel: label}, nil
}

func maskAccount(value string) string {
	parts := strings.Split(value, "@")
	if len(parts) != 2 || len(parts[0]) < 1 {
		return "configured"
	}
	return string(parts[0][0]) + "***@" + parts[1]
}

func waitResponse(ctx context.Context, messages <-chan envelope, readErrors <-chan error, id string) (envelope, error) {
	for {
		select {
		case <-ctx.Done():
			return envelope{}, errors.New("Codex app-server response timeout")
		case err := <-readErrors:
			return envelope{}, err
		case message := <-messages:
			if string(message.ID) == id {
				if len(message.Error) > 0 && string(message.Error) != "null" {
					return envelope{}, errors.New("Codex app-server request rejected")
				}
				return message, nil
			}
		}
	}
}

func readMessages(reader io.Reader, output chan<- envelope, fail chan<- error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maximumProtocolMessageBytes)
	for scanner.Scan() {
		var value envelope
		if json.Unmarshal(scanner.Bytes(), &value) != nil {
			fail <- errors.New("Codex app-server protocol message is invalid")
			return
		}
		output <- value
	}
	if err := scanner.Err(); err != nil {
		fail <- errors.New("read Codex app-server protocol")
	}
}

type boundedWriter struct {
	target    *bytes.Buffer
	remaining int
}

func (writer *boundedWriter) Write(value []byte) (int, error) {
	size := len(value)
	if writer.remaining > 0 {
		part := value
		if len(part) > writer.remaining {
			part = part[:writer.remaining]
		}
		_, _ = writer.target.Write(part)
		writer.remaining -= len(part)
	}
	return size, nil
}

func (client *Client) Test(ctx context.Context, credential []byte) error {
	if len(credential) == 0 || len(credential) > 128<<10 {
		return errors.New("Codex credential is invalid")
	}
	var value map[string]any
	if json.Unmarshal(credential, &value) != nil || len(value) == 0 {
		return errors.New("Codex credential format is invalid")
	}
	work, err := os.MkdirTemp(client.config.TemporaryRoot, "codex-test-")
	if err != nil {
		return errors.New("create Codex test home")
	}
	defer os.RemoveAll(work)
	if err = os.Chmod(work, 0o700); err != nil {
		return errors.New("secure Codex test home")
	}
	credentialPath := filepath.Join(work, "auth.json")
	if err = os.WriteFile(credentialPath, credential, 0o600); err != nil {
		return errors.New("stage Codex test credential")
	}
	testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(testCtx, client.config.Executable, "app-server", "--stdio")
	command.Env = client.environment(work)
	stdin, err := command.StdinPipe()
	if err != nil {
		return errors.New("open Codex test stdin")
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return errors.New("open Codex test stdout")
	}
	command.Stderr = &boundedWriter{target: &bytes.Buffer{}, remaining: 8 << 10}
	if err = command.Start(); err != nil {
		return errors.New("start Codex test app-server")
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		_ = stdin.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			if command.Process != nil {
				_ = command.Process.Kill()
			}
			<-done
		}
	}()
	messages := make(chan envelope, 8)
	readErrors := make(chan error, 1)
	go readMessages(stdout, messages, readErrors)
	send := func(request any) error {
		raw, marshalErr := json.Marshal(request)
		if marshalErr != nil {
			return errors.New("encode Codex test request")
		}
		_, writeErr := stdin.Write(append(raw, '\n'))
		return writeErr
	}
	if err = send(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "mattercodex-integration-test", "version": "1"}, "capabilities": map[string]any{}}}); err != nil {
		return errors.New("write Codex test initialize")
	}
	if _, err = waitResponse(testCtx, messages, readErrors, "1"); err != nil {
		return err
	}
	if err = send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return errors.New("write Codex test initialized")
	}
	if err = send(map[string]any{"id": 2, "method": "account/read", "params": map[string]bool{"refreshToken": false}}); err != nil {
		return errors.New("write Codex account readback")
	}
	response, err := waitResponse(testCtx, messages, readErrors, "2")
	if err != nil {
		return err
	}
	var account struct {
		Account json.RawMessage `json:"account"`
	}
	if json.Unmarshal(response.Result, &account) != nil || len(account.Account) == 0 || string(account.Account) == "null" {
		return errors.New("Codex test account readback is invalid")
	}
	return nil
}

func (client *Client) Revoke(ctx context.Context, credential []byte) error {
	if len(credential) == 0 || len(credential) > 128<<10 {
		return errors.New("Codex credential is invalid")
	}
	var value map[string]any
	if json.Unmarshal(credential, &value) != nil || len(value) == 0 {
		return errors.New("Codex credential format is invalid")
	}
	work, err := os.MkdirTemp(client.config.TemporaryRoot, "codex-revoke-")
	if err != nil {
		return errors.New("create Codex revoke home")
	}
	defer os.RemoveAll(work)
	if err = os.Chmod(work, 0o700); err != nil {
		return errors.New("secure Codex revoke home")
	}
	credentialPath := filepath.Join(work, "auth.json")
	if err = os.WriteFile(credentialPath, credential, 0o600); err != nil {
		return errors.New("stage Codex revoke credential")
	}
	revokeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(revokeCtx, client.config.Executable, "app-server", "--stdio")
	command.Env = client.environment(work)
	stdin, err := command.StdinPipe()
	if err != nil {
		return errors.New("open Codex revoke stdin")
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return errors.New("open Codex revoke stdout")
	}
	command.Stderr = &boundedWriter{target: &bytes.Buffer{}, remaining: 8 << 10}
	if err = command.Start(); err != nil {
		return errors.New("start Codex revoke app-server")
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		_ = stdin.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			if command.Process != nil {
				_ = command.Process.Kill()
			}
			<-done
		}
	}()
	messages := make(chan envelope, 8)
	readErrors := make(chan error, 1)
	go readMessages(stdout, messages, readErrors)
	send := func(request any) error {
		raw, marshalErr := json.Marshal(request)
		if marshalErr != nil {
			return errors.New("encode Codex revoke request")
		}
		_, writeErr := stdin.Write(append(raw, '\n'))
		return writeErr
	}
	if err = send(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "mattercodex-integration-revoke", "version": "1"}, "capabilities": map[string]any{}}}); err != nil {
		return errors.New("write Codex revoke initialize")
	}
	if _, err = waitResponse(revokeCtx, messages, readErrors, "1"); err != nil {
		return err
	}
	if err = send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return errors.New("write Codex revoke initialized")
	}
	if err = send(map[string]any{"id": 2, "method": "account/logout", "params": map[string]any{}}); err != nil {
		return errors.New("write Codex account logout")
	}
	if _, err = waitResponse(revokeCtx, messages, readErrors, "2"); err != nil {
		return err
	}
	if err = send(map[string]any{"id": 3, "method": "account/read", "params": map[string]bool{"refreshToken": false}}); err != nil {
		return errors.New("write Codex revoked account readback")
	}
	response, err := waitResponse(revokeCtx, messages, readErrors, "3")
	if err != nil {
		return err
	}
	var account struct {
		Account json.RawMessage `json:"account"`
	}
	if json.Unmarshal(response.Result, &account) != nil || len(account.Account) == 0 || string(account.Account) != "null" {
		return errors.New("Codex account logout readback is invalid")
	}
	return nil
}

func (client *Client) Check(ctx context.Context) error {
	command := exec.CommandContext(ctx, client.config.Executable, "--version")
	command.Env = []string{"HOME=" + client.config.TemporaryRoot, "PATH=" + filepath.Dir(client.config.Executable) + ":/usr/bin:/bin", "NO_COLOR=1"}
	output, err := command.Output()
	if err != nil || len(output) == 0 || len(output) > 4096 {
		return errors.New("Codex provider adapter is unavailable")
	}
	return egressproxy.Check(ctx, client.config.HTTPSProxy)
}

func (client *Client) environment(home string) []string {
	return []string{"CODEX_HOME=" + home, "HOME=" + home, "PATH=" + filepath.Dir(client.config.Executable) + ":/usr/bin:/bin", "SSL_CERT_FILE=" + client.config.SSLCertificateFile, "HTTPS_PROXY=" + client.config.HTTPSProxy, "HTTP_PROXY=" + client.config.HTTPSProxy, "NO_PROXY=", "NO_COLOR=1"}
}

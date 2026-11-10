//go:build ignore

// Command workspace_executor_probe drives only a disposable synthetic UDS fixture.
// Команда workspace_executor_probe проверяет лишь disposable synthetic UDS fixture.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// result decodes only the fixed fixture response, without credential or endpoint selectors.
// result разбирает лишь fixed fixture response без credential и endpoint selectors.
type result struct {
	Status    string `json:"status"`
	ExitCode  int    `json:"exit_code"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}

// call sends one local request without retry; a lost response is a fixture failure.
// call отправляет один local request без retry; потерянный response означает failure оснастки.
func call(client *http.Client, action string, fields map[string]any) (result, error) {
	fields["action"] = action
	fields["binding"] = strings.Repeat("a", 64)
	raw, _ := json.Marshal(fields)
	response, err := client.Post("http://fixture/execute", "application/json", bytes.NewReader(raw))
	if err != nil {
		return result{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return result{}, fmt.Errorf("executor HTTP status %d", response.StatusCode)
	}
	var output result
	if json.NewDecoder(io.LimitReader(response.Body, 512<<10)).Decode(&output) != nil {
		return result{}, fmt.Errorf("invalid fixture result")
	}
	return output, nil
}

// run checks secret-free execution, filesystem confinement, detached-child cleanup and timeout.
// run проверяет execution без секретов, filesystem confinement, detached-child cleanup и timeout.
func run() error {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/run/kodex/workspace/authority/executor.sock")
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	if os.Geteuid() != 10001 {
		_, err := call(client, "INITIALIZE", map[string]any{"commands": []string{"bash"}})
		if err == nil {
			return fmt.Errorf("foreign UID reached executor")
		}
		fmt.Println("PASS rejected foreign UID")
		return nil
	}
	initialized, err := call(client, "INITIALIZE", map[string]any{"commands": []string{"bash"}})
	if err != nil || initialized.Status != "READY" {
		return fmt.Errorf("initialize failed: %v", err)
	}
	checks := []struct {
		script, status, output string
		timeout                int
	}{
		{`test -z "${SYNTHETIC_MODEL_KEY:-}" && test -z "${KODEX_RUNTIME_REVISION_FILE:-}" && printf safe`, "SUCCEEDED", "safe", 5},
		{`if rm /run/kodex/workspace/authority/executor.sock 2>/dev/null; then exit 11; fi; if (printf bad > /run/kodex/workspace/authority/replacement) 2>/dev/null; then exit 12; fi; printf confined`, "SUCCEEDED", "confined", 5},
		{`setsid bash -c 'sleep 2; printf survived > /tmp/survivor' >/dev/null 2>&1 & printf detached`, "SUCCEEDED", "detached", 5},
		{`sleep 3; test ! -e /tmp/survivor && printf reaped`, "SUCCEEDED", "reaped", 5},
		{`sleep 10`, "TIMED_OUT", "", 1},
		{`printf after-timeout`, "SUCCEEDED", "after-timeout", 5},
	}
	for index, check := range checks {
		value, err := call(client, "RUN", map[string]any{"command": "bash", "arguments": []string{"-c", check.script}, "directory": "work", "timeout_seconds": check.timeout})
		if err != nil || value.Status != check.status || value.Output != check.output || value.Truncated {
			return fmt.Errorf("fixture check %d failed: status=%s exit=%d output=%q error=%v", index, value.Status, value.ExitCode, value.Output, err)
		}
	}
	stopped, err := call(client, "QUIESCE", map[string]any{})
	if err != nil || stopped.Status != "QUIESCENT" {
		return fmt.Errorf("quiescence failed: %v", err)
	}
	fmt.Println("PASS credentials, control socket, detached children, timeout and quiescence")
	return nil
}

// main reports only synthetic fixture status and safe diagnostics, never process environment.
// main выводит лишь synthetic fixture status и безопасную диагностику, не process environment.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

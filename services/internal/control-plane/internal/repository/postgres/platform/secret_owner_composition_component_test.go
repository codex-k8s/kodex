package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Публичная PG-оснастка выделяет пустую template-копию. Два независимых
// тестовых бинаря связываются настоящим Proto/gRPC; DB-полномочия получает
// исключительно CP owner, а broker видит лишь loopback rendezvous.
func TestSecretOwnerCompositionComponent(t *testing.T) {
	dsn := isolatedAssistantComponentDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 240*time.Second)
	defer cancel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve public composition sources")
	}
	cpModule := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../.."))
	brokerModule := filepath.Join(cpModule, "..", "secret-broker")
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal("protect composition temporary directory")
	}
	environment := []string{"GOTOOLCHAIN=go1.26.6", "GOMAXPROCS=2", "GOWORK=off", "GOENV=off"}
	for _, key := range []string{"PATH", "HOME", "GOCACHE", "GOMODCACHE", "TMPDIR", "XDG_CACHE_HOME"} {
		if value := os.Getenv(key); value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	build := func(module, packageName, binary string) {
		t.Helper()
		command := exec.CommandContext(ctx, "go", "test", "-p", "2", "-c", "-o", binary, packageName)
		command.Dir = module
		command.Env = environment
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("composition test binary build failed: %v\n%s", err, output)
		}
	}
	ownerBinary, brokerBinary := filepath.Join(directory, "owner-test"), filepath.Join(directory, "broker-test")
	build(cpModule, "./internal/transport/grpc", ownerBinary)
	build(brokerModule, "./internal/app", brokerBinary)
	ready := filepath.Join(directory, "ready.json")
	common := append(append([]string(nil), environment...), "KODEX_SECRET_COMPOSITION_SYNTHETIC=1", "KODEX_SECRET_COMPOSITION_READY_FILE="+ready)
	owner := exec.CommandContext(ctx, ownerBinary, "-test.run=^TestSecretOwnerLoopbackHarness$", "-test.timeout=160s")
	owner.Env = append(append([]string(nil), common...), "KODEX_SECRET_COMPOSITION_OWNER_DSN="+dsn)
	owner.Dir = cpModule
	var ownerOutput bytes.Buffer
	owner.Stdout, owner.Stderr = &ownerOutput, &ownerOutput
	if err := owner.Start(); err != nil {
		t.Fatal("start isolated owner test binary")
	}
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- owner.Wait() }()
	joined := false
	defer func() {
		if !joined {
			_ = owner.Process.Kill()
			<-ownerDone
		}
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var receipt struct{ Address, ProjectRef string }
		if raw, err := os.ReadFile(ready); err == nil && json.Unmarshal(raw, &receipt) == nil && receipt.Address != "" && receipt.ProjectRef != "" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("composition rendezvous exceeded bounded budget")
		case err := <-ownerDone:
			joined = true
			t.Fatalf("owner failed before rendezvous: %v\n%s", err, ownerOutput.String())
		case <-ticker.C:
		}
	}
	consumer := exec.CommandContext(ctx, brokerBinary, "-test.run=^TestSecretOwnerCompositionConsumer$", "-test.timeout=100s", "-test.v")
	consumer.Dir = brokerModule
	consumer.Env = common
	output, consumerErr := consumer.CombinedOutput()
	if err := os.WriteFile(ready+".stop", nil, 0o600); err != nil {
		t.Fatal("stop isolated owner harness")
	}
	select {
	case ownerErr := <-ownerDone:
		joined = true
		if ownerErr != nil {
			t.Fatalf("actual owner composition failed: %v\n%s", ownerErr, ownerOutput.String())
		}
	case <-ctx.Done():
		t.Fatal("owner did not join within composition budget")
	}
	if consumerErr != nil {
		t.Fatalf("actual broker composition failed: %v\n%s", consumerErr, output)
	}
	t.Log("actual CP owner + Proto/gRPC + broker AEAD + immutable fake Kubernetes + fenced recovery passed for PROJECT and ORGANIZATION; live mTLS/SSO/inference NOT RUN")
}

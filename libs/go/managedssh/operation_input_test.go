package managedssh

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// operationInputFixture serializes synthetic owner scalars with the actual standard-library canonical map encoding, without credentials or operator authority.
// operationInputFixture сериализует synthetic owner scalars actual standard-library canonical map encoding без credentials либо operator authority.
func operationInputFixture(t *testing.T, input map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestOperationInputBindsEveryManagedRecipe exercises all advertised finite recipes with real helper codec round trips; caller input supplies no attempt UUID, profile digest or remote executable.
// TestOperationInputBindsEveryManagedRecipe проверяет все advertised finite recipes через actual helper codec round trips; caller input не задаёт attempt UUID, profile digest либо remote executable.
func TestOperationInputBindsEveryManagedRecipe(t *testing.T) {
	for _, test := range []struct {
		operation Operation
		fields    map[string]any
		effect    EffectClass
	}{
		{Inspect, map[string]any{}, ReadOnly}, {SystemRead, map[string]any{}, ReadOnly},
		{ProcessList, map[string]any{"limit": 128}, ReadOnly},
		{FileList, map[string]any{"resource": "work", "limit": 64}, ReadOnly},
		{FileStat, map[string]any{"resource": "work", "path": "config.json"}, ReadOnly},
		{FileRead, map[string]any{"resource": "work", "path": "config.json", "expected": strings.Repeat("a", 64), "offset": 1048576, "limit": 16384}, ReadOnly},
		{FileCreate, map[string]any{"resource": "work", "path": "config.json", "content_base64": ""}, ScopedChange},
		{FileReplace, map[string]any{"resource": "work", "path": "config.json", "expected": strings.Repeat("a", 64), "content_base64": "YQ=="}, ScopedChange},
		{FileDelete, map[string]any{"resource": "work", "path": "config.json", "expected": strings.Repeat("a", 64)}, EachChange},
		{ServiceRead, map[string]any{"resource": "worker"}, ReadOnly},
		{ServiceRestart, map[string]any{"resource": "worker"}, EachChange},
		{ServiceReload, map[string]any{"resource": "worker"}, EachChange},
		{JournalRead, map[string]any{"resource": "worker", "limit": 64}, ReadOnly},
	} {
		t.Run(test.operation.Name(), func(t *testing.T) {
			input, err := DecodeOperationInput(test.operation.Name(), operationInputFixture(t, test.fields))
			if err != nil || input.Kind() != ManagedInput {
				t.Fatal("typed recipe rejected", err)
			}
			defer input.Clear()
			if effect, err := input.Effect(); err != nil || effect != test.effect {
				t.Fatal("recipe risk changed")
			}
			request, err := input.Request("33333333-3333-4333-8333-333333333333", strings.Repeat("b", 64))
			if err != nil || request.Operation != test.operation {
				t.Fatal("owner pins did not bind recipe", err)
			}
			raw, err := EncodeRequest(request)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeRequest(raw)
			if err != nil || decoded.Operation != test.operation || decoded.AttemptID != request.AttemptID {
				t.Fatal("real helper codec disagrees", err)
			}
			if _, err := input.Request("caller_reference", strings.Repeat("b", 64)); err == nil {
				t.Fatal("non-UUID caller reference became helper attempt")
			}
		})
	}
}

// TestOperationInputNeverClassifiesShellAsRead verifies command syntax cannot lower EACH risk and that byte limits, owned stdin copies, redaction and explicit clearing survive the public decoder boundary.
// TestOperationInputNeverClassifiesShellAsRead проверяет невозможность понизить EACH risk через command syntax, а также byte limits, owned stdin copies, redaction и explicit clearing на public decoder boundary.
func TestOperationInputNeverClassifiesShellAsRead(t *testing.T) {
	for _, command := range []string{"cat /etc/hostname", "echo synthetic; rm -f operator-selected-path", "printf '%s' synthetic-canary", "a" + strings.Repeat("я", 4095)} {
		input, err := DecodeOperationInput("ssh.execute", operationInputFixture(t, map[string]any{"command": command, "stdin_base64": "YQ==", "maximum_output": 32768}))
		if err != nil || input.Kind() != ExecuteInput {
			t.Fatal("execute input rejected", err)
		}
		if effect, err := input.Effect(); err != nil || effect != EachChange {
			t.Fatal("shell syntax became READ")
		}
		text, stdin, limit, err := input.Execution()
		if err != nil || text != command || string(stdin) != "a" || limit != 32768 {
			t.Fatal("execute values changed")
		}
		stdin[0] = 'b'
		_, again, _, _ := input.Execution()
		if string(again) != "a" {
			t.Fatal("caller changed owned stdin")
		}
		clear(stdin)
		clear(again)
		if _, err := input.Request("33333333-3333-4333-8333-333333333333", strings.Repeat("b", 64)); err == nil {
			t.Fatal("arbitrary execute became managed recipe")
		}
		if strings.Contains(fmt.Sprintf("%v %#v", input, input), command) {
			t.Fatal("command leaked through formatting")
		}
		input.Clear()
		if _, err := input.Digest(); err == nil || input.Kind() != 0 {
			t.Fatal("cleared input remained executable")
		}
	}
}

// TestOperationInputRejectsAmbiguousScalarsAndAuthority closes duplicate/alias/null, alternate number/UTF/base64, oversized command/content and caller attempt/profile channels before binding a request.
// TestOperationInputRejectsAmbiguousScalarsAndAuthority закрывает duplicate/alias/null, alternate number/UTF/base64, oversized command/content и caller attempt/profile channels до binding request.
func TestOperationInputRejectsAmbiguousScalarsAndAuthority(t *testing.T) {
	for _, test := range []struct{ operation, raw string }{
		{"ssh.execute", `{"command":"first","command":"second","maximum_output":1}`},
		{"ssh.execute", `{"Command":"first","maximum_output":1}`},
		{"ssh.execute", `{"command":null,"maximum_output":1}`},
		{"ssh.execute", `{"command":"x","maximum_output":null}`},
		{"ssh.execute", `{"command":"x","maximum_output":1,"stdin_base64":null}`},
		{"ssh.execute", `{"command":"x","maximum_output":1.0}`},
		{"ssh.execute", `{"command":"x","maximum_output":1e0}`},
		{"ssh.execute", `{"command":"x","maximum_output":-0}`},
		{"ssh.execute", `{"command":"x","maximum_output":18446744073709551616}`},
		{"ssh.execute", `{"command":"\ud800","maximum_output":1}`},
		{"ssh.execute", `{"command":"x","maximum_output":1,"stdin_base64":"YQ==\n"}`},
		{"ssh.execute", `{"command":"x","maximum_output":1,"stdin_base64":"YR=="}`},
		{"ssh.execute", `{"command":"x","maximum_output":1,"stdin_base64":"YQ"}`},
		{"ssh.execute", `{"command":"x","maximum_output":1,"stdin_base64":"YQ__"}`},
		{"ssh.execute", ` {"command":"x","maximum_output":1}`},
		{"ssh.execute", `{"maximum_output":1,"command":"x"}`},
		{"ssh.execute", `{"command":"x","maximum_output":1} {}`},
		{"ssh.target.inspect", `{"command":"cat /etc/hostname"}`},
		{"ssh.identity.inspect", `{"profile_digest":"caller"}`},
		{"ssh.file.list", `{"limit":1,"path":"../escape","resource":"work"}`},
		{"ssh.process.list", `{"limit":0}`},
		{"ssh.journal.read", `{"limit":65,"resource":"worker"}`},
		{"ssh.receipt.read", `{"attempt_id":"33333333-3333-4333-8333-333333333333"}`},
		{"ssh.receipt.read", `{"receipt_invocation_ref":"ssu_other"}`},
		{"ssh.unknown", `{}`},
	} {
		if input, err := DecodeOperationInput(test.operation, []byte(test.raw)); err == nil || input.Kind() != 0 {
			t.Fatalf("ambiguous input accepted for %s", test.operation)
		}
	}
	for _, length := range []int{MaximumCommand + 1, 32768} {
		if _, err := DecodeOperationInput("ssh.execute", operationInputFixture(t, map[string]any{"command": strings.Repeat("x", length), "maximum_output": 1})); err == nil {
			t.Fatal("unsupported command bytes accepted")
		}
	}
	for _, length := range []int{MaximumContent, MaximumContent + 1, MaximumContent + 2} {
		raw := operationInputFixture(t, map[string]any{"resource": "work", "path": "data", "content_base64": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", length)))})
		input, err := DecodeOperationInput(FileCreate.Name(), raw)
		if (err == nil) != (length == MaximumContent) {
			t.Fatal("base64 encoded bound substituted for decoded byte bound")
		}
		input.Clear()
	}
}

// TestOperationInputReceiptUsesOriginalOwnerReference prevents a new read invocation from choosing the original helper request UUID or digest; actual same-owner receipt lookup remains mandatory.
// TestOperationInputReceiptUsesOriginalOwnerReference не позволяет новой read invocation выбирать original helper request UUID либо digest; actual same-owner receipt lookup остаётся обязательным.
func TestOperationInputReceiptUsesOriginalOwnerReference(t *testing.T) {
	input, err := DecodeOperationInput(ReceiptRead.Name(), []byte(`{"receipt_invocation_ref":"inv_synthetic_original"}`))
	if err != nil || input.Kind() != ReceiptInput {
		t.Fatal(err)
	}
	defer input.Clear()
	if ref, err := input.ReceiptReference(); err != nil || ref != "inv_synthetic_original" {
		t.Fatal("original reference lost")
	}
	if _, err := input.Request("33333333-3333-4333-8333-333333333333", strings.Repeat("b", 64)); err == nil {
		t.Fatal("new read attempt replaced original helper attempt")
	}
	if _, _, _, err := input.Execution(); err == nil {
		t.Fatal("receipt read acquired arbitrary execute")
	}
}

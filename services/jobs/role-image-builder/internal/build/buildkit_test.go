package build

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestProvenanceAttestationPinsSLSAV1 fixes the exact requested SLSA version, mode and builder identity string.
// It checks configuration bytes, not the presence or validity of a remote attestation.
// TestProvenanceAttestationPinsSLSAV1 закрепляет точную строку запрашиваемой версии SLSA, режима и идентичности сборщика.
// Проверяются байты настройки, а не наличие или достоверность удалённого свидетельства.
func TestProvenanceAttestationPinsSLSAV1(t *testing.T) {
	t.Parallel()

	want := "mode=min,version=v1,builder-id=spiffe://kodex.local/ns/kodex-system/sa/role-image-builder"
	if provenanceAttestation != want {
		t.Fatalf("provenance attestation = %q, want %q", provenanceAttestation, want)
	}
}

// TestBuildKitReadinessUsesSupportedTinyLocalExport checks pinned runtime executable checks and the marker-only local export.
// It rejects cacheonly configuration through rendered bytes without invoking BuildKit or a registry.
// TestBuildKitReadinessUsesSupportedTinyLocalExport проверяет закреплённые проверки исполняемых файлов runtime и локальный экспорт одного маркера.
// По сформированным байтам отклоняется настройка cacheonly без вызова BuildKit или реестра.
func TestBuildKitReadinessUsesSupportedTinyLocalExport(t *testing.T) {
	t.Parallel()
	dockerfile := string(buildKitReadinessDockerfile(
		"registry.example/kodex/dockerfile",
		strings.Repeat("a", 64),
		"registry.example/kodex/agent-runner",
		"sha256:"+strings.Repeat("b", 64),
	))
	for _, required := range []string{
		"FROM registry.example/kodex/agent-runner@sha256:" + strings.Repeat("b", 64) + " AS verify",
		"test -x /usr/local/bin/kodex-init",
		"test -x /usr/local/bin/kodex-agent-runner",
		"printf ready > /tmp/kodex-readiness",
		"FROM scratch",
		"COPY --from=verify /tmp/kodex-readiness /kodex-readiness",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Fatalf("readiness Dockerfile does not contain %q", required)
		}
	}
	if strings.Contains(dockerfile, "cacheonly") {
		t.Fatal("readiness Dockerfile references unsupported cacheonly exporter")
	}

	outputDirectory := filepath.Join(t.TempDir(), "output")
	if got, want := buildKitReadinessOutput(outputDirectory), "type=local,dest="+outputDirectory; got != want {
		t.Fatalf("readiness output = %q, want %q", got, want)
	}
}

// TestBuildctlCommandPreservesTransportAndCallerOwnership checks exact mTLS argument order for worker and build commands.
// It verifies isolated argument storage and caller-owned environment/streams without starting any subprocess.
// TestBuildctlCommandPreservesTransportAndCallerOwnership проверяет точный порядок аргументов mTLS для команд исполнителей и сборки.
// Проверяются независимость хранения аргументов и владение окружением и потоками без запуска дочерних процессов.
func TestBuildctlCommandPreservesTransportAndCallerOwnership(t *testing.T) {
	t.Parallel()
	executor := &Executor{config: Config{
		Binary: "/synthetic/buildctl", Address: "tcp://builder.invalid:1234", TLSServerName: "builder.invalid",
		CAFile: "/synthetic/ca.pem", CertificateFile: "/synthetic/client.pem", PrivateKeyFile: "/synthetic/client-key.pem",
	}}
	prefix := []string{"/synthetic/buildctl", "--addr", "tcp://builder.invalid:1234",
		"--tlscacert", "/synthetic/ca.pem", "--tlscert", "/synthetic/client.pem",
		"--tlskey", "/synthetic/client-key.pem", "--tlsservername", "builder.invalid"}
	for _, arguments := range [][]string{
		{"debug", "workers"},
		{"build", "--frontend", "dockerfile.v0", "--local", "context=/synthetic/context", "--progress=rawjson"},
	} {
		t.Run(arguments[0], func(t *testing.T) {
			original := append([]string(nil), arguments...)
			command := executor.buildctlCommand(context.Background(), arguments...)
			want := append(append([]string(nil), prefix...), original...)
			if command.Path != executor.config.Binary || !reflect.DeepEqual(command.Args, want) {
				t.Fatalf("command = %q, want %q", command.Args, want)
			}
			if command.Env != nil || command.Stdout != nil || command.Stderr != nil || command.Stdin != nil || command.Dir != "" || command.Cancel == nil || command.Process != nil {
				t.Fatal("command construction changed caller-owned lifecycle or streams")
			}
			arguments[0] = "caller-mutated"
			if !reflect.DeepEqual(command.Args, want) {
				t.Fatal("command shares caller argument storage")
			}
			command.Args[1], command.Args[len(prefix)] = "command-mutated", "command-mutated"
			if arguments[0] != "caller-mutated" {
				t.Fatal("caller shares command argument storage")
			}
			fresh := executor.buildctlCommand(context.Background(), original...)
			if !reflect.DeepEqual(fresh.Args, want) {
				t.Fatal("command mutation affected a later transport prefix")
			}
		})
	}
}

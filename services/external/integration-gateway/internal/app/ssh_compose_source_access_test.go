package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// deniedSSHSourceOwner models the fresh owner revocation check, not repository or network access.
// An embedded nil client makes any unexpected RPC fail instead of granting synthetic authority.
// deniedSSHSourceOwner моделирует свежий owner revocation check, а не repository или network.
// Встроенный nil client делает неожиданный RPC ошибкой вместо выдачи синтетических полномочий.
type deniedSSHSourceOwner struct {
	cp.RuntimeWorkServiceClient
	calls int
}

// RenewSSHComposeInvocation denies before any source credential or Git request is permitted.
// RenewSSHComposeInvocation отказывает до разрешения source credential либо Git request.
func (owner *deniedSSHSourceOwner) RenewSSHComposeInvocation(context.Context, *cp.RenewSSHComposeInvocationRequest, ...grpc.CallOption) (*cp.RenewSSHComposeInvocationResponse, error) {
	owner.calls++
	return nil, status.Error(codes.PermissionDenied, "source authority denied")
}

// deniedSSHSourceBroker models a fresh protected projection denial without returning private bytes.
// deniedSSHSourceBroker моделирует свежий protected projection denial без выдачи private bytes.
type deniedSSHSourceBroker struct {
	sb.RuntimeCredentialProjectionServiceClient
	calls int
}

// ProjectSSHComposeCredential refuses inspect admission before source fetching or SSH execution.
// ProjectSSHComposeCredential отклоняет inspect admission до чтения source либо SSH execution.
func (broker *deniedSSHSourceBroker) ProjectSSHComposeCredential(context.Context, *sb.ProjectSSHComposeCredentialRequest, ...grpc.CallOption) (*sb.ProjectSSHComposeCredentialResponse, error) {
	broker.calls++
	return nil, status.Error(codes.PermissionDenied, "source authority denied")
}

// TestSSHSourceReadsRequireFreshOwnerAdmission executes the actual worker entrypoint with a
// deliberately unusable adapter. A denied fresh owner/broker check must return before touching
// source credentials, network or SSH; a stale claim alone cannot authorize hidden Git reads.
// TestSSHSourceReadsRequireFreshOwnerAdmission вызывает настоящий worker с намеренно
// недоступным adapter. Свежий отказ owner/broker должен вернуться до source credentials,
// network и SSH; прежний claim сам по себе не разрешает скрытый Git read.
func TestSSHSourceReadsRequireFreshOwnerAdmission(t *testing.T) {
	for _, operation := range []string{"ssh.compose.deploy", "ssh.compose.inspect"} {
		t.Run(operation, func(t *testing.T) {
			host := append([]byte{0, 0, 0, 11}, []byte("ssh-ed25519")...)
			host = append(host, 0, 0, 0, 32)
			host = append(host, make([]byte, 32)...)
			snapshot := sshcompose.Snapshot{Version: 1, Target: sshcompose.Target{Ref: "target_fixture", ProjectRef: "project_fixture",
				Repository: "example/project", Address: "192.0.2.10", Port: 22, User: "deployer", HostKey: "ssh-ed25519 " + base64.StdEncoding.EncodeToString(host),
				PublishedPort: 8080, RuntimeImage: "registry.example/app@sha256:" + strings.Repeat("a", 64)}, Source: &sshcompose.SourceBinding{ConnectionRef: "int_source_fixture"}}
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			owner, broker := &deniedSSHSourceOwner{}, &deniedSSHSourceBroker{}
			worker := &sshComposeWorker{control: &controlplaneclient.Client{Runtime: owner}, broker: broker,
				config: Config{InstanceID: "source-authority-fixture", RequestTimeout: time.Second, OperationTimeout: time.Second}}
			claim := &cp.IntegrationInvocationClaim{DefinitionKey: "ssh-compose", Operation: operation, InvocationRef: "inv_source_fixture", SshComposeSnapshot: raw,
				Lease: &cp.WorkLease{Ref: "lease_fixture", Fence: "fence_fixture", Generation: 1}}
			_, err = worker.execute(t.Context(), claim)
			if status.Code(err) != codes.PermissionDenied {
				t.Fatal("fresh denial did not precede hidden source access")
			}
			wantOwner, wantBroker := 1, 0
			if operation == "ssh.compose.inspect" {
				wantOwner, wantBroker = 0, 1
			}
			if owner.calls != wantOwner || broker.calls != wantBroker {
				t.Fatal("unexpected owner admission ordering")
			}
		})
	}
}

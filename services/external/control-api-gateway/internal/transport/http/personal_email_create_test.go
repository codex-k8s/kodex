package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// personalEmailCreateRPC records the real HTTP-to-generated-RPC body mapping and supplies the
// existing enabled/NOT_CONNECTED Create semantics; it does not authenticate an actual user.
// personalEmailCreateRPC записывает реальное HTTP-to-generated-RPC body mapping и задаёт прежнюю
// enabled/NOT_CONNECTED семантику Create; действительный пользователь не аутентифицируется.
type personalEmailCreateRPC struct {
	grpc.ClientConnInterface
	calls int
}

// Invoke returns a canonical personal creation receipt with trimmed Unicode name and safe owner
// metadata. The actual owner and PostgreSQL create transaction remain outside this HTTP unit test.
// Invoke возвращает каноническую квитанцию личного создания с trimmed Unicode name и безопасной
// owner metadata. Настоящий owner и PostgreSQL create transaction не проверяются HTTP unit test.
func (client *personalEmailCreateRPC) Invoke(_ context.Context, method string, args, reply any, _ ...grpc.CallOption) error {
	if method != cp.PersonalEmailMailboxService_CreatePersonalEmailMailboxConnection_FullMethodName {
		panic("unexpected personal email RPC")
	}
	client.calls++
	request := args.(*cp.CreatePersonalEmailMailboxConnectionRequest)
	response := &cp.CreatePersonalEmailMailboxConnectionResponse{Mailbox: &cp.MyEmailMailbox{Name: strings.TrimSpace(request.GetName()), Enabled: true, Available: false, Ownership: &cp.EmailMailboxOwnership{ConnectionRef: "int_mailbox_owner", ConnectionVersion: 1, Policy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED, State: cp.EmailMailboxOwnershipState_EMAIL_MAILBOX_OWNERSHIP_STATE_ACTIVE, Generation: 1, OwnerEligible: true}}}
	proto.Merge(reply.(proto.Message), response)
	return nil
}

// TestPersonalEmailCreateUnicodeHTTPBoundary proves valid long Unicode and trimmed names survive
// the actual generated route while overlength/control names never call RPC; creation remains
// enabled but unavailable, and no caller owner/project/service selector is sent.
// TestPersonalEmailCreateUnicodeHTTPBoundary доказывает прохождение длинных Unicode и trimmed
// имён через generated route, а overlength/control имена не вызывают RPC; созданное подключение
// включено, но недоступно, и caller owner/project/service selector не передаётся.
func TestPersonalEmailCreateUnicodeHTTPBoundary(t *testing.T) {
	for _, test := range []struct {
		name  string
		valid bool
	}{{strings.Repeat("я", 160), true}, {strings.Repeat("😀", 160), true}, {"  Ящик  ", true}, {strings.Repeat("я", 161), false}, {"name\nother", false}, {"   ", false}} {
		client := &personalEmailCreateRPC{}
		handler := generated.Handler(&Server{control: &controlplaneclient.Client{PersonalEmail: cp.NewPersonalEmailMailboxServiceClient(client)}})
		raw, _ := json.Marshal(map[string]string{"name": test.name, "sender": "owner@example.test"})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/personal-email-mailboxes", bytes.NewReader(raw))
		request.Header.Set("Idempotency-Key", "personal-email-name-fixture")
		request.Header.Set("X-CSRF-Token", strings.Repeat("c", 43))
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, request)
		if !test.valid {
			if writer.Code != 400 || client.calls != 0 {
				t.Fatal("invalid name reached owner RPC")
			}
			continue
		}
		if writer.Code != 201 || client.calls != 1 {
			t.Fatalf("valid Unicode creation failed: %d", writer.Code)
		}
		var item generated.MyEmailMailbox
		if json.Unmarshal(writer.Body.Bytes(), &item) != nil || item.Name != strings.TrimSpace(test.name) || !item.Enabled || item.Available || !item.Ownership.OwnerEligible {
			t.Fatal("personal creation receipt changed enabled/name semantics")
		}
	}
}

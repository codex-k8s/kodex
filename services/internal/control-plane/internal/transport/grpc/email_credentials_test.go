package grpc

import (
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestEmailCredentialCasterExcludesPrivateMaterialization(t *testing.T) {
	for _, kind := range []string{"CA_CERTIFICATE", "USERNAME", "AUTH_SECRET"} {
		item := castEmailMailboxCredential(entity.EmailMailboxCredential{Name: "email-" + strings.Repeat("a", 32), Generation: 3, Kind: kind,
			ConnectionRef: "intconn_example", ConnectionVersion: 3, ContentSHA256: strings.Repeat("b", 64), SecretRef: "private-secret-ref", SecretUID: "private-uid", SecretResourceVersion: "private-resource-version"})
		if item.GetKind() == controlplanev1.EmailMailboxCredentialKind_EMAIL_MAILBOX_CREDENTIAL_KIND_UNSPECIFIED || item.ProtoReflect().Descriptor().Fields().Len() != 5 {
			t.Fatal("credential response shape mismatch")
		}
		raw, err := protojson.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "private") || strings.Contains(string(raw), strings.Repeat("b", 64)) {
			t.Fatal("private materialization exposed")
		}
	}
}

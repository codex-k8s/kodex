package vault

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

func TestVerifyStaticRoleResponseBindsPrincipalAndRotation(t *testing.T) {
	t.Parallel()

	expected := repository.VaultStaticRoleExpectation{
		Role:         "internal-rpc-authority-publisher-g1",
		Principal:    "ira_publisher_g1",
		DatabaseName: "internal-rpc-authority",
	}
	response := func(body string) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		}
	}
	valid := `{
		"request_id":"opaque",
		"data":{
			"credential_type":"password",
			"db_name":"internal-rpc-authority",
			"username":"ira_publisher_g1",
			"rotation_period":3600,
			"skip_import_rotation":false
		}
	}`
	if err := verifyStaticRoleResponse(response(valid), expected); err != nil {
		t.Fatalf("valid static role rejected: %v", err)
	}
	wrongPrincipal := strings.ReplaceAll(valid, "ira_publisher_g1", "ira_publisher_g2")
	if err := verifyStaticRoleResponse(response(wrongPrincipal), expected); err == nil {
		t.Fatal("wrong static role principal accepted")
	}
	wrongDatabase := strings.ReplaceAll(valid, "internal-rpc-authority", "other")
	if err := verifyStaticRoleResponse(response(wrongDatabase), expected); err == nil {
		t.Fatal("wrong static role database accepted")
	}
	ambiguousRotation := strings.Replace(
		valid,
		`"rotation_period":3600`,
		`"rotation_period":3600,"rotation_schedule":"0 0 * * SAT"`,
		1,
	)
	if err := verifyStaticRoleResponse(response(ambiguousRotation), expected); err == nil {
		t.Fatal("ambiguous static role rotation accepted")
	}
}

package vault

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
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

func TestValidateStaticRoleExpectationSeparatesVaultAndPostgreSQLNames(t *testing.T) {
	t.Parallel()

	valid := repository.VaultStaticRoleExpectation{
		Role:         "internal-rpc-authority-publisher-g3",
		Principal:    "ira_publisher_g3",
		DatabaseName: "mattercodex-postgresql",
	}
	if err := validateStaticRoleExpectation(valid); err != nil {
		t.Fatalf("valid PostgreSQL principal rejected: %v", err)
	}

	tests := []struct {
		name        string
		expectation repository.VaultStaticRoleExpectation
	}{
		{
			name: "underscore in Vault role",
			expectation: repository.VaultStaticRoleExpectation{
				Role:         "internal_rpc_authority_publisher_g3",
				Principal:    valid.Principal,
				DatabaseName: valid.DatabaseName,
			},
		},
		{
			name: "hyphen in PostgreSQL principal",
			expectation: repository.VaultStaticRoleExpectation{
				Role:         valid.Role,
				Principal:    "ira-publisher-g3",
				DatabaseName: valid.DatabaseName,
			},
		},
		{
			name: "path in database name",
			expectation: repository.VaultStaticRoleExpectation{
				Role:         valid.Role,
				Principal:    valid.Principal,
				DatabaseName: "mattercodex/postgresql",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := validateStaticRoleExpectation(test.expectation); err == nil {
				t.Fatal("invalid static role expectation accepted")
			}
		})
	}
}

func TestReadTokenFileAcceptsProjectedGroupReadableMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("bounded-projected-token"), 0o440); err != nil {
		t.Fatalf("write projected token: %v", err)
	}
	if _, err := readTokenFile(path); err != nil {
		t.Fatalf("readTokenFile() error = %v", err)
	}
}

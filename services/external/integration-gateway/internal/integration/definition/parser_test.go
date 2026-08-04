package definition

import (
	"strings"
	"testing"
)

const validDefinition = `apiVersion: mattercodex.io/v1
kind: IntegrationDefinition
metadata:
  name: payments
  version: 1
spec:
  tools:
    - name: list-payments
      version: 1
      description: List payments
      capability: payments
      risk: READ
      permission: integrations.payments.read
      approval: NEVER
      idempotency: NONE
      inputSchema:
        type: object
        additionalProperties: false
        properties: {}
      outputSchema:
        type: object
        additionalProperties: false
        properties: {}
      redactionPointers: []
      http:
        method: GET
        path: /v1/payments
        timeout: 2s
        idempotencyHeader: ""
        credentialHeaders: {}
`

func TestParseRejectsDuplicateAndUnknownFields(t *testing.T) {
	t.Parallel()
	duplicate := strings.Replace(validDefinition, "  name: payments\n", "  name: payments\n  name: shadow\n", 1)
	if _, err := Parse([]byte(duplicate)); err == nil {
		t.Fatal("duplicate YAML key was accepted")
	}
	unknown := strings.Replace(validDefinition, "  version: 1\n", "  version: 1\n  owner: caller\n", 1)
	if _, err := Parse([]byte(unknown)); err == nil {
		t.Fatal("unknown YAML field was accepted")
	}
}

func TestParseUsesCanonicalDigestAndClosedRisk(t *testing.T) {
	t.Parallel()
	first, err := Parse([]byte(validDefinition))
	if err != nil {
		t.Fatal(err)
	}
	secondSource := strings.Replace(validDefinition, "description: List payments", "description:  List payments", 1)
	second, err := Parse([]byte(secondSource))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatal("semantically equal package has a different canonical digest")
	}
	unsafe := strings.Replace(validDefinition, "risk: READ", "risk: WRITE", 1)
	if _, err := Parse([]byte(unsafe)); err == nil {
		t.Fatal("dangerous tool without mandatory approval/idempotency was accepted")
	}
}

func TestParseRejectsTrailingDocumentData(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{"\n---\n{}\n", "\ninvalid: [\n"} {
		if _, err := Parse([]byte(validDefinition + suffix)); err == nil {
			t.Fatalf("trailing YAML was accepted: %q", suffix)
		}
	}
}

func TestParseRejectsExposedNameCollisionsAndReservedNamespace(t *testing.T) {
	t.Parallel()
	secondVersion := strings.Replace(validDefinition, "    - name: list-payments", `    - name: list-payments
      version: 1
      description: List payments duplicate
      capability: payments
      risk: READ
      permission: integrations.payments.read
      approval: NEVER
      idempotency: NONE
      inputSchema:
        type: object
        additionalProperties: false
        properties: {}
      outputSchema:
        type: object
        additionalProperties: false
        properties: {}
      redactionPointers: []
      http:
        method: GET
        path: /v1/payments-duplicate
        timeout: 2s
        idempotencyHeader: ""
        credentialHeaders: {}
    - name: list-payments`, 1)
	secondVersion = strings.Replace(secondVersion, "    - name: list-payments\n      version: 1", "    - name: list-payments\n      version: 2", 1)
	if _, err := Parse([]byte(secondVersion)); err == nil {
		t.Fatal("same exposed name with another tool version was accepted")
	}
	reserved := strings.Replace(validDefinition, "name: list-payments", "name: mattercodex-session", 1)
	if _, err := Parse([]byte(reserved)); err == nil {
		t.Fatal("reserved mattercodex-* tool name was accepted")
	}
}

func TestParseAcceptsOnlySafeDirectDeliveryDescriptors(t *testing.T) {
	t.Parallel()
	direct := strings.Replace(validDefinition, "      http:\n", `      direct:
        reference: payments.readonly
        cliNames:
          - list-payments
        environmentNames:
          - PAYMENTS_REFERENCE
      http:
`, 1)
	definition, err := Parse([]byte(direct))
	if err != nil {
		t.Fatal(err)
	}
	if definition.Tools[0].DirectDelivery == nil || definition.Tools[0].DirectDelivery.Reference != "payments.readonly" {
		t.Fatal("safe direct delivery descriptor is missing")
	}

	dangerous := strings.NewReplacer(
		"risk: READ", "risk: WRITE",
		"approval: NEVER", "approval: ALWAYS",
		"idempotency: NONE", "idempotency: PROVIDER_HEADER",
		`idempotencyHeader: ""`, "idempotencyHeader: Idempotency-Key",
	).Replace(direct)
	if _, err := Parse([]byte(dangerous)); err == nil {
		t.Fatal("dangerous direct delivery descriptor was accepted")
	}

	credentialBearing := strings.Replace(direct, "credentialHeaders: {}", "credentialHeaders:\n          X-Api-Key: provider-key", 1)
	if _, err := Parse([]byte(credentialBearing)); err == nil {
		t.Fatal("credential-bearing direct delivery descriptor was accepted")
	}
}

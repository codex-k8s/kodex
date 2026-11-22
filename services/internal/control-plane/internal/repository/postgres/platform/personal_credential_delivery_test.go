package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestPersonalCredentialDeliveryUsesExactCurrentPackage tests explicit fresh package metadata,
// including a published managed revision; missing source pins never infer a token from its key.
// TestPersonalCredentialDeliveryUsesExactCurrentPackage проверяет explicit fresh package metadata,
// включая published managed revision; missing source pins не выводят token из имени его ключа.
func TestPersonalCredentialDeliveryUsesExactCurrentPackage(t *testing.T) {
	t.Parallel()
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	repository := &Repository{integrationDefinitions: definitions}
	definition := definitions["github"]
	delivery, err := repository.credentialDeliveryForExactPackageSnapshot(definition.Metadata.Key, definition.Metadata.Version, definition.Digest, "", "")
	if err != nil || delivery.Kind != integrationpackage.CredentialDeliveryToken || delivery.BrokerPurpose != "" {
		t.Fatal("fresh exact package lost its explicit TOKEN kind")
	}
	delivery, err = repository.credentialDeliveryForExactPackageSnapshot(definition.Metadata.Key, "99.0.0", strings.Repeat("a", 64), "", "")
	if err != nil || delivery.Kind != "" {
		t.Fatal("missing current package pins inferred a token credential")
	}
	if _, err := repository.credentialDeliveryForExactPackageSnapshot(definition.Metadata.Key, definition.Metadata.Version, definition.Digest, "JSON", "malformed"); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("current shipped pins hid corrupt explicitly bound package content")
	}
	definition.Metadata.Origin = integrationpackage.OriginUI
	raw, err := json.Marshal(definition)
	if err != nil {
		t.Fatal("encode synthetic managed package")
	}
	managed, err := integrationpackage.Parse(raw)
	if err != nil {
		t.Fatal("parse synthetic managed package")
	}
	delivery, err = repository.credentialDeliveryForExactPackageSnapshot(managed.Metadata.Key, managed.Metadata.Version, managed.Digest, "JSON", string(raw))
	if err != nil || delivery.Kind != integrationpackage.CredentialDeliveryToken {
		t.Fatal("same-snapshot published package did not supply explicit credential kind")
	}
	for _, test := range []struct{ key, version, digest, format, content string }{
		{managed.Metadata.Key, managed.Metadata.Version, managed.Digest, "XML", string(raw)},
		{managed.Metadata.Key, managed.Metadata.Version, managed.Digest, "JSON", ""},
		{managed.Metadata.Key, managed.Metadata.Version, strings.Repeat("b", 64), "JSON", string(raw)},
		{managed.Metadata.Key, "99.0.0", managed.Digest, "JSON", string(raw)},
		{"foreign_package", managed.Metadata.Version, managed.Digest, "JSON", string(raw)},
	} {
		if _, err := repository.credentialDeliveryForExactPackageSnapshot(test.key, test.version, test.digest, test.format, test.content); !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("malformed or mismatched source supplied credential representation")
		}
	}
}

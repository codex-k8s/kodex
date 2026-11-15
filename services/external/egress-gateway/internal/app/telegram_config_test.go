package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// TestTelegramRevisionIsIndependentFromReleasedBase loads two real policy files with distinct immutable revisions and requires the Telegram-specific pin without weakening base validation.
// TestTelegramRevisionIsIndependentFromReleasedBase загружает два real policy files с разными immutable revisions и требует Telegram-specific pin без ослабления base validation.
func TestTelegramRevisionIsIndependentFromReleasedBase(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "..", "deploy", "k8s", "base", "egress-gateway"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	old := []byte(`"2026-09-05.1"`)
	if bytes.Count(raw, old) != 1 {
		t.Fatal("base revision fixture changed")
	}
	basePath := filepath.Join(t.TempDir(), "base-policy.json")
	if os.WriteFile(basePath, bytes.Replace(raw, old, []byte(`"release-component-test"`), 1), 0o600) != nil {
		t.Fatal("base policy fixture write failed")
	}
	config := nativeConfigurationFixture()
	config.PolicyFile = basePath
	config.ExpectedRevision = "release-component-test"
	config.ExpectedDigest, err = policy.DigestFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	config.TelegramPolicyFile = filepath.Join(root, "telegram-policy.json")
	config.TelegramExpectedRevision = "2026-09-05.1"
	config.TelegramExpectedDigest, err = policy.DigestFile(config.TelegramPolicyFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.validate(); err != nil {
		t.Fatal("independent revision configuration rejected", err)
	}
	base, err := policy.LoadFile(config.PolicyFile, config.ExpectedRevision, config.ExpectedDigest)
	if err != nil {
		t.Fatal("released base policy rejected", err)
	}
	telegram, err := loadTelegramProfile(config)
	if err != nil || telegram == nil || telegram.Revision() == base.Revision() || !telegram.Allows("api.telegram.org", 443) || telegram.Allows("api.github.com", 443) {
		t.Fatal("independent Telegram revision rejected or scope expanded", err)
	}
	for _, revision := range []string{"", "release-component-test", "foreign-telegram-revision"} {
		broken := config
		broken.TelegramExpectedRevision = revision
		if _, err := loadTelegramProfile(broken); err == nil {
			t.Fatal("wrong Telegram revision accepted")
		}
		if _, err := policy.LoadFile(broken.PolicyFile, broken.ExpectedRevision, broken.ExpectedDigest); err != nil {
			t.Fatal("Telegram refusal changed base policy", err)
		}
	}
	wrongBase := config
	wrongBase.ExpectedRevision = "foreign-base-revision"
	if _, err := policy.LoadFile(wrongBase.PolicyFile, wrongBase.ExpectedRevision, wrongBase.ExpectedDigest); err == nil {
		t.Fatal("base revision guard weakened")
	}
	if _, err := loadTelegramProfile(wrongBase); err != nil {
		t.Fatal("base expectation was reused as Telegram authority", err)
	}
}

// TestTelegramInputKeepsBasePolicyStable binds the optional listener to its own immutable file,
// refuses an ordinary base policy as its source and keeps unconfigured startup explicit.
// TestTelegramInputKeepsBasePolicyStable связывает optional listener с отдельным immutable file,
// отвергает обычную base policy как его источник и явно сохраняет unconfigured startup.
func TestTelegramInputKeepsBasePolicyStable(t *testing.T) {
	if active, err := loadTelegramProfile(Config{}); err != nil || active != nil {
		t.Fatal("unconfigured Telegram listener is not explicit", err)
	}
	root := filepath.Join("..", "..", "..", "..", "..", "deploy", "k8s", "base", "egress-gateway")
	basePath := filepath.Join(root, "policy.json")
	baseDigest, err := policy.DigestFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	if baseDigest != "8529a00f3e8923e59d1776ee64d1965ee1e8f891daa17b94927c816248d6f218" {
		t.Fatal("Telegram changed the existing base policy")
	}
	config := Config{TelegramConnectAddress: ":8086", TelegramPolicyFile: basePath, ExpectedRevision: "release-distinct-base", TelegramExpectedRevision: "2026-09-05.1", TelegramExpectedDigest: baseDigest}
	if _, err := loadTelegramProfile(config); err == nil {
		t.Fatal("base policy impersonated Telegram profile")
	}
	config.TelegramPolicyFile = filepath.Join(root, "telegram-policy.json")
	config.TelegramExpectedDigest, err = policy.DigestFile(config.TelegramPolicyFile)
	if err != nil {
		t.Fatal(err)
	}
	active, err := loadTelegramProfile(config)
	if err != nil || active == nil || active.Digest() == baseDigest || !active.Allows("api.telegram.org", 443) || active.Allows("api.github.com", 443) {
		t.Fatal("Telegram policy lost its independent scope", err)
	}
}

// TestTelegramConfigurationRequiresCompleteOptionalInput rejects partial credentials-free listener
// configuration and port/mode confusion while accepting the existing deployment without Telegram.
// TestTelegramConfigurationRequiresCompleteOptionalInput отвергает неполный несекретный input listener
// и путаницу port/mode, сохраняя существующую установку без Telegram.
func TestTelegramConfigurationRequiresCompleteOptionalInput(t *testing.T) {
	config := nativeConfigurationFixture()
	config.TelegramConnectAddress = ""
	config.TelegramPolicyFile = ""
	config.TelegramExpectedDigest = ""
	config.TelegramExpectedRevision = ""
	if err := config.validate(); err != nil {
		t.Fatal("existing configuration requires Telegram", err)
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.TelegramPolicyFile = "" }, func(c *Config) { c.TelegramExpectedDigest = "" }, func(c *Config) { c.TelegramExpectedRevision = "" }, func(c *Config) { c.TelegramExpectedRevision = " invalid " }, func(c *Config) { c.TelegramExpectedRevision = strings.Repeat("a", 65) }, func(c *Config) { c.TelegramConnectAddress = "" },
		func(c *Config) { c.TelegramConnectAddress = ":8085" }, func(c *Config) { c.TelegramPolicyFile = "relative" }, func(c *Config) { c.Mode = "clamav" },
	} {
		broken := nativeConfigurationFixture()
		mutate(&broken)
		if broken.validate() == nil {
			t.Fatal("partial Telegram input was accepted")
		}
	}
}

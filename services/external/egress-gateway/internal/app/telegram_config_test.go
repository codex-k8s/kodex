package app

import (
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

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
	config := Config{TelegramConnectAddress: ":8086", TelegramPolicyFile: basePath, ExpectedRevision: "2026-09-05.1", TelegramExpectedDigest: baseDigest}
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
	if err := config.validate(); err != nil {
		t.Fatal("existing configuration requires Telegram", err)
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.TelegramPolicyFile = "" }, func(c *Config) { c.TelegramExpectedDigest = "" }, func(c *Config) { c.TelegramConnectAddress = "" },
		func(c *Config) { c.TelegramConnectAddress = ":8085" }, func(c *Config) { c.TelegramPolicyFile = "relative" }, func(c *Config) { c.Mode = "clamav" },
	} {
		broken := nativeConfigurationFixture()
		mutate(&broken)
		if broken.validate() == nil {
			t.Fatal("partial Telegram input was accepted")
		}
	}
}

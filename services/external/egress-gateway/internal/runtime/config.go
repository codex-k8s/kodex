// Package runtime собирает production composition root egress gateway.
package runtime

import (
	"errors"
	"os"
	"strings"
)

// Config задаёт только deployment-owned runtime inputs.
type Config struct {
	PolicyFile       string
	ExpectedRevision string
	ExpectedDigest   string
	ConnectAddress   string
	TechnicalAddress string
	ResolverConfig   string
}

// ConfigFromEnv строго загружает обязательные runtime inputs.
func ConfigFromEnv() (Config, error) {
	config := Config{
		PolicyFile:       strings.TrimSpace(os.Getenv("EGRESS_GATEWAY_POLICY_FILE")),
		ExpectedRevision: strings.TrimSpace(os.Getenv("EGRESS_GATEWAY_EXPECTED_POLICY_REVISION")),
		ExpectedDigest:   strings.TrimSpace(os.Getenv("EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST")),
		ConnectAddress:   strings.TrimSpace(os.Getenv("EGRESS_GATEWAY_CONNECT_LISTEN")),
		TechnicalAddress: strings.TrimSpace(os.Getenv("EGRESS_GATEWAY_TECHNICAL_LISTEN")),
		ResolverConfig:   strings.TrimSpace(os.Getenv("EGRESS_GATEWAY_RESOLV_CONF")),
	}
	if config.PolicyFile == "" || config.ExpectedRevision == "" || config.ExpectedDigest == "" ||
		config.ConnectAddress == "" || config.TechnicalAddress == "" || config.ResolverConfig == "" {
		return Config{}, errors.New("egress gateway runtime configuration is incomplete")
	}
	return config, nil
}

package service

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestValidCredentialAuthentication(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		value     *model.CredentialAuthentication
		actorKind string
		want      bool
	}{
		{name: "not supplied", actorKind: "SERVICE", want: true},
		{name: "human credential", actorKind: "HUMAN", value: &model.CredentialAuthentication{AuthenticatedAt: now.Add(-time.Minute).Unix(), ACR: "urn:kodex:reauth", AMR: []string{"pwd", "otp"}}, want: true},
		{name: "service cannot carry browser authentication", actorKind: "SERVICE", value: &model.CredentialAuthentication{AuthenticatedAt: now.Unix()}, want: false},
		{name: "future authentication", actorKind: "HUMAN", value: &model.CredentialAuthentication{AuthenticatedAt: now.Add(10 * time.Second).Unix()}, want: false},
		{name: "duplicate method", actorKind: "HUMAN", value: &model.CredentialAuthentication{AuthenticatedAt: now.Unix(), AMR: []string{"pwd", "pwd"}}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validCredentialAuthentication(test.value, test.actorKind, now, 5*time.Second); got != test.want {
				t.Fatalf("validCredentialAuthentication() = %v, want %v", got, test.want)
			}
		})
	}
}

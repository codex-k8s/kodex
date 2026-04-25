package runner

import "testing"

func TestDesiredCodexAuthModeUsesDeviceAuthByDefault(t *testing.T) {
	t.Parallel()

	svc := NewService(Config{
		PromptConfig: PromptConfig{
			AgentModel: "gpt-5.4",
		},
		OpenAIConfig: OpenAIConfig{
			OpenAIAPIKey: "sk-test",
		},
	}, nil, nil)

	if mode := svc.desiredCodexAuthMode(); mode != codexAuthModeChatGPT {
		t.Fatalf("desiredCodexAuthMode() = %q, want %q", mode, codexAuthModeChatGPT)
	}
}

func TestIsCodexAuthenticationError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		parts []string
		want  bool
	}{
		{
			name: "detects 401 unauthorized",
			parts: []string{
				"codex exec failed",
				"401 Unauthorized: Missing bearer or basic authentication in header",
			},
			want: true,
		},
		{
			name: "detects 403 forbidden",
			parts: []string{
				"provider returned 403 Forbidden for https://api.openai.com/v1/responses",
			},
			want: true,
		},
		{
			name: "detects code 4013 payload",
			parts: []string{
				`{"error":{"code":4013,"message":"auth required"}}`,
			},
			want: true,
		},
		{
			name: "ignores unrelated error",
			parts: []string{
				"failed to parse structured output",
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := isCodexAuthenticationError(tc.parts...); got != tc.want {
				t.Fatalf("isCodexAuthenticationError(%q) = %v, want %v", tc.parts, got, tc.want)
			}
		})
	}
}

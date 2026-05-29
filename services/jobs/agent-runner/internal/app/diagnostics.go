package app

import (
	"log/slog"
	"strings"
	"unicode/utf8"
)

const (
	ExitOK      = 0
	ExitFailure = 2
	ExitUsage   = 64

	CodeOK                   = "ok"
	CodeInvalidConfiguration = "agent_runner_configuration_invalid"
)

type Diagnostic struct {
	Code     string
	Summary  string
	ExitCode int
}

func OKDiagnostic() Diagnostic {
	return Diagnostic{Code: CodeOK, Summary: "agent-runner completed", ExitCode: ExitOK}
}

func NewDiagnostic(code string, summary string, exitCode int) Diagnostic {
	return Diagnostic{Code: strings.TrimSpace(code), Summary: safeSummary(summary), ExitCode: exitCode}
}

func (d Diagnostic) OK() bool {
	return d.Code == CodeOK && d.ExitCode == ExitOK
}

func (d Diagnostic) LogAttrs() []any {
	return []any{
		slog.String("error_code", d.Code),
		slog.String("safe_summary", d.Summary),
	}
}

func safeSummary(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || unsafeText(trimmed) {
		return "agent-runner failed with a safe diagnostic"
	}
	if len(trimmed) > 512 {
		return trimmed[:512]
	}
	return trimmed
}

func safeRef(value string, required bool) bool {
	return safeBoundedText(value, required, maxSafeRefBytes, "\r\n\t{}")
}

func safeLabel(value string, required bool) bool {
	return safeBoundedText(value, required, maxSafeLabelBytes, "\r\n\t {}")
}

func safeBoundedText(value string, required bool, maxBytes int, forbiddenChars string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return !required
	}
	if len(trimmed) > maxBytes || !utf8.ValidString(trimmed) || strings.ContainsAny(trimmed, forbiddenChars) {
		return false
	}
	return !unsafeText(trimmed)
}

func unsafeText(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	markers := []string{
		"raw_provider_payload",
		"provider_payload",
		"prompt_text",
		"prompt_body",
		"transcript",
		"tool_input",
		"tool_output",
		"workspace_path",
		"kubeconfig",
		"secret_value",
		"secret-value",
		"token=",
		"authorization",
		"stdout",
		"stderr",
		"large_log",
		"-----begin",
		"bearer ",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

package integration

import (
	"encoding/json"
	"slices"
	"unicode/utf8"
)

// sentryEventFrame retains only nullable diagnostic coordinates from the native API context; vars, source context, absolute paths, links and raw symbol data have no projection field.
// sentryEventFrame сохраняет только nullable diagnostic coordinates native API context; vars, source context, absolute paths, links и raw symbol data не имеют projection field.
type sentryEventFrame struct {
	Filename *string `json:"filename"`
	Function *string `json:"function"`
	Module   *string `json:"module"`
	Line     *int64  `json:"lineNo"`
	Column   *int64  `json:"colNo"`
	InApp    *bool   `json:"inApp"`
}

// sentryEventStacktrace preserves declared omission metadata and frame order without accepting an executable context or untyped provider document.
// sentryEventStacktrace сохраняет declared omission metadata и frame order без executable context либо untyped provider document.
type sentryEventStacktrace struct {
	Frames        []sentryEventFrame `json:"frames"`
	FramesOmitted []int64            `json:"framesOmitted"`
}

// sentryEventException models nullable exception labels and the processed stacktrace only; rawStacktrace and rawValue remain unprojected.
// sentryEventException моделирует nullable exception labels и только processed stacktrace; rawStacktrace и rawValue не выдаются.
type sentryEventException struct {
	Type       *string                `json:"type"`
	Value      *string                `json:"value"`
	Stacktrace *sentryEventStacktrace `json:"stacktrace"`
}

// sentryEventEntry delays interface-specific semantic decoding until its exact native type is selected; all other event interfaces are discarded.
// sentryEventEntry откладывает interface-specific semantic decode до выбора exact native type; остальные event interfaces отбрасываются.
type sentryEventEntry struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// sentryEventDiagnosticText shares one UTF-8 byte budget across all exception/frame strings and marks any local shortening; keys originate only from the closed projection builder.
// sentryEventDiagnosticText разделяет единый UTF-8 byte budget между exception/frame strings и отмечает сокращение; keys происходят только из закрытого projection builder.
func sentryEventDiagnosticText(output map[string]any, key string, value *string, maximum int, remaining *int, truncated *bool) {
	if value == nil {
		return
	}
	maximum = min(maximum, *remaining)
	text := *value
	if len(text) > maximum {
		text = text[:maximum]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		*truncated = true
	}
	*remaining -= len(text)
	output[key] = text
}

// sentryEventFrames keeps the most recent frames within the shared count/text budgets and restores their original call order; invalid numeric coordinates fail closed instead of becoming fabricated zeroes.
// sentryEventFrames сохраняет ближайшие к ошибке frames в общих count/text budgets и восстанавливает исходный call order; неверные numeric coordinates закрыто отклоняются вместо выдуманного нуля.
func sentryEventFrames(trace *sentryEventStacktrace, remainingFrames, remainingText *int, truncated *bool) ([]map[string]any, error) {
	frames := []map[string]any{}
	if trace == nil {
		return frames, nil
	}
	count := min(len(trace.Frames), *remainingFrames)
	if count != len(trace.Frames) || len(trace.FramesOmitted) != 0 {
		*truncated = true
	}
	*remainingFrames -= count
	for index := len(trace.Frames) - 1; index >= len(trace.Frames)-count; index-- {
		frame := trace.Frames[index]
		if frame.Line != nil && (*frame.Line < -2147483648 || *frame.Line > 2147483647) || frame.Column != nil && (*frame.Column < -2147483648 || *frame.Column > 2147483647) {
			return nil, &SafeError{Code: providerResponseInvalidCode}
		}
		item := map[string]any{}
		sentryEventDiagnosticText(item, "filename", frame.Filename, 256, remainingText, truncated)
		sentryEventDiagnosticText(item, "function", frame.Function, 256, remainingText, truncated)
		sentryEventDiagnosticText(item, "module", frame.Module, 128, remainingText, truncated)
		if frame.Line != nil {
			item["line"] = *frame.Line
		}
		if frame.Column != nil {
			item["column"] = *frame.Column
		}
		if frame.InApp != nil {
			item["in_app"] = *frame.InApp
		}
		frames = append(frames, item)
	}
	slices.Reverse(frames)
	return frames, nil
}

// sentryEventDiagnostics projects at most three most recent exceptions and 32 total frames with 4096 diagnostic UTF-8 bytes, explicitly reporting local/upstream truncation; unsupported interfaces never become arbitrary event passthrough.
// sentryEventDiagnostics выдаёт не более трёх ближайших exceptions и 32 frames суммарно с 4096 diagnostic UTF-8 bytes, явно отмечая local/upstream truncation; неподдержанные interfaces не превращаются в arbitrary event passthrough.
func sentryEventDiagnostics(entries []sentryEventEntry) (map[string]any, error) {
	exceptions := []map[string]any{}
	stacktrace := []map[string]any{}
	truncated := false
	remainingFrames, remainingText := 32, 4096
	seen := map[string]bool{}
	for _, kind := range []string{"exception", "stacktrace"} {
		for _, entry := range entries {
			if entry.Type != kind {
				continue
			}
			if seen[entry.Type] {
				return nil, &SafeError{Code: providerResponseInvalidCode}
			}
			seen[entry.Type] = true
			if entry.Type == "stacktrace" {
				var trace sentryEventStacktrace
				if decodeTypedProviderJSON(entry.Data, &trace) != nil {
					return nil, &SafeError{Code: providerResponseInvalidCode}
				}
				var err error
				stacktrace, err = sentryEventFrames(&trace, &remainingFrames, &remainingText, &truncated)
				if err != nil {
					return nil, err
				}
				continue
			}
			var data struct {
				Values     []sentryEventException `json:"values"`
				ExcOmitted []int64                `json:"excOmitted"`
			}
			if decodeTypedProviderJSON(entry.Data, &data) != nil || data.Values == nil {
				return nil, &SafeError{Code: providerResponseInvalidCode}
			}
			count := min(len(data.Values), 3)
			if count != len(data.Values) || len(data.ExcOmitted) != 0 {
				truncated = true
			}
			for index := len(data.Values) - 1; index >= len(data.Values)-count; index-- {
				value := data.Values[index]
				item := map[string]any{}
				sentryEventDiagnosticText(item, "type", value.Type, 128, &remainingText, &truncated)
				sentryEventDiagnosticText(item, "value", value.Value, 512, &remainingText, &truncated)
				frames, err := sentryEventFrames(value.Stacktrace, &remainingFrames, &remainingText, &truncated)
				if err != nil {
					return nil, err
				}
				item["frames"] = frames
				exceptions = append(exceptions, item)
			}
			slices.Reverse(exceptions)
		}
	}
	state := "NO_SUPPORTED_DIAGNOSTICS"
	if len(seen) > 0 {
		state = "AVAILABLE"
	}
	return map[string]any{"diagnostics_state": state, "exceptions": exceptions, "stacktrace": stacktrace, "truncated": truncated, "untrusted_content": true}, nil
}

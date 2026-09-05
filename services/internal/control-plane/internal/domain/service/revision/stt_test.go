package revision

import (
	"strings"
	"testing"
)

func TestSystemSTTImmutableParametersAndLimits(t *testing.T) {
	content := `{"name":"Dictation","stt":{"enabled":true,"providerAccountRef":"pacc_fixture","model":"gpt-transcribe","permissionKey":"platform.stt.use","parameters":{"languages":["ru","en"],"keywords":["Kodex"],"prompt":"Names","temperature":0.2,"chunkingStrategy":"auto","stream":false}}}`
	specification, err := ParseSystemSTT(content)
	if err != nil || len(specification.Parameters.Languages) != 2 || len(specification.Parameters.Keywords) != 1 || specification.Parameters.Prompt != "Names" ||
		specification.Parameters.Temperature != 0.2 || specification.Parameters.ChunkingStrategy != "auto" || specification.Parameters.Stream ||
		specification.MaximumAudioBytes != 10<<20 || specification.MaximumAudioDurationMilliseconds != 120000 || specification.ProviderTimeoutMilliseconds != 15000 {
		t.Fatalf("typed STT parameters/default limits: %#v %v", specification, err)
	}
	for _, invalid := range []string{
		strings.Replace(content, `"stream":false`, `"stream":true`, 1),
		strings.Replace(content, `"temperature":0.2`, `"temperature":2`, 1),
		strings.Replace(content, `"model":"gpt-transcribe"`, `"model":"whisper-1"`, 1),
		strings.Replace(content, `"enabled":true`, `"enabled":true,"maximumAudioBytes":26214401`, 1),
		strings.Replace(content, `"enabled":true`, `"enabled":true,"maximumAudioBytes":1`, 1),
		strings.Replace(content, `"enabled":true`, `"enabled":true,"maximumAudioDurationMilliseconds":1`, 1),
		strings.Replace(content, `"enabled":true`, `"enabled":true,"maximumAudioDurationMilliseconds":1800001`, 1),
		strings.Replace(content, `"enabled":true`, `"enabled":true,"providerTimeoutMilliseconds":1`, 1),
		strings.Replace(content, `"enabled":true`, `"enabled":true,"providerTimeoutMilliseconds":60000`, 1),
		strings.Replace(content, `"enabled":true`, `"enabled":true,"language":"ru"`, 1),
		strings.Replace(content, `"enabled":true`, `"enabled":true,"credential":"forbidden"`, 1),
	} {
		if _, err := ParseSystemSTT(invalid); err == nil {
			t.Fatal("unsupported STT parameters accepted")
		}
	}
}

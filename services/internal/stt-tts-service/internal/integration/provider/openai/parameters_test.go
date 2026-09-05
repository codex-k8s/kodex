package openai

import (
	"bytes"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

func TestCatalogModelMultipartParameters(t *testing.T) {
	for _, profile := range modelprofile.OpenAICatalog().Models {
		t.Run(profile.Model, func(t *testing.T) {
			parameters := modelprofile.Parameters{Prompt: "Technical discussion", Temperature: 0.2}
			language := "ru"
			if profile.Model == modelprofile.RecommendedModel {
				language = ""
				parameters.Languages = []string{"ru", "en"}
				parameters.Keywords = []string{"Kodex", "OpenAI"}
			}
			if !profile.Legacy {
				parameters.ChunkingStrategy = "auto"
			}
			calls := 0
			client, _ := NewWithHTTPClient(doerFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if err := request.ParseMultipartForm(1024); err != nil {
					t.Fatal(err)
				}
				defer request.MultipartForm.RemoveAll()
				fields := request.MultipartForm.Value
				if fields["model"][0] != profile.Model || fields["temperature"][0] != "0.2" || fields["prompt"][0] != parameters.Prompt || fields["response_format"][0] != "json" {
					t.Fatal("model parameters mismatch")
				}
				if profile.Model == modelprofile.RecommendedModel {
					if len(fields["language"]) != 0 || !reflect.DeepEqual(fields["languages[]"], parameters.Languages) || !reflect.DeepEqual(fields["keywords[]"], parameters.Keywords) {
						t.Fatal("multilingual context mismatch")
					}
				} else if fields["language"][0] != "ru" || len(fields["languages[]"])+len(fields["keywords[]"]) != 0 {
					t.Fatal("legacy context mismatch")
				}
				if len(fields["stream"]) != 0 || (profile.Legacy && len(fields["chunking_strategy"]) != 0) {
					t.Fatal("unsupported parameter sent")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"text":"ok"}`))}, nil
			}))
			request := value.ProviderRequest{Model: profile.Model, Language: language, Parameters: parameters, APIKey: []byte("test-only-key"), Audio: value.Audio{Reader: bytes.NewReader([]byte{1}), SizeBytes: 1, FileName: "audio.wav"}}
			if _, err := client.Transcribe(t.Context(), request); err != nil || calls != 1 {
				t.Fatalf("adapter failed: %v", err)
			}
			request.Parameters.Stream = true
			if _, err := client.Transcribe(t.Context(), request); err == nil || calls != 1 {
				t.Fatal("unsupported parameters reached provider")
			}
		})
	}
}

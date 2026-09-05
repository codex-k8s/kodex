package httptransport

import (
	"encoding/json"
	"net/http"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func validSTTSpecification(value generated.SystemSTTSpecification) bool {
	return opaqueHTTPReference.MatchString(value.ProviderAccountRef) && value.PermissionKey == "platform.stt.use" &&
		value.MaximumAudioBytes >= modelprofile.MinimumAudioBytes && value.MaximumAudioBytes <= modelprofile.MaximumAudioBytes &&
		value.MaximumAudioDurationMilliseconds >= modelprofile.MinimumAudioDuration.Milliseconds() && value.MaximumAudioDurationMilliseconds <= modelprofile.MaximumAudioDuration.Milliseconds() &&
		value.ProviderTimeoutMilliseconds >= modelprofile.MinimumProviderTimeout.Milliseconds() && value.ProviderTimeoutMilliseconds <= modelprofile.MaximumProviderTimeout.Milliseconds() &&
		modelprofile.Validate(value.Model, value.Language, modelprofile.Parameters{Languages: value.Parameters.Languages, Keywords: value.Parameters.Keywords, Prompt: value.Parameters.Prompt,
			Temperature: value.Parameters.Temperature, ChunkingStrategy: string(value.Parameters.ChunkingStrategy), Stream: bool(value.Parameters.Stream)}) == nil
}

func (server *Server) CreateTypedSystemSTTConfigurationDraft(w http.ResponseWriter, r *http.Request, p generated.CreateTypedSystemSTTConfigurationDraftParams) {
	body, ok := decodeJSON[generated.SystemSTTConfigurationDraftInput](w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(body.Name) == "" || len(body.Name) > 160 || !validSTTSpecification(body.Specification) ||
		!httpIdempotencyKey.MatchString(p.IdempotencyKey) || body.ConfigurationRef != nil && !opaqueHTTPReference.MatchString(*body.ConfigurationRef) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	// CP получает тот же закрытый JSON document, что и редактор managed revision.
	content, err := json.Marshal(struct {
		Name string                           `json:"name"`
		STT  generated.SystemSTTSpecification `json:"stt"`
	}{Name: body.Name, STT: body.Specification})
	if err != nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	draft := generated.ManagedConfigurationDraftInput{ConfigurationRef: body.ConfigurationRef, Name: body.Name, ContentFormat: generated.ManagedConfigurationDraftInputContentFormat("JSON"), Content: string(content)}
	mutation, ok := requireManagedDraftMutation(w, p.IdempotencyKey, stringValue(p.IfMatch), draft)
	if !ok {
		return
	}
	if p.IfMatch != nil {
		mutation, ok = requireVersionedMutation(w, p.IdempotencyKey, *p.IfMatch)
		if !ok {
			return
		}
	}
	response, err := server.control.Command.CreateSystemSTTConfigurationDraft(r.Context(), &controlplanev1.CreateSystemSTTConfigurationDraftRequest{Mutation: mutation,
		ConfigurationRef: stringValue(body.ConfigurationRef), Name: body.Name, ContentFormat: "JSON", Content: string(content)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusCreated, response)
}

func systemSTTSpecificationView(value *controlplanev1.SystemSTTConfiguration) (generated.SystemSTTSpecification, bool) {
	p := value.GetParameters()
	if p == nil || value.GetMaximumAudioBytes() > modelprofile.MaximumAudioBytes || value.GetMaximumAudioDurationMilliseconds() > uint64(modelprofile.MaximumAudioDuration.Milliseconds()) || value.GetProviderTimeoutMilliseconds() > uint64(modelprofile.MaximumProviderTimeout.Milliseconds()) {
		return generated.SystemSTTSpecification{}, false
	}
	result := generated.SystemSTTSpecification{Enabled: value.GetEnabled(), ProviderAccountRef: value.GetProviderAccountRef(), Model: value.GetModel(), Language: value.GetLanguage(),
		PermissionKey: generated.SystemSTTSpecificationPermissionKey(value.GetPermissionKey()), MaximumAudioBytes: int64(value.GetMaximumAudioBytes()),
		MaximumAudioDurationMilliseconds: int64(value.GetMaximumAudioDurationMilliseconds()), ProviderTimeoutMilliseconds: int64(value.GetProviderTimeoutMilliseconds()),
		Parameters: generated.SystemSTTParameters{Languages: append([]string{}, p.GetLanguages()...), Keywords: append([]string{}, p.GetKeywords()...), Prompt: p.GetPrompt(), Temperature: p.GetTemperature(),
			ChunkingStrategy: generated.SystemSTTParametersChunkingStrategy(p.GetChunkingStrategy()), Stream: generated.SystemSTTParametersStream(p.GetStream())}}
	return result, validSTTSpecification(result)
}

package runtimecontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

const (
	ProviderProcessObservationSchema = "kodex.provider-process-observation.v1"
	// Сохраняем прежний пользовательский progress code, без нового UI lifecycle.
	ProviderProcessInitializedProgress = "MODEL_REQUEST_RUNNING"
)

var (
	providerProcessVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})(-(alpha|beta|rc)\.(0|[1-9][0-9]{0,5}))?$`)
	errProviderProcessObservation = errors.New("provider process observation is invalid")
)

// ProviderProcessObservation — диагностический handshake текущего процесса,
// не authority, admission, inventory или утверждение о live readiness.
type ProviderProcessObservation struct {
	Schema                 string `json:"schema"`
	Version                string `json:"version"`
	OrganizationRef        string `json:"organization_ref"`
	ProjectRef             string `json:"project_ref"`
	RunRef                 string `json:"run_ref"`
	NodeRef                string `json:"node_ref"`
	SessionRef             string `json:"session_ref"`
	TurnRef                string `json:"turn_ref"`
	Attempt                int32  `json:"attempt"`
	RuntimeRevisionRef     string `json:"runtime_revision_ref"`
	RuntimeRevisionVersion int64  `json:"runtime_revision_version"`
	RuntimeRevisionDigest  string `json:"runtime_revision_digest"`
	ImageReference         string `json:"image_reference"`
	ImageManifestDigest    string `json:"image_manifest_digest"`
	InputDigest            string `json:"input_digest"`
	ExecutionBindingDigest string `json:"execution_binding_digest"`
}

func ValidProviderProcessVersion(version string) bool {
	return len(version) <= 64 && providerProcessVersionPattern.MatchString(version)
}

func BindProviderProcessObservation(input RunnerInput, version string) ProviderProcessObservation {
	return ProviderProcessObservation{Schema: ProviderProcessObservationSchema, Version: version,
		OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, RunRef: input.RunRef, NodeRef: input.NodeRef,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt,
		RuntimeRevisionRef: input.RuntimeRevisionRef, RuntimeRevisionVersion: input.RuntimeRevisionVersion, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		ImageReference: input.ImageReference, ImageManifestDigest: input.ImageManifestDigest,
		InputDigest: input.InputDigest, ExecutionBindingDigest: input.ExecutionBindingDigest}
}

func (value ProviderProcessObservation) Validate() error {
	if value.Schema != ProviderProcessObservationSchema || !ValidProviderProcessVersion(value.Version) ||
		!opaqueReferencePattern.MatchString(value.OrganizationRef) ||
		(value.ProjectRef != "" && !opaqueReferencePattern.MatchString(value.ProjectRef)) ||
		!opaqueReferencePattern.MatchString(value.RunRef) || !opaqueReferencePattern.MatchString(value.NodeRef) ||
		!opaqueReferencePattern.MatchString(value.SessionRef) || !opaqueReferencePattern.MatchString(value.TurnRef) || value.Attempt < 1 ||
		(!opaqueReferencePattern.MatchString(value.RuntimeRevisionRef) && !systemRuntimeRevisionPattern.MatchString(value.RuntimeRevisionRef)) ||
		value.RuntimeRevisionVersion < 1 || !sha256Pattern.MatchString(value.RuntimeRevisionDigest) ||
		!validPinnedImage(value.ImageReference, value.ImageManifestDigest) ||
		!sha256Pattern.MatchString(value.InputDigest) || !sha256Pattern.MatchString(value.ExecutionBindingDigest) {
		return errProviderProcessObservation
	}
	return nil
}

func (value ProviderProcessObservation) Matches(input RunnerInput) bool {
	return value.Validate() == nil && value == BindProviderProcessObservation(input, value.Version)
}

// Producer передаёт canonical typed JSON; aliases, duplicate/unknown/missing
// поля не создают частично восстановленное наблюдение.
func (value *ProviderProcessObservation) UnmarshalJSON(raw []byte) error {
	if len(raw) > 4096 || boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 2) != nil {
		return errProviderProcessObservation
	}
	type wire ProviderProcessObservation
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decoded) != nil || decoder.Decode(new(any)) != io.EOF || ProviderProcessObservation(decoded).Validate() != nil {
		return errProviderProcessObservation
	}
	canonical, err := json.Marshal(decoded)
	var compact bytes.Buffer
	if err != nil || json.Compact(&compact, raw) != nil || !bytes.Equal(compact.Bytes(), canonical) {
		return errProviderProcessObservation
	}
	*value = ProviderProcessObservation(decoded)
	return nil
}

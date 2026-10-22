package app

import (
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// buildInputManifests reconstructs and checks every expected set digest before validating the
// full workspace catalog; materialization and readback share this order and fixed error classes.
// It performs no file access, byte retrieval or authorization and returns no partial catalog on failure.
// buildInputManifests восстанавливает и сверяет ожидаемые хеши всех наборов до проверки полного
// каталога workspace; материализация и сверка используют этот порядок и фиксированные классы ошибок.
// Доступ к файлам, получение байтов и авторизация не выполняются, частичный каталог при ошибке не выдаётся.
func buildInputManifests(input model.Input) (map[string]runtimecontract.CanonicalAttachmentManifest, runtimecontract.CanonicalWorkspaceAttachmentManifest, error) {
	sets := make(map[string]runtimecontract.CanonicalAttachmentManifest, len(input.AttachmentSets))
	for _, set := range input.AttachmentSets {
		manifest, err := runtimecontract.BuildAttachmentSetManifest(set, input.InputArtifacts)
		if err != nil || manifest.Digest != set.ManifestDigest {
			return nil, runtimecontract.CanonicalWorkspaceAttachmentManifest{}, errors.New("runtime attachment manifest digest is invalid")
		}
		sets[set.Ref] = manifest
	}
	workspace, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts)
	if err != nil {
		return nil, runtimecontract.CanonicalWorkspaceAttachmentManifest{}, errors.New("build runtime workspace manifest")
	}
	return sets, workspace, nil
}

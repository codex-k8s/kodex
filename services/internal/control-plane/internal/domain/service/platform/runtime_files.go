package platform

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// executionFileDigestPattern accepts only a qualified lowercase SHA-256 digest shape for exact
// execution file references, not file authority or content validity.
// executionFileDigestPattern принимает только форму квалифицированного lowercase-дайджеста SHA-256
// точной ссылки файла выполнения без подтверждения прав на файл либо содержимого.
var executionFileDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// filePrincipal resolves principal, requires runtime-controller and the exact file operation and
// validates catalog/purpose/lease-fence shape; owner state still validates execution ownership.
// filePrincipal разрешает principal, требует runtime-controller и точную файловую операцию и
// проверяет форму каталога, purpose и lease fence; владение выполнением подтверждает состояние
// владельца.
func (service *Service) filePrincipal(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, operation string) (value.Principal, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return principal, err
	}
	if principal.CallerWorkload != "runtime-controller" || principal.Permission != "platform.runtime.files."+operation {
		return principal, errs.ErrForbidden
	}
	purposes := []string{runtimecontract.FilePurposeProject, runtimecontract.FilePurposeWorkspaceInput, runtimecontract.FilePurposeRunResult, runtimecontract.FilePurposeSkill}
	if !slices.Contains(purposes, execution.Purpose) || execution.Generation < 1 || len(execution.Fence) < 8 || len(execution.Fence) > 256 ||
		strings.TrimSpace(execution.LeaseRef) == "" || len(execution.LeaseRef) > 96 ||
		!strings.HasPrefix(execution.CatalogRef, "vfc_") ||
		(runtimecontract.RuntimeFileCatalog{Ref: execution.CatalogRef, Digest: execution.CatalogDigest, Purposes: []string{execution.Purpose}}).Validate() != nil {
		return principal, errs.ErrInvalid
	}
	return principal, nil
}

// validExecutionFileRef checks bounded entry/artifact prefixes, positive revision and qualified
// digest shape without authorizing the referenced resource.
// validExecutionFileRef проверяет ограниченные префиксы entry и artifact, положительную ревизию и
// квалифицированную форму дайджеста без авторизации ресурса по ссылке.
func validExecutionFileRef(file query.ExecutionFileRef) bool {
	return strings.HasPrefix(file.EntryRef, "vfe_") && len(file.EntryRef) <= 96 &&
		strings.HasPrefix(file.ArtifactRef, "art_") && len(file.ArtifactRef) <= 96 && file.Revision > 0 && executionFileDigestPattern.MatchString(file.Digest)
}

// SearchExecutionFiles requires exact execution-file search authority and validates a 200-rune
// UTF-8 query plus page-size bounds before owner search.
// SearchExecutionFiles требует точные полномочия поиска файлов выполнения и проверяет UTF-8 запрос
// до 200 символов и границы размера страницы до поиска владельца.
func (service *Service) SearchExecutionFiles(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, search string, page query.Page) (entity.ExecutionFilePage, error) {
	principal, err := service.filePrincipal(ctx, principal, execution, "search")
	if err != nil {
		return entity.ExecutionFilePage{}, err
	}
	if !utf8.ValidString(search) || len([]rune(search)) > 200 || strings.ContainsRune(search, '\x00') || page.Size < 0 || page.Size > 100 {
		return entity.ExecutionFilePage{}, errs.ErrInvalid
	}
	return service.repository.SearchExecutionFiles(ctx, principal, execution, search, page)
}

// GetExecutionFileManifest requires exact manifest authority and a zero-to-one-hundred page size
// before reading the owner-bound execution file catalog.
// GetExecutionFileManifest требует точные полномочия manifest и размер страницы от нуля до ста до
// чтения связанного с владельцем каталога файлов выполнения.
func (service *Service) GetExecutionFileManifest(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, page query.Page) (entity.ExecutionFilePage, error) {
	principal, err := service.filePrincipal(ctx, principal, execution, "manifest")
	if err != nil {
		return entity.ExecutionFilePage{}, err
	}
	if page.Size < 0 || page.Size > 100 {
		return entity.ExecutionFilePage{}, errs.ErrInvalid
	}
	return service.repository.GetExecutionFileManifest(ctx, principal, execution, page)
}

// GetExecutionFileMetadata requires exact metadata authority and an explicit valid
// file/revision/digest pin before querying the owner repository.
// GetExecutionFileMetadata требует точные полномочия metadata и явный корректный pin файла,
// ревизии и дайджеста до запроса репозиторию владельца.
func (service *Service) GetExecutionFileMetadata(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, file query.ExecutionFileRef) (entity.ExecutionFileMetadata, error) {
	principal, err := service.filePrincipal(ctx, principal, execution, "metadata")
	if err != nil {
		return entity.ExecutionFileMetadata{}, err
	}
	if !validExecutionFileRef(file) {
		return entity.ExecutionFileMetadata{}, errs.ErrInvalid
	}
	return service.repository.GetExecutionFileMetadata(ctx, principal, execution, file)
}

// PreviewExecutionFile requires exact preview authority and file pins, bounds output to sixteen
// KiB and defaults an omitted zero maximum to four KiB.
// PreviewExecutionFile требует точные полномочия preview и pins файла, ограничивает вывод
// шестнадцатью KiB и заменяет пропущенный нулевой максимум четырьмя KiB.
func (service *Service) PreviewExecutionFile(ctx context.Context, principal value.Principal, execution query.ExecutionFileContext, file query.ExecutionFileRef, maximum int32) (entity.ExecutionFilePreview, error) {
	principal, err := service.filePrincipal(ctx, principal, execution, "preview")
	if err != nil {
		return entity.ExecutionFilePreview{}, err
	}
	if !validExecutionFileRef(file) || maximum < 0 || maximum > 16384 {
		return entity.ExecutionFilePreview{}, errs.ErrInvalid
	}
	if maximum == 0 {
		maximum = 4096
	}
	return service.repository.PreviewExecutionFile(ctx, principal, execution, file, maximum)
}

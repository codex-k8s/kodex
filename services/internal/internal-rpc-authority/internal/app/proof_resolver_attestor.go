package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/snapshot"
)

// proofResolverAudience pins the proof resolver's expected authority-issuer audience to the control API gateway. It is a verification input, not a caller-selected route or permission grant.
// proofResolverAudience закрепляет ожидаемую аудиторию издателя полномочий proof resolver за control API gateway. Это вход проверки, а не выбранный вызывающим маршрут или выданное полномочие.
const proofResolverAudience = "urn:kodex:internal-rpc-authority-issuer:control-api-gateway"

// proofResolverAttestor composes primary snapshot attestation with a separately configured resolver attestor and exact proof signer/trust references. Its clock is injected for signer verification; it stores file locators and issuer identity, not a preapproved receipt or a permission derived from payload.
// proofResolverAttestor объединяет основную аттестацию снимка с отдельно настроенным resolver attestor и точными ссылками подписанта и доверия proof. Часы переданы для проверки подписанта; хранятся адреса файлов и идентичность издателя, а не заранее одобренная квитанция или полномочия из payload.
type proofResolverAttestor struct {
	primary              repository.SnapshotAttestor
	resolver             repository.SnapshotAttestor
	privateJWKFile       string
	signerGenerationFile string
	proofTrustFile       string
	issuer               string
	now                  func() time.Time
}

// Attest obtains the primary receipt, reads the current signer generation, verifies the proof signer against exact issuer/audience/source revision/digest/time, then requires resolver attestation before returning that primary receipt. Later failures return no receipt but do not undo the already completed primary attestation; resolver/key refusals are sanitized, and primary errors pass through.
// Attest получает основную квитанцию, читает текущее поколение подписанта, проверяет подписанта proof по точным издателю, аудитории, исходной ревизии, дайджесту и времени, затем требует аттестацию resolver перед возвратом основной квитанции. Поздняя ошибка не возвращает квитанцию, но не отменяет уже завершённую основную аттестацию; отказы ключа и resolver безопасны, ошибки основного attestor передаются напрямую.
func (attestor *proofResolverAttestor) Attest(
	ctx context.Context,
	state repository.SnapshotState,
) (repository.SnapshotAttestationReceipt, error) {
	primaryReceipt, err := attestor.primary.Attest(ctx, state)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, err
	}
	signerGeneration, err := readProofSignerGeneration(attestor.signerGenerationFile)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, err
	}
	if err := snapshot.VerifyProofSigner(
		attestor.privateJWKFile,
		attestor.proofTrustFile,
		attestor.issuer,
		proofResolverAudience,
		state.SourceRevision,
		state.SourceDigestSHA256,
		signerGeneration,
		attestor.now().UTC(),
	); err != nil {
		return repository.SnapshotAttestationReceipt{}, errors.New("served authority proof resolver key rejected")
	}
	if _, err := attestor.resolver.Attest(ctx, state); err != nil {
		return repository.SnapshotAttestationReceipt{}, errors.New("obtain authority proof resolver readback receipt")
	}
	return primaryReceipt, nil
}

// readProofSignerGeneration reads at most thirty-two bytes through securefile, trims outer whitespace and parses a positive decimal uint64 bounded by the JavaScript-safe integer maximum. It rereads the supplied file for each call, performs no separate canonical-spelling comparison and returns fixed errors without exposing file contents.
// readProofSignerGeneration читает не более тридцати двух байтов через securefile, обрезает внешние пробелы и разбирает положительный десятичный uint64 не выше безопасного для JavaScript целого. Файл перечитывается при каждом вызове, отдельного сравнения канонического написания нет, возвращаются фиксированные ошибки без раскрытия содержимого.
func readProofSignerGeneration(path string) (uint64, error) {
	raw, err := securefile.Read(path, 32)
	if err != nil {
		return 0, errors.New("read authority proof signer generation")
	}
	generation, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil || generation == 0 || generation > 9_007_199_254_740_991 {
		return 0, errors.New("authority proof signer generation rejected")
	}
	return generation, nil
}

var _ repository.SnapshotAttestor = (*proofResolverAttestor)(nil)

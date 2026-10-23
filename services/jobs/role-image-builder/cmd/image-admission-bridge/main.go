package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/clients/imageowner"
	"github.com/google/uuid"
	"google.golang.org/grpc/status"
)

// maximumStateBytes caps encoded claim/promotion snapshots and the bytes accepted
// by readState; vulnerability reports use their separate runtime-contract limit.
// maximumStateBytes ограничивает закодированные снимки claim/promotion и объём,
// принимаемый readState; отчёты об уязвимостях имеют отдельный лимит контракта.
const maximumStateBytes = 32 << 10

// main owns the signal-cancelled process context and reports only the gRPC status
// code on failure, never the remote error text or private evidence payload.
// main создаёт контекст процесса с отменой по сигналу и при ошибке выводит только
// gRPC-код, не раскрывая удалённый текст ошибки или закрытые доказательства.
func main() {
	base := context.Background()
	ctx, stop := signal.NotifyContext(base, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, bridgeDiagnostic(err))
		os.Exit(1)
	}
}

// run checks the image-owner client before dispatching one admission/promotion
// command. Admission mutations retain the exact claim attempt; record binds the
// inventory and report to that claim, while promotion updates its own snapshot.
// The owner client performs the RPCs; this bridge does not publish image bytes.
// run проверяет клиент владельца образов перед одной командой допуска/promotion.
// Изменения допуска сохраняют точную попытку claim; record связывает с ней
// inventory и отчёт, а promotion обновляет отдельный снимок. RPC выполняет клиент
// владельца; сам мост не публикует байты образа.
func run(ctx context.Context) error {
	if len(os.Args) != 2 {
		return errors.New("usage: image-admission-bridge claim|record|fail|claim-promotion|authorize-promotion|complete")
	}
	operation := os.Args[1]
	promotionMode := operation == "claim-promotion" || operation == "authorize-promotion" || operation == "complete"
	config, err := clientConfig(promotionMode)
	if err != nil {
		return err
	}
	client, err := imageowner.Dial(ctx, config)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Check(ctx); err != nil {
		return err
	}
	statePath, err := requiredPath("IMAGE_OWNER_STATE_FILE")
	if err != nil {
		return err
	}
	runID, err := requiredEnv("ADMISSION_RUN_ID")
	if err != nil {
		return err
	}
	switch operation {
	case "fail":
		var claim imageowner.Claim
		if err := readState(statePath, &claim); err != nil {
			return err
		}
		if err := imageowner.ValidateAdmissionClaim(claim); err != nil {
			return err
		}
		code, err := requiredEnv("IMAGE_OWNER_ADMISSION_FAILURE_CODE")
		if err != nil {
			return err
		}
		switch code {
		case "ADMISSION_EVIDENCE_ENTRY_EXCEEDS_BOUND", "ADMISSION_EVIDENCE_EXCEEDS_BOUND", "ADMISSION_WORKER_FAILED":
		default:
			return errors.New("image admission failure code is invalid")
		}
		return client.FailWithTerminalRecovery(ctx, admissionIdempotencyKey(operation, runID, claim), idempotencyKey("claim", runID), claim, code)
	case "claim":
		claim, err := client.Claim(ctx, idempotencyKey(operation, runID))
		if err != nil {
			return err
		}
		return writeClaimState(statePath, claim)
	case "record":
		var claim imageowner.Claim
		if err := readState(statePath, &claim); err != nil {
			return err
		}
		if err := imageowner.ValidateAdmissionClaim(claim); err != nil {
			return err
		}
		sbom, err := readSHAFile("IMAGE_OWNER_SBOM_SHA256_FILE")
		if err != nil {
			return err
		}
		vulnerability, err := readSHAFile("IMAGE_OWNER_VULNERABILITY_SHA256_FILE")
		if err != nil {
			return err
		}
		signature, err := readSHAFile("IMAGE_OWNER_SIGNATURE_SHA256_FILE")
		if err != nil {
			return err
		}
		receipt, err := readSHAFile("IMAGE_OWNER_ADMISSION_RECEIPT_SHA256_FILE")
		if err != nil {
			return err
		}
		receiptManifest, err := readManifestDigestFile("IMAGE_OWNER_ADMISSION_RECEIPT_OCI_MANIFEST_DIGEST_FILE")
		if err != nil {
			return err
		}
		signatureIdentity, identityErr := requiredEnv("IMAGE_OWNER_SIGNATURE_IDENTITY")
		verdict, verdictErr := requiredEnv("IMAGE_OWNER_VERDICT")
		if identityErr != nil || verdictErr != nil || (verdict != "ACCEPTED" && verdict != "REJECTED") {
			return errors.New("image admission evidence environment is invalid")
		}
		evidence := imageowner.AdmissionEvidence{SBOMSHA256: sbom,
			VulnerabilityEvidenceSHA256: vulnerability,
			SignatureIdentity:           signatureIdentity, SignatureSHA256: signature,
			AdmissionReceiptSHA256: receipt, AdmissionReceiptOCIManifestDigest: receiptManifest,
			Accepted: verdict == "ACCEPTED"}
		inventoryPath, err := requiredPath("IMAGE_OWNER_TOOL_INVENTORY_FILE")
		if err != nil {
			return err
		}
		inventoryFile, err := os.Open(inventoryPath)
		if err != nil {
			return errors.New("image tool inventory claim binding is invalid")
		}
		inventoryRaw, err := io.ReadAll(io.LimitReader(inventoryFile, runtimecontract.MaximumImageInventoryBytes+1))
		closeErr := inventoryFile.Close()
		inventory, decodeErr := runtimecontract.DecodeImageToolInventory(inventoryRaw)
		if err != nil || closeErr != nil || decodeErr != nil || inventory.ImageDigest != claim.ManifestDigest || inventory.ProvenanceSHA256 != claim.ProvenanceSHA256 {
			return errors.New("image tool inventory claim binding is invalid")
		}
		evidence.ToolInventoryJSON, evidence.ToolInventorySHA256 = string(inventoryRaw), runtimecontract.ImageInventorySHA256(inventoryRaw)
		if err := readVulnerabilityReportEvidence(claim, &evidence); err != nil {
			return err
		}
		return client.Record(ctx, admissionIdempotencyKey(operation, runID, claim), claim, evidence)
	case "claim-promotion":
		promotion, err := client.ClaimPromotion(ctx, idempotencyKey(operation, runID))
		if err != nil {
			return err
		}
		promotionPath, pathErr := requiredPath("IMAGE_OWNER_PROMOTION_FILE")
		if pathErr != nil {
			return pathErr
		}
		return writeState(promotionPath, promotion)
	case "authorize-promotion":
		promotionPath, pathErr := requiredPath("IMAGE_OWNER_PROMOTION_FILE")
		if pathErr != nil {
			return pathErr
		}
		var promotion imageowner.Promotion
		if err := readState(promotionPath, &promotion); err != nil {
			return err
		}
		if err := client.AuthorizePromotion(ctx, idempotencyKey(operation, runID+"\x00"+promotion.ArtifactID), &promotion); err != nil {
			return err
		}
		return writeState(promotionPath, promotion)
	case "complete":
		var promotion imageowner.Promotion
		promotionPath, pathErr := requiredPath("IMAGE_OWNER_PROMOTION_FILE")
		if pathErr != nil {
			return pathErr
		}
		if err := readState(promotionPath, &promotion); err != nil {
			return err
		}
		promotion.PromotedReference, err = requiredEnv("IMAGE_OWNER_PROMOTED_REFERENCE")
		if err != nil {
			return err
		}
		readback, err := readSHAFile("IMAGE_OWNER_PROMOTION_READBACK_SHA256_FILE")
		if err != nil {
			return err
		}
		promotion.ReadbackSHA256 = readback
		return client.Complete(ctx, idempotencyKey(operation, runID+"\x00"+promotion.ArtifactID), promotion)
	default:
		return errors.New("image admission bridge operation is invalid")
	}
}

// clientConfig selects only the explicit trusted-cluster profile or the protected
// default requiring TLS/grant paths. It validates path spelling, not file contents,
// and preserves the promotion mode and bounded dial/RPC timeouts for the client.
// clientConfig выбирает только явный trusted-cluster либо защищённый профиль
// по умолчанию с путями TLS/grant. Проверяется запись путей, а не содержимое файлов;
// клиент получает режим promotion и ограниченные тайм-ауты соединения/RPC.
func clientConfig(promotion bool) (imageowner.Config, error) {
	profile := os.Getenv("KODEX_RPC_PROFILE")
	if profile == transportprofile.TrustedCluster {
		target, err := requiredEnv("IMAGE_OWNER_CONTROL_PLANE_TARGET")
		if err != nil {
			return imageowner.Config{}, err
		}
		return imageowner.Config{RPCProfile: profile, Target: target, DialTimeout: 3 * time.Second,
			RPCDeadline: 8 * time.Second, Promotion: promotion}, nil
	}
	if profile != "" {
		return imageowner.Config{}, errors.New("image owner RPC profile is invalid")
	}
	values := make([]string, 0, 6)
	for _, name := range []string{"IMAGE_OWNER_CONTROL_PLANE_TARGET", "IMAGE_OWNER_CONTROL_PLANE_TLS_SERVER_NAME",
		"IMAGE_OWNER_CONTROL_PLANE_CA_FILE", "IMAGE_OWNER_CONTROL_PLANE_CERTIFICATE_FILE",
		"IMAGE_OWNER_CONTROL_PLANE_PRIVATE_KEY_FILE", "IMAGE_OWNER_APPLICATION_GRANT_FILE"} {
		value, err := requiredEnv(name)
		if err != nil {
			return imageowner.Config{}, err
		}
		values = append(values, value)
	}
	for _, value := range values[2:] {
		if !filepath.IsAbs(value) || filepath.Clean(value) != value {
			return imageowner.Config{}, errors.New("image owner path is invalid")
		}
	}
	return imageowner.Config{Target: values[0], TLSServerName: values[1], CAFile: values[2],
		ClientCertificateFile: values[3], ClientPrivateKeyFile: values[4], ApplicationGrantFile: values[5],
		ExpectedIssuerUID: 29001, ExpectedIssuerGID: 29000, DialTimeout: 3 * time.Second,
		RPCDeadline: 8 * time.Second, Promotion: promotion}, nil
}

// idempotencyKey derives a stable UUID from the bridge namespace, operation and
// seed separated by NUL bytes; it is a receipt key, not an authorization token.
// idempotencyKey получает устойчивый UUID из пространства моста, операции и seed
// с NUL-разделителями; это ключ квитанции, а не токен полномочий.
func idempotencyKey(operation, seed string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("image-admission-bridge\x00"+operation+"\x00"+seed)).String()
}

// admissionIdempotencyKey scopes record/fail receipts to the run, artifact and
// exact admission attempt reference/number so recovery reuses only that attempt.
// admissionIdempotencyKey связывает квитанции record/fail с запуском, артефактом
// и точными ссылкой/номером попытки допуска для повторного обращения той же попытки.
func admissionIdempotencyKey(operation, runID string, claim imageowner.Claim) string {
	return idempotencyKey(operation, runID+"\x00"+claim.ArtifactID+"\x00"+claim.AdmissionAttemptRef+"\x00"+strconv.FormatUint(uint64(claim.AdmissionAttempt), 10))
}

// bridgeDiagnostic projects any local or wrapped RPC error to a fixed prefix and
// status code, excluding credentials, evidence and arbitrary upstream diagnostics.
// bridgeDiagnostic сводит локальную или обёрнутую RPC-ошибку к постоянному префиксу
// и коду, исключая реквизиты, доказательства и произвольную диагностику сервера.
func bridgeDiagnostic(err error) string {
	return "image admission bridge failed: " + status.Code(err).String()
}

// writeClaimState publishes the first validated, bounded claim through an exclusive
// private temporary file, file fsync, hard link and directory fsync. An existing
// destination is accepted only when its entire validated canonical snapshot matches;
// recovery cannot replace its fence, token or risk decision with a newer claim.
// writeClaimState публикует первый проверенный ограниченный claim через эксклюзивный
// закрытый временный файл, fsync файла, жёсткую ссылку и fsync каталога. Существующий
// путь принимается лишь при полном совпадении проверенного канонического снимка;
// восстановление не заменяет fence, токен или решение о риске новым claim.
func writeClaimState(path string, claim imageowner.Claim) error {
	if err := imageowner.ValidateAdmissionClaim(claim); err != nil {
		return err
	}
	raw, err := json.Marshal(claim)
	if err != nil || len(raw) > maximumStateBytes {
		return errors.New("encode bounded image owner state")
	}
	temporary := path + "." + uuid.NewString() + ".tmp"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return errors.New("write bounded image owner state")
	}
	defer os.Remove(temporary)
	_, writeErr := file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("write bounded image owner state")
	}
	if err := os.Link(temporary, path); err == nil {
		directory, openErr := os.Open(filepath.Dir(path))
		if openErr != nil {
			return errors.New("commit bounded image owner state")
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if syncErr != nil || closeErr != nil {
			return errors.New("commit bounded image owner state")
		}
		return nil
	} else if !errors.Is(err, os.ErrExist) {
		return errors.New("commit bounded image owner state")
	}
	var retained imageowner.Claim
	if readState(path, &retained) != nil || imageowner.ValidateAdmissionClaim(retained) != nil {
		return errors.New("retained image admission claim is invalid")
	}
	previous, _ := json.Marshal(retained)
	if !bytes.Equal(previous, raw) {
		return errors.New("retained image admission claim is immutable")
	}
	return nil
}

// readVulnerabilityReportEvidence loads the bounded private report and projection
// digest, copies risk acceptance from the retained claim rather than the environment,
// then validates the complete evidence binding. The pointed value is updated before
// validation; callers must discard it if an error is returned.
// readVulnerabilityReportEvidence читает ограниченный закрытый отчёт и хеш проекции,
// берёт принятие риска из сохранённого claim, а не окружения, затем проверяет полную
// привязку доказательств. Значение по указателю изменяется до проверки; при ошибке
// вызывающий код не должен использовать его как прошедшее проверку.
func readVulnerabilityReportEvidence(claim imageowner.Claim, evidence *imageowner.AdmissionEvidence) error {
	raw, err := readBoundedPrivateFile("IMAGE_OWNER_VULNERABILITY_REPORT_JSON_FILE", runtimecontract.MaximumImageVulnerabilityReportBytes)
	if err != nil {
		return err
	}
	projectionSHA, err := readSHAFile("IMAGE_OWNER_VULNERABILITY_REPORT_PROJECTION_SHA256_FILE")
	if err != nil {
		return err
	}
	evidence.VulnerabilityReportJSON = string(raw)
	evidence.VulnerabilityReportProjectionSHA256 = projectionSHA
	evidence.RiskAcceptanceSHA256 = claim.RiskAcceptanceSHA256
	return imageowner.ValidateAdmissionEvidence(claim, *evidence)
}

// readBoundedPrivateFile opens an environment-selected clean absolute path without
// following its final symlink or blocking on a FIFO. It requires a nonempty regular
// 0600 file owned by the effective user and checks the size both before and after
// reading at most maximum+1 bytes; errors omit the file contents and path.
// readBoundedPrivateFile открывает заданный окружением чистый абсолютный путь без
// перехода по последней символьной ссылке и блокировки на FIFO. Нужен непустой
// обычный файл 0600 эффективного пользователя; размер проверяется до и после чтения
// не более maximum+1 байтов. Ошибки не содержат содержимое файла и путь.
func readBoundedPrivateFile(name string, maximum int64) ([]byte, error) {
	path, err := requiredPath(name)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("read bounded image report evidence")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() <= 0 || info.Size() > maximum {
		return nil, errors.New("read bounded image report evidence")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("read bounded image report evidence")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > maximum {
		return nil, errors.New("read bounded image report evidence")
	}
	return raw, nil
}

// writeState marshals a bounded mutable promotion snapshot, writes path+.tmp and
// renames it over the destination. Unlike writeClaimState, it provides neither
// no-clobber publication nor explicit file/directory fsync or existing-temp checks.
// writeState кодирует ограниченный изменяемый снимок promotion, пишет path+.tmp
// и заменяет целевой путь переименованием. В отличие от writeClaimState здесь нет
// неизменяемой публикации, явного fsync файла/каталога и проверки старого temp-файла.
func writeState(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > maximumStateBytes {
		return errors.New("encode bounded image owner state")
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, raw, 0o600); err != nil {
		return errors.New("write bounded image owner state")
	}
	if err := os.Rename(temporary, path); err != nil {
		return errors.New("commit bounded image owner state")
	}
	return nil
}

// readState accepts only an effective-user-owned regular 0600 file within the state
// bound, without following a final symlink or waiting on a FIFO. It rejects unknown
// fields, trailing JSON and any byte sequence differing from re-marshalled target;
// domain-specific claim validity remains the caller's separate check.
// readState принимает только обычный файл 0600 эффективного пользователя в пределах
// лимита, без перехода по последней ссылке и ожидания FIFO. Отклоняются неизвестные
// поля, дополнительный JSON и байты, отличающиеся от повторной сериализации target;
// доменная корректность claim отдельно проверяется вызывающим кодом.
func readState(path string, target any) error {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return errors.New("read bounded image owner state")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() <= 0 || info.Size() > maximumStateBytes {
		return errors.New("read bounded image owner state")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("read bounded image owner state")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumStateBytes+1))
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err != nil || len(raw) == 0 || len(raw) > maximumStateBytes || decoder.Decode(target) != nil {
		return errors.New("read bounded image owner state")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("read bounded image owner state")
	}
	canonical, err := json.Marshal(target)
	if err != nil || !bytes.Equal(canonical, raw) {
		return errors.New("read bounded image owner state")
	}
	return nil
}

// readSHAFile reads an environment-selected digest file completely, trims surrounding
// whitespace and requires exactly 64 lowercase hexadecimal characters. Unlike report
// and state readers, it does not enforce private-file metadata or bounded input I/O.
// readSHAFile целиком читает заданный окружением файл хеша, убирает крайние пробелы
// и требует ровно 64 строчных шестнадцатеричных символа. В отличие от чтения отчёта
// и состояния здесь не проверяются закрытые метаданные и лимит входного чтения.
func readSHAFile(name string) (string, error) {
	path, err := requiredPath(name)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("read image evidence digest")
	}
	value := strings.TrimSpace(string(raw))
	if len(value) != 64 || strings.Trim(value, "0123456789abcdef") != "" {
		return "", errors.New("image evidence digest is invalid")
	}
	return value, nil
}

// readManifestDigestFile requires a trimmed sha256: manifest digest with 64 lowercase
// hexadecimal digits; both read errors and malformed values become one safe error.
// readManifestDigestFile требует хеш манифеста sha256: с 64 строчными hex-цифрами
// после удаления крайних пробелов; ошибки чтения и формата имеют одну безопасную ошибку.
func readManifestDigestFile(name string) (string, error) {
	value, err := readDigestFile(name)
	if err != nil || !strings.HasPrefix(value, "sha256:") || len(value) != 71 ||
		strings.Trim(strings.TrimPrefix(value, "sha256:"), "0123456789abcdef") != "" {
		return "", errors.New("image evidence manifest digest is invalid")
	}
	return value, nil
}

// readDigestFile reads the entire file at a validated environment path and trims
// whitespace without validating the digest syntax, ownership, mode or input size.
// readDigestFile целиком читает файл по проверенному пути из окружения и убирает
// крайние пробелы, не проверяя синтаксис хеша, владельца, режим и размер входа.
func readDigestFile(name string) (string, error) {
	path, err := requiredPath(name)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("read image evidence digest")
	}
	return strings.TrimSpace(string(raw)), nil
}

// requiredPath requires a valid environment value that is already a clean absolute
// path; it does not resolve symlinks, inspect ownership or establish path authority.
// requiredPath требует корректное значение окружения в виде уже чистого абсолютного
// пути; ссылки, владелец и полномочия на путь этой функцией не проверяются.
func requiredPath(name string) (string, error) {
	value, err := requiredEnv(name)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return "", errors.New("image owner path is invalid")
	}
	return value, nil
}

// requiredEnv requires a nonempty value of at most 2048 bytes without CR, LF or NUL,
// preserving other bytes and returning a fixed error without the name or value.
// requiredEnv требует непустое значение до 2048 байтов без CR, LF и NUL, сохраняя
// остальные байты; постоянная ошибка не раскрывает имя переменной или её значение.
func requiredEnv(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" || len(value) > 2048 || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("required image owner environment is invalid")
	}
	return value, nil
}

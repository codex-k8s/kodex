package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/clients/imageowner"
	"github.com/google/uuid"
)

const maximumStateBytes = 32 << 10

func main() {
	base := context.Background()
	ctx, stop := signal.NotifyContext(base, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if len(os.Args) != 2 {
		return errors.New("usage: image-admission-bridge claim|record|claim-promotion|authorize-promotion|complete")
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
	case "claim":
		claim, err := client.Claim(ctx, idempotencyKey(operation, runID))
		if err != nil {
			return err
		}
		return writeState(statePath, claim)
	case "record":
		var claim imageowner.Claim
		if err := readState(statePath, &claim); err != nil {
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
		return client.Record(ctx, idempotencyKey(operation, runID+"\x00"+claim.ArtifactID), claim, evidence)
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

func clientConfig(promotion bool) (imageowner.Config, error) {
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

func idempotencyKey(operation, seed string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("image-admission-bridge\x00"+operation+"\x00"+seed)).String()
}

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

func readState(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > maximumStateBytes || json.Unmarshal(raw, target) != nil {
		return errors.New("read bounded image owner state")
	}
	return nil
}

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

func readManifestDigestFile(name string) (string, error) {
	value, err := readDigestFile(name)
	if err != nil || !strings.HasPrefix(value, "sha256:") || len(value) != 71 ||
		strings.Trim(strings.TrimPrefix(value, "sha256:"), "0123456789abcdef") != "" {
		return "", errors.New("image evidence manifest digest is invalid")
	}
	return value, nil
}

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

func requiredEnv(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" || len(value) > 2048 || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("required image owner environment is invalid")
	}
	return value, nil
}

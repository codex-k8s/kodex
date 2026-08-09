package resource

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
)

var configurationSecretPattern = regexp.MustCompile(`(?i)(?:-----BEGIN [A-Z ]*PRIVATE KEY-----|\b(?:AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{20,}|sk-[A-Za-z0-9_-]{20,})\b|\b[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{12,}\b|://[^/@[:space:]]+:[^/@[:space:]]+@)`)

type configurationDiffCursor struct {
	ComparisonSHA256    string `json:"comparisonSha256"`
	LeftVersion         uint64 `json:"leftVersion"`
	LeftSHA256          string `json:"leftSha256"`
	LeftSnapshotSHA256  string `json:"leftSnapshotSha256"`
	RightVersion        uint64 `json:"rightVersion"`
	RightSHA256         string `json:"rightSha256"`
	RightSnapshotSHA256 string `json:"rightSnapshotSha256"`
	Offset              int    `json:"offset"`
}

func (service *Service) buildConfigurationDiffPage(
	leftVersion uint64,
	leftSHA256, leftSnapshotSHA256, leftContent string,
	rightVersion uint64,
	rightSHA256, rightSnapshotSHA256, rightContent, comparisonSHA256, token string,
	limit int,
) (OwnerConfigurationPage, error) {
	if limit < 1 || limit > 100 {
		return OwnerConfigurationPage{}, errs.ErrInvalidInput
	}
	cursor := configurationDiffCursor{ComparisonSHA256: comparisonSHA256,
		LeftVersion: leftVersion, LeftSHA256: leftSHA256, LeftSnapshotSHA256: leftSnapshotSHA256,
		RightVersion: rightVersion, RightSHA256: rightSHA256, RightSnapshotSHA256: rightSnapshotSHA256}
	if token != "" {
		decoded, err := service.decodeConfigurationDiffCursor(token)
		if err != nil || decoded.ComparisonSHA256 != comparisonSHA256 || decoded.LeftVersion != leftVersion ||
			decoded.LeftSHA256 != leftSHA256 || decoded.LeftSnapshotSHA256 != leftSnapshotSHA256 ||
			decoded.RightVersion != rightVersion || decoded.RightSHA256 != rightSHA256 ||
			decoded.RightSnapshotSHA256 != rightSnapshotSHA256 {
			return OwnerConfigurationPage{}, errs.ErrStateConflict
		}
		cursor = decoded
	}
	changes := boundedLineChanges(leftContent, rightContent)
	if cursor.Offset < 0 || cursor.Offset > len(changes) {
		return OwnerConfigurationPage{}, errs.ErrInvalidInput
	}
	end := cursor.Offset + limit
	if end > len(changes) {
		end = len(changes)
	}
	page := OwnerConfigurationPage{Changes: slicesCloneChanges(changes[cursor.Offset:end])}
	if end < len(changes) {
		cursor.Offset = end
		var err error
		page.NextPageToken, err = service.encodeConfigurationDiffCursor(cursor)
		if err != nil {
			return OwnerConfigurationPage{}, err
		}
		page.Truncated = true
	}
	return page, nil
}

func slicesCloneChanges(input []ConfigurationChange) []ConfigurationChange {
	return append([]ConfigurationChange(nil), input...)
}

func boundedLineChanges(left, right string) []ConfigurationChange {
	leftLines, rightLines := strings.Split(left, "\n"), strings.Split(right, "\n")
	maximum := len(leftLines)
	if len(rightLines) > maximum {
		maximum = len(rightLines)
	}
	changes := make([]ConfigurationChange, 0)
	for index := 0; index < maximum; index++ {
		before, after, kind := "", "", "CHANGED"
		if index < len(leftLines) {
			before = leftLines[index]
		} else {
			kind = "ADDED"
		}
		if index < len(rightLines) {
			after = rightLines[index]
		} else {
			kind = "REMOVED"
		}
		if before == after {
			continue
		}
		display := "TEXT"
		if configurationLineSensitive(before) || configurationLineSensitive(after) {
			before, after, display = "[REDACTED]", "[REDACTED]", "REDACTED"
		} else {
			before, after = boundedDisplayText(before), boundedDisplayText(after)
		}
		changes = append(changes, ConfigurationChange{Kind: kind,
			Path: "/content/lines/" + strconv.Itoa(index+1), Display: display, Before: before, After: after})
	}
	return changes
}

func configurationLineSensitive(value string) bool {
	if !utf8.ValidString(value) {
		return true
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"password", "passwd", "secret", "token", "private_key", "private key",
		"authorization:", "cookie:", "dsn", "credential"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return configurationSecretPattern.MatchString(value)
}

func boundedDisplayText(value string) string {
	const maximum = 256
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "…"
}

func (service *Service) encodeConfigurationDiffCursor(cursor configurationDiffCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", errs.ErrInternal
	}
	mac := hmac.New(sha256.New, service.leaseSigningKey)
	_, _ = mac.Write([]byte("control-plane:configuration-diff:v1\x00"))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func (service *Service) decodeConfigurationDiffCursor(token string) (configurationDiffCursor, error) {
	if len(token) > 2048 {
		return configurationDiffCursor{}, errs.ErrInvalidInput
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return configurationDiffCursor{}, errs.ErrInvalidInput
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	signature, signatureErr := hex.DecodeString(parts[1])
	if err != nil || signatureErr != nil {
		return configurationDiffCursor{}, errs.ErrInvalidInput
	}
	mac := hmac.New(sha256.New, service.leaseSigningKey)
	_, _ = mac.Write([]byte("control-plane:configuration-diff:v1\x00"))
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return configurationDiffCursor{}, errs.ErrStateConflict
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var cursor configurationDiffCursor
	if err := decoder.Decode(&cursor); err != nil || cursor.Offset <= 0 ||
		!validSHA256Text(cursor.ComparisonSHA256) || !validSHA256Text(cursor.LeftSHA256) ||
		!validSHA256Text(cursor.LeftSnapshotSHA256) || !validSHA256Text(cursor.RightSHA256) ||
		!validSHA256Text(cursor.RightSnapshotSHA256) || cursor.LeftVersion == 0 || cursor.RightVersion == 0 {
		return configurationDiffCursor{}, errs.ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return configurationDiffCursor{}, errs.ErrInvalidInput
	}
	return cursor, nil
}

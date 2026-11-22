package objectstorage

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaximumRangeBytes limits one immutable slice to an existing stream frame budget.
// MaximumRangeBytes ограничивает immutable slice существующим stream frame budget.
const MaximumRangeBytes int64 = 32768

// RangeRead pins the complete owner-authorized object and the separate exact slice digest.
// It never supplies artifact visibility or native execution authority; the owner must establish both.
// RangeRead закрепляет полный owner-authorized object и отдельный exact slice digest.
// Artifact visibility либо native execution authority не выдаёт; owner проверяет их отдельно.
type RangeRead struct {
	Object         Receipt
	Offset, Length int64
	Digest         string
}

// Validate rejects ambiguous, overflowing or unversioned ranges before storage IO.
// Only one positive closed range is accepted; an empty EOF is an owner ledger fact, not a GET.
// Validate отклоняет ambiguous, overflowing либо unversioned ranges до storage IO.
// Принимается один positive closed range; empty EOF принадлежит owner ledger, не GET.
func (input RangeRead) Validate() error {
	if !ValidKey(input.Object.Key) || !ValidDigest(input.Object.Digest) || !ValidDigest(input.Digest) ||
		input.Object.SizeBytes < 1 || input.Offset < 0 || input.Length < 1 || input.Length > MaximumRangeBytes ||
		input.Length > input.Object.SizeBytes || input.Offset > input.Object.SizeBytes-input.Length ||
		input.Object.VersionID == "" || input.Object.VersionID == "null" || len(input.Object.VersionID) > 1024 || !utf8.ValidString(input.Object.VersionID) ||
		input.Object.ETag == "" || len(input.Object.ETag) > 1024 || !utf8.ValidString(input.Object.ETag) ||
		strings.ContainsAny(input.Object.ETag, "\"\\") {
		return ErrInvalid
	}
	for _, value := range []string{input.Object.VersionID, input.Object.ETag} {
		for _, char := range value {
			if unicode.IsControl(char) || unicode.IsSpace(char) {
				return ErrInvalid
			}
		}
	}
	return nil
}

// RangeReader is an optional capability of the existing versioned store, not a new transport.
// It returns at most32KiB owned bytes only after exact source metadata and slice hash validation;
// implementations close and join their response body before returning, including error paths.
// RangeReader — optional capability существующего versioned store, не новый transport.
// Возвращает до32KiB owned bytes после сверки exact source metadata и slice hash;
// implementation закрывает response body до возврата, включая error paths.
type RangeReader interface {
	// ReadRange returns owned verified slice bytes, never a transferable object locator or grant.
	// ReadRange возвращает owned verified slice bytes, не transferable object locator либо grant.
	ReadRange(context.Context, RangeRead) ([]byte, error)
}

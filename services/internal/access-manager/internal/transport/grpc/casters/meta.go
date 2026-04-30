package casters

import (
	"strings"

	"github.com/google/uuid"

	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

// CommandMetaFromProto maps command metadata without importing transport types into the domain layer.
func CommandMetaFromProto(meta *accessaccountsv1.CommandMeta) (value.CommandMeta, error) {
	if meta == nil {
		return value.CommandMeta{}, nil
	}
	commandID, err := optionalUUID(meta.GetCommandId())
	if err != nil {
		return value.CommandMeta{}, err
	}
	var expectedVersion *int64
	if meta.ExpectedVersion != nil {
		expected := meta.GetExpectedVersion()
		expectedVersion = &expected
	}
	actor := meta.GetActor()
	return value.CommandMeta{
		CommandID:       commandID,
		IdempotencyKey:  strings.TrimSpace(meta.GetIdempotencyKey()),
		ExpectedVersion: expectedVersion,
		Actor: value.Actor{
			Type: strings.TrimSpace(actor.GetType()),
			ID:   strings.TrimSpace(actor.GetId()),
		},
		Reason:    strings.TrimSpace(meta.GetReason()),
		RequestID: strings.TrimSpace(meta.GetRequestId()),
	}, nil
}

// SubjectRefFromProto maps a gRPC subject reference to the domain value object.
func SubjectRefFromProto(ref *accessaccountsv1.SubjectRef) value.SubjectRef {
	refType, refID := typeID(ref)
	return value.SubjectRef{Type: refType, ID: refID}
}

// ResourceRefFromProto maps a gRPC resource reference to the domain value object.
func ResourceRefFromProto(ref *accessaccountsv1.ResourceRef) value.ResourceRef {
	refType, refID := typeID(ref)
	return value.ResourceRef{Type: refType, ID: refID}
}

// ScopeRefFromProto maps a gRPC scope reference to the domain value object.
func ScopeRefFromProto(ref *accessaccountsv1.ScopeRef) value.ScopeRef {
	refType, refID := typeID(ref)
	return value.ScopeRef{Type: refType, ID: refID}
}

// SubjectRefToProto maps a domain subject reference to gRPC.
func SubjectRefToProto(ref value.SubjectRef) *accessaccountsv1.SubjectRef {
	return &accessaccountsv1.SubjectRef{Type: ref.Type, Id: ref.ID}
}

// ScopeRefToProto maps a domain scope reference to gRPC.
func ScopeRefToProto(ref value.ScopeRef) *accessaccountsv1.ScopeRef {
	return &accessaccountsv1.ScopeRef{Type: ref.Type, Id: ref.ID}
}

func typeID(ref interface {
	GetType() string
	GetId() string
}) (string, string) {
	if ref == nil {
		return "", ""
	}
	return strings.TrimSpace(ref.GetType()), strings.TrimSpace(ref.GetId())
}

func optionalUUID(raw string) (uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	return id, nil
}

func requiredUUID(raw string) (uuid.UUID, error) {
	id, err := optionalUUID(raw)
	if err != nil {
		return uuid.Nil, err
	}
	if id == uuid.Nil {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	return id, nil
}

func optionalUUIDPtr(raw string) (*uuid.UUID, error) {
	id, err := optionalUUID(raw)
	if err != nil {
		return nil, err
	}
	if id == uuid.Nil {
		return nil, nil
	}
	return &id, nil
}

func uuidString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func uuidPtrString(id *uuid.UUID) string {
	if id == nil || *id == uuid.Nil {
		return ""
	}
	return id.String()
}

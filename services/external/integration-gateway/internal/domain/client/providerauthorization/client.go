package providerauthorization

import (
	"context"
	"time"
)

type (
	DeviceCode struct {
		VerificationURL, UserCode string
		ExpiresAt                 time.Time
	}
	CapacityObservation struct {
		Usage, Limit, Revision, WindowSeconds uint64
		ObservedAt, ResetsAt, ExpiresAt       time.Time
	}
	Result struct {
		Credential                 []byte
		MaskedAccount, MaskedLabel string
		Capacity                   CapacityObservation
	}
	Client interface {
		Authorize(context.Context, func(DeviceCode) error, func(context.Context) (bool, error)) (Result, error)
		Test(context.Context, []byte) (CapacityObservation, error)
		Revoke(context.Context, []byte) error
		Check(context.Context) error
	}
)

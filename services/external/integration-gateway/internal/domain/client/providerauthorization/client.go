package providerauthorization

import (
	"context"
	"time"
)

type (
	DeviceCode struct {
		LoginID, VerificationURL, UserCode string
		ExpiresAt                          time.Time
	}
	Result struct {
		Credential                 []byte
		MaskedAccount, MaskedLabel string
	}
	Client interface {
		Authorize(context.Context, func(DeviceCode) error, func(context.Context) (bool, error)) (Result, error)
		Test(context.Context, []byte) error
		Revoke(context.Context, []byte) error
		Check(context.Context) error
	}
)

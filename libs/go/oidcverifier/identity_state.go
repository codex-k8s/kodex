package oidcverifier

import "errors"

// ErrIdentityStateUnavailable distinguishes unavailable durable application identity state from an invalid signature or a denied user.
// ErrIdentityStateUnavailable отличает недоступное durable application identity state от неверной подписи и denied user.
var ErrIdentityStateUnavailable = errors.New("OIDC identity state is unavailable")

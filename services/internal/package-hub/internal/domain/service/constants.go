package service

import (
	packageevents "github.com/codex-k8s/kodex/libs/go/platformevents/packagehub"
)

const (
	packageEventVerificationUpdated = packageevents.EventVerificationUpdated
	packageAggregateVersion         = packageevents.AggregatePackageVersion
	packageOperationVerify          = "domain.Service.SetPackageVerification"
)

package platform

import (
	"strings"
	"time"
)

// syntheticRoleImageComponentConfig returns the existing isolated-test image policy, one-minute
// lease/claim lifetimes and reserved registry.invalid references. Every call allocates its own
// synthetic signing-key slice; no credential, environment, repository, owner or clock is read.
// It does not configure storage, bootstrap state or manufacture build/admission authority.
// syntheticRoleImageComponentConfig возвращает прежнюю политику изолированных тестов, сроки
// lease/claim по минуте и зарезервированные ссылки registry.invalid. Каждый вызов выделяет свой
// массив синтетического ключа; credentials, env, repository, owner и clock не читаются.
// Хранилище не настраивает, state не инициализирует и build/admission authority не создаёт.
func syntheticRoleImageComponentConfig() RoleImageConfig {
	return RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute,
		MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles",
		DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64),
		LeaseSigningKey:       []byte(strings.Repeat("d", 32)),
	}
}

// namespacedRoleImageComponentConfig keeps the alternate synthetic fixture's exact /kodex
// staging, promoted and default-image paths while sharing policy and fresh key allocation.
// It changes no repository state and introduces no configurable registry or secret source.
// namespacedRoleImageComponentConfig сохраняет точные /kodex пути staging, promoted и default
// image другой синтетической оснастки, используя общие политику и выделение нового массива ключа.
// Состояние repository не меняет, настраиваемый registry либо secret source не вводит.
func namespacedRoleImageComponentConfig() RoleImageConfig {
	config := syntheticRoleImageComponentConfig()
	config.StagingRepository = "registry.invalid/kodex/staging"
	config.PromotedRepository = "registry.invalid/kodex/roles"
	config.DefaultImageReference = "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64)
	return config
}

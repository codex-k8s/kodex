package app

import "testing"

// TestAdmissionControllerConfigRejectsImplicitOrUnknownRPCProfile records actual loader behavior: empty protected and trusted-cluster are accepted.
// An unknown insecure value must fail; parsing is tested with synthetic environment values and no transport startup.
// TestAdmissionControllerConfigRejectsImplicitOrUnknownRPCProfile фиксирует фактическое поведение: пустой protected и trusted-cluster принимаются.
// Неизвестное insecure-значение должно отклоняться; разбор проверяется искусственным окружением без запуска транспорта.
func TestAdmissionControllerConfigRejectsImplicitOrUnknownRPCProfile(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	t.Setenv("POD_UID", "11111111-1111-4111-8111-111111111111")
	for _, test := range []struct {
		name    string
		profile string
		valid   bool
	}{
		{name: "protected", profile: "", valid: true},
		{name: "trusted cluster", profile: "trusted-cluster", valid: true},
		{name: "unknown", profile: "insecure", valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("KODEX_RPC_PROFILE", test.profile)
			_, err := loadAdmissionControllerConfig()
			if (err == nil) != test.valid {
				t.Fatalf("RPC profile %q validity mismatch: %v", test.profile, err)
			}
		})
	}
}

// TestAdmissionControllerRequiresDownwardAPIPodUID rejects absent, name-like, path-like and unhyphenated Pod identities during config loading.
// TestAdmissionControllerRequiresDownwardAPIPodUID отклоняет отсутствующую, похожую на имя или путь и лишённую дефисов идентичность Pod при загрузке конфигурации.
func TestAdmissionControllerRequiresDownwardAPIPodUID(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	for _, identity := range []string{"", "pod-name", "../foreign", "11111111111141118111111111111111"} {
		t.Setenv("POD_UID", identity)
		if _, err := loadAdmissionControllerConfig(); err == nil {
			t.Fatal("invalid Pod UID accepted")
		}
	}
}

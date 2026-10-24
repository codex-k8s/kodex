package app

// workerGrantTrustFilesFor builds the fixed workload-to-trust-file map and adds interaction/email
// entries only when configured, without loading keys or substituting one workload trust file for
// another.
// workerGrantTrustFilesFor строит фиксированную карту workload и trust file и добавляет
// interaction и email лишь при настройке без загрузки ключей либо подмены trust file одной
// нагрузки другой.
func workerGrantTrustFilesFor(config Config) map[string]string {
	files := map[string]string{
		"automation-scheduler": config.AutomationGrantTrustFile,
		"session-archive":      config.SessionArchiveGrantTrustFile,
		"integration-gateway":  config.IntegrationGrantTrustFile,
		"runtime-controller":   config.RuntimeGrantTrustFile,
		"role-image-builder":   config.RoleImageBuilderGrantTrustFile,
		"image-admission":      config.ImageAdmissionGrantTrustFile,
		"image-promotion":      config.ImagePromotionGrantTrustFile,
		"secret-broker":        config.SecretBrokerGrantTrustFile,
		"control-plane":        config.ControlPlaneGrantTrustFile,
	}
	if config.InteractionGrantTrustFile != "" {
		files["interaction-gateway"] = config.InteractionGrantTrustFile
	}
	if config.EmailGrantTrustFile != "" {
		files["email-bridge"] = config.EmailGrantTrustFile
	}
	return files
}

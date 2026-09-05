package app

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

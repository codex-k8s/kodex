// Package projection содержит закрытые правила внешних owner-проекций.
package projection

// IsConfigurationAction принимает только audit actions, которыми control-plane
// фактически фиксирует изменение управляемой конфигурации.
func IsConfigurationAction(action string) bool {
	switch action {
	case "create", "update", "transition", "delete",
		"detach_access_configuration", "copy_access_configuration",
		"create_schedule", "manage_schedule_UPDATE", "manage_schedule_ACTIVATE",
		"manage_schedule_PAUSE", "manage_schedule_ARCHIVE", "manage_schedule_DELETE":
		return true
	default:
		return false
	}
}

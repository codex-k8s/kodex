package entity

// EmailMailboxOwnershipView exposes only safe current ownership status to its configuring user; it never contains a secret, secret locator or generic token binding.
// EmailMailboxOwnershipView выдаёт только безопасное текущее состояние владения настроившему пользователю; не содержит секрет, его locator либо generic token binding.
type EmailMailboxOwnershipView struct {
	ConnectionRef     string                      `json:"connectionRef"`
	ConnectionVersion int64                       `json:"connectionVersion"`
	DeliveryKind      string                      `json:"deliveryKind"`
	Policy            IntegrationCredentialPolicy `json:"policy"`
	State             string                      `json:"state"`
	Generation        int64                       `json:"generation"`
	OwnerEligible     bool                        `json:"ownerEligible"`
	ServiceSubjectRef string                      `json:"serviceSubjectRef,omitempty"`
}

// MyEmailMailbox exposes a current self-owned connection and safe setup availability; listing it does not grant an agent access or prove provider readiness.
// MyEmailMailbox выдаёт текущее собственное подключение и безопасную доступность настройки; выдача не разрешает доступ агенту и не доказывает готовность провайдера.
type MyEmailMailbox struct {
	Ownership          EmailMailboxOwnershipView
	Name               string
	Enabled, Available bool
}

package entity

// EmailMailboxCredentialOwner binds mailbox authentication to an explicit stored
// user or service subject. It is private owner metadata, not a secret descriptor
// or a credential selector supplied by a model or a Human Gate approver.
// EmailMailboxCredentialOwner связывает аутентификацию ящика с явно сохранённым
// пользователем либо служебным субъектом. Это закрытые метаданные владельца,
// а не секрет или выбор credentials моделью либо участником Human Gate.
type EmailMailboxCredentialOwner struct {
	OrganizationID, SubjectID, SubjectKind string                      `json:"-"`
	Policy                                 IntegrationCredentialPolicy `json:"-"`
	Generation                             int64                       `json:"-"`
	SubjectActive                          bool                        `json:"-"`
}

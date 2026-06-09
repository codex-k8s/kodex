package value

type ServiceVersion string

type StatusSnapshot struct {
	Status               string
	ServiceName          string
	ServiceVersion       ServiceVersion
	MattermostConfigured bool
	BotTokenConfigured   bool
	SlashTokenConfigured bool
	DatabaseConfigured   bool
	StorageReady         bool
	RuntimeConfigured    bool
	DefaultTeamName      string
	DefaultChannels      []string
}

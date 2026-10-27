package platform

// assistantGrantConnectionHeader holds the connection identity and package pins returned by the
// owner query. Header shape does not establish authorization, readiness or package eligibility.
// assistantGrantConnectionHeader хранит идентичность соединения и привязки пакета из owner query.
// Форма заголовка не подтверждает авторизацию, готовность или допустимость пакета.
type assistantGrantConnectionHeader struct {
	ref, name, definitionKey, definitionVersion, definitionDigest string
	version                                                       int64
}

// validAssistantGrantConnectionHeader checks the exact reference and required lexical header fields
// without querying or granting authority; each caller retains its own tenant and access checks.
// validAssistantGrantConnectionHeader проверяет точную ссылку и обязательные поля заголовка без
// запросов и выдачи полномочий; проверки tenant и доступа остаются у каждого вызывающего.
func validAssistantGrantConnectionHeader(header assistantGrantConnectionHeader, connectionRef string) bool {
	return connectionRef != "" && header.ref == connectionRef && header.name != "" && header.version > 0 &&
		header.definitionKey != "" && header.definitionVersion != "" && validAssistantDefinitionDigest(header.definitionDigest)
}

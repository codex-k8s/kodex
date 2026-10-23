package session

import (
	_ "embed"
	"fmt"
	"strings"
)

// sessionIdentitySQL embeds readback of immutable session_user and current effective current_user, allowing the adapter to distinguish login identity from an assumed capability role.
// sessionIdentitySQL встраивает чтение неизменяемого session_user и эффективного current_user, позволяя адаптеру отличить LOGIN-идентичность от принятой capability-role.
//
//go:embed sql/session__identity.sql
var sessionIdentitySQL string

// issuerAssumeSQL embeds the exact SET ROLE for issuer capability. Execution changes the effective role only within PostgreSQL's granted permissions; the string does not create a login or replace session_user.
// issuerAssumeSQL встраивает точный SET ROLE для capability issuer. Исполнение меняет эффективную роль только в пределах выданных PostgreSQL прав; строка не создаёт LOGIN и не заменяет session_user.
//
//go:embed sql/issuer__assume.sql
var issuerAssumeSQL string

// verifierAssumeSQL embeds the fixed verifier SET ROLE rather than interpolating a caller-provided role name; PostgreSQL grants and the adapter's identity checks remain required.
// verifierAssumeSQL встраивает фиксированный SET ROLE verifier без подстановки имени роли от вызывающего; выданные PostgreSQL права и проверки идентичности адаптера остаются обязательными.
//
//go:embed sql/verifier__assume.sql
var verifierAssumeSQL string

// publisherAssumeSQL embeds the fixed publisher capability-role switch, distinct from authenticating the underlying LOGIN principal or validating its credential lifecycle.
// publisherAssumeSQL встраивает фиксированное переключение capability-role publisher отдельно от аутентификации исходного LOGIN principal и проверки жизненного цикла его credential.
//
//go:embed sql/publisher__assume.sql
var publisherAssumeSQL string

// readbackAttestorAssumeSQL embeds the exact readback-attestor capability-role switch; it does not grant that role or activate trust/readback state by being loaded.
// readbackAttestorAssumeSQL встраивает точное переключение capability-role readback-attestor; загрузка не выдаёт эту роль и не активирует состояние доверия или readback.
//
//go:embed sql/readback_attestor__assume.sql
var readbackAttestorAssumeSQL string

// restoreControllerAssumeSQL embeds the exact restore-controller SET ROLE without accepting dynamic role text. Permission to assume it must already exist at the database boundary.
// restoreControllerAssumeSQL встраивает точный SET ROLE restore-controller без динамического текста роли. Разрешение принять её уже должно существовать на границе БД.
//
//go:embed sql/restore_controller__assume.sql
var restoreControllerAssumeSQL string

// querySet stores the fixed session identity query and five role-switch statements selected by adapter code; it does not cache a principal or authorization decision.
// querySet хранит фиксированный запрос идентичности сессии и пять переключений ролей, выбираемых кодом адаптера; principal и решение об авторизации не кешируются.
type querySet struct {
	sessionIdentity         string
	issuerAssume            string
	verifierAssume          string
	publisherAssume         string
	readbackAttestorAssume  string
	restoreControllerAssume string
}

// loadQueries assembles six original embedded bodies and validates their nonempty name/cardinality prefixes before returning the fixed set. It neither executes SET ROLE nor parses complete SQL or enumerates filesystem entries.
// loadQueries собирает шесть исходных встроенных тел и проверяет их непустые префиксы имени и cardinality перед возвратом набора. SET ROLE не выполняется, полный SQL не разбирается и содержимое каталога не перечисляется.
func loadQueries() (querySet, error) {
	queries := querySet{
		sessionIdentity:         sessionIdentitySQL,
		issuerAssume:            issuerAssumeSQL,
		verifierAssume:          verifierAssumeSQL,
		publisherAssume:         publisherAssumeSQL,
		readbackAttestorAssume:  readbackAttestorAssumeSQL,
		restoreControllerAssume: restoreControllerAssumeSQL,
	}
	for _, definition := range []struct {
		name        string
		cardinality string
		body        string
	}{
		{"session__identity", "one", queries.sessionIdentity},
		{"issuer__assume", "exec", queries.issuerAssume},
		{"verifier__assume", "exec", queries.verifierAssume},
		{"publisher__assume", "exec", queries.publisherAssume},
		{"readback_attestor__assume", "exec", queries.readbackAttestorAssume},
		{"restore_controller__assume", "exec", queries.restoreControllerAssume},
	} {
		header := fmt.Sprintf("-- name: %s :%s", definition.name, definition.cardinality)
		if strings.TrimSpace(definition.body) == "" ||
			!strings.HasPrefix(strings.TrimSpace(definition.body), header) {
			return querySet{}, fmt.Errorf("invalid embedded query %s", definition.name)
		}
	}
	return queries, nil
}

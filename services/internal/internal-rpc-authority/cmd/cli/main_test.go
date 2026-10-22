package main

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// publicationFunctionDeclaration embeds the exact SQL publication-function fixture used to require one matching declaration in the baseline. It is comparison data, not a function executed by this unit suite.
// publicationFunctionDeclaration встраивает точный SQL-эталон функции публикации для требования одного совпадающего объявления в исходной миграции. Это данные сравнения, а не исполняемая этим юнит-набором функция.
//
//go:embed testdata/publisher_append_snapshot_history.sql
var publicationFunctionDeclaration string

// baselineMigration names the published baseline SQL fixture whose exact SHA-256 and expected authority boundaries are checked by these tests; it is not an editable replacement migration.
// baselineMigration задаёт опубликованный SQL-эталон исходной миграции, точный SHA-256 и ожидаемые границы полномочий которого проверяются тестами; это не изменяемая замена миграции.
const baselineMigration = "migrations/20260823000100_internal_rpc_authority_baseline.sql"

// workloadBoundaryMigration identifies the forward workload/database-boundary migration in the expected ordered history; the constant does not apply its SQL.
// workloadBoundaryMigration определяет прямую миграцию границы workload и БД в ожидаемой упорядоченной истории; константа не применяет SQL.
const workloadBoundaryMigration = "migrations/20260906000100_workload_database_boundary.sql"

// snapshotWorkloadSignerMigration identifies the forward snapshot/workload-signer boundary migration in the immutable migration-history assertion.
// snapshotWorkloadSignerMigration определяет прямую миграцию границы снимка и подписанта workload в проверке неизменной истории миграций.
const snapshotWorkloadSignerMigration = "migrations/20260907000100_snapshot_workload_signer_boundary.sql"

// authorityRotationLifecycleMigration locates the forward rotation-lifecycle SQL fixture for bounded-transition and no-reset text checks.
// authorityRotationLifecycleMigration указывает прямой SQL-эталон жизненного цикла ротации для текстовых проверок ограниченных переходов и отсутствия сброса.
const authorityRotationLifecycleMigration = "migrations/20260909000100_authority_rotation_lifecycle.sql"

// authorityNormalRotationOperationMigration locates the forward normal-rotation SQL fixture with explicit distribution, switch and retirement phases; the tests read its bytes rather than run rotation.
// authorityNormalRotationOperationMigration указывает прямой SQL-эталон штатной ротации с явными фазами распространения, переключения и вывода; тесты читают его байты, а не запускают ротацию.
const authorityNormalRotationOperationMigration = "migrations/20260909000200_authority_normal_rotation_operation.sql"

// authoritySnapshotPredecessorMigration locates the forward predecessor-provenance SQL fixture used to assert exact cross-domain bindings.
// authoritySnapshotPredecessorMigration указывает прямой SQL-эталон происхождения предшественника для проверки точных междоменных привязок.
const authoritySnapshotPredecessorMigration = "migrations/20260909000300_authority_snapshot_predecessor.sql"

// TestParseCommandAcceptsFreshOnlyCommands accepts the forward up action and rejects four retired or unsupported command forms without loading configuration or opening a database.
// TestParseCommandAcceptsFreshOnlyCommands принимает прямое действие up и отклоняет четыре устаревшие или неподдерживаемые формы команды без загрузки конфигурации и открытия БД.
func TestParseCommandAcceptsFreshOnlyCommands(t *testing.T) {
	t.Parallel()

	action, err := parseCommand([]string{"up"})
	if err != nil {
		t.Fatalf("parse up command: %v", err)
	}
	if action != commandUp {
		t.Fatalf("unexpected up command: %q", action)
	}
	for _, arguments := range [][]string{{"migrate", "deploy"}, {"expand"}, {"contract"}, {"version"}} {
		if _, parseErr := parseCommand(arguments); parseErr == nil {
			t.Fatalf("retired migration command was accepted: %v", arguments)
		}
	}
}

// TestReadbackContentionMigrationUsesScopedAdvisoryLock checks baseline SQL text for the registered runtime principals, advisory transaction lock, idempotency parameter, row locking and receipt table. Substring presence is not a concurrent PostgreSQL execution proof.
// TestReadbackContentionMigrationUsesScopedAdvisoryLock проверяет в тексте исходного SQL зарегистрированных субъектов runtime, транзакционную advisory-блокировку, параметр идемпотентности, блокировку строки и таблицу квитанций. Наличие подстрок не доказывает конкурентное исполнение PostgreSQL.
func TestReadbackContentionMigrationUsesScopedAdvisoryLock(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(baselineMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(content)
	for _, required := range []string{
		"ira_restore_controller_g1",
		"ira_publisher_g4",
		"ira_readback_attestor_g4",
		"pg_advisory_xact_lock",
		"p_idempotency_key::text",
		"FOR UPDATE",
		"authority_readback_attestation_receipts",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration is missing concurrency invariant %q", required)
		}
	}
}

// TestPeerScopedReadbackMigrationUsesCompositeIdempotencyLookup requires peer-qualified idempotency predicates and lock-key composition in baseline SQL and rejects an unscoped lookup string; it does not execute the database function.
// TestPeerScopedReadbackMigrationUsesCompositeIdempotencyLookup требует в исходном SQL предикаты идемпотентности с peer и составление ключа блокировки и отклоняет строку поиска без области; функция БД не выполняется.
func TestPeerScopedReadbackMigrationUsesCompositeIdempotencyLookup(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(baselineMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(content)
	for _, required := range []string{
		"challenge_peer_spiffe_id text",
		"WHERE peer_spiffe_id = challenge_peer_spiffe_id",
		"AND idempotency_key = p_idempotency_key",
		"challenge_peer_spiffe_id || ':' || p_idempotency_key::text",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration is missing peer-scoped invariant %q", required)
		}
	}
	if strings.Contains(text, "WHERE idempotency_key = p_idempotency_key") {
		t.Fatal("migration restored an unscoped idempotency lookup")
	}
}

// TestSnapshotPredecessorMigrationKeepsExactCrossDomainProvenance checks predecessor SQL for exact revision/digest/publication-input and role predicates, including phase handling. This is source-contract coverage, not a live signed snapshot or database authorization check.
// TestSnapshotPredecessorMigrationKeepsExactCrossDomainProvenance проверяет SQL предшественника на точные предикаты ревизии, дайджеста, входа публикации и роли, включая обработку фазы. Это проверка исходного контракта, а не живого подписанного снимка или авторизации БД.
func TestSnapshotPredecessorMigrationKeepsExactCrossDomainProvenance(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(authoritySnapshotPredecessorMigration)
	if err != nil {
		t.Fatalf("read predecessor migration: %v", err)
	}
	text := string(content)
	for _, required := range []string{
		"history.source_revision = p_source_revision",
		"history.source_digest_sha256 = p_source_digest_sha256",
		"intent.registry_source_digest_sha256 = p_registry_digest_sha256",
		"phase_intent.publication_input_digest_sha256 = history.publication_input_digest_sha256",
		"operation.publication_input_digest_sha256 = history.publication_input_digest_sha256",
		"operation.snapshot_digest_sha256 = history.source_digest_sha256",
		"COALESCE(operation.phase, '')",
		"pg_has_role(session_user, 'internal_rpc_authority_publisher', 'MEMBER')",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("predecessor migration omits %q", required)
		}
	}
}

// TestSnapshotPromotionUsesExactRequiredReadbackTargets checks declared workload/role/generation arrays, cardinality and exact readback joins, and rejects the old dynamic-count query fragment. It reads migration text without promoting a snapshot.
// TestSnapshotPromotionUsesExactRequiredReadbackTargets проверяет объявленные массивы workload, роли и поколения, мощность и точные соединения readback и отклоняет фрагмент прежнего подсчёта динамических записей. Читается текст миграции без продвижения снимка.
func TestSnapshotPromotionUsesExactRequiredReadbackTargets(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(baselineMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(content)
	for _, required := range []string{
		`"p_expected_workload_ids" "text"[]`,
		`"p_expected_roles" "text"[]`,
		`"p_expected_workload_generations" bigint[]`,
		"pg_catalog.cardinality(p_expected_workload_ids)",
		"readback.workload_id = expected.workload_id",
		"readback.role = expected.role",
		"readback.workload_generation = expected.workload_generation",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("snapshot promotion is missing exact target invariant %q", required)
		}
	}
	if strings.Contains(
		text,
		"FROM internal_rpc_authority.authority_snapshot_readbacks\n"+
			"    WHERE source_revision = p_source_revision",
	) {
		t.Fatal("snapshot promotion still counts optional dynamic readbacks")
	}
}

// TestAuthorityMigrationHistoryPreservesPublishedBaseline requires the exact nine-file ordered forward history and the fixed SHA-256 of the published baseline. It catches byte drift without applying, rolling back or editing migrations.
// TestAuthorityMigrationHistoryPreservesPublishedBaseline требует точную упорядоченную историю девяти прямых файлов и фиксированный SHA-256 опубликованной исходной миграции. Изменение байтов обнаруживается без применения, отката или редактирования миграций.
func TestAuthorityMigrationHistoryPreservesPublishedBaseline(t *testing.T) {
	t.Parallel()

	entries, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	if len(entries) != 9 || entries[0] != baselineMigration || entries[1] != workloadBoundaryMigration || entries[2] != snapshotWorkloadSignerMigration || entries[3] != "migrations/20260908000100_authority_bounded_freshness.sql" || entries[4] != authorityRotationLifecycleMigration || entries[5] != authorityNormalRotationOperationMigration || entries[6] != authoritySnapshotPredecessorMigration || entries[7] != "migrations/20260912000100_authority_legacy_registry_provenance.sql" || entries[8] != "migrations/20260912000200_authority_legacy_proof_budget.sql" {
		t.Fatalf("unexpected forward migration set: %v", entries)
	}
	content, err := os.ReadFile(baselineMigration)
	if err != nil {
		t.Fatal("read published baseline")
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(content)); got != "d4c9ed792ae0e157247fd3e1b58d15f7bbff43bf38f202bf72e9201398be4e0a" {
		t.Fatal("published authority baseline bytes changed")
	}
}

// TestAuthorityNormalRotationOperationHasThreeImmutablePhases checks SQL text for DISTRIBUTE/SWITCH/RETIRE intents, immutable publication bindings, timing fields and locking, and forbids DROP TABLE/TRUNCATE strings. It does not run a rotation protocol.
// TestAuthorityNormalRotationOperationHasThreeImmutablePhases проверяет SQL-текст на намерения DISTRIBUTE/SWITCH/RETIRE, неизменные привязки публикации, временные поля и блокировки и запрещает строки DROP TABLE/TRUNCATE. Протокол ротации не запускается.
func TestAuthorityNormalRotationOperationHasThreeImmutablePhases(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(authorityNormalRotationOperationMigration)
	if err != nil {
		t.Fatal("read authority normal rotation operation migration")
	}
	text := string(content)
	for _, required := range []string{
		"authority_rotation_operation_phase_intents",
		"authority_rotation_operation_publications",
		"'DISTRIBUTE', 'SWITCH', 'RETIRE'",
		"switch_not_before",
		"previous_not_after",
		"pg_advisory_xact_lock(1390, 1428)",
		"registry_digest_sha256",
		"publication_input_digest_sha256",
		"snapshot_digest_sha256",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("normal rotation operation migration is missing %q", required)
		}
	}
	if strings.Contains(text, "DROP TABLE") || strings.Contains(text, "TRUNCATE") {
		t.Fatal("normal rotation operation migration contains destructive state reset")
	}
}

// TestAuthorityRotationLifecycleMigrationIsForwardOnlyAndBounded requires lifecycle functions, version two, bounded overlap and terminal/abort predicates in SQL and rejects destructive table-reset text; it is not an executed lifecycle transition matrix.
// TestAuthorityRotationLifecycleMigrationIsForwardOnlyAndBounded требует в SQL функции жизненного цикла, вторую версию, ограниченное перекрытие и terminal/abort-предикаты и отклоняет текст разрушительного сброса таблиц; это не исполняемая матрица переходов.
func TestAuthorityRotationLifecycleMigrationIsForwardOnlyAndBounded(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(authorityRotationLifecycleMigration)
	if err != nil {
		t.Fatal("read authority rotation lifecycle migration")
	}
	text := string(content)
	for _, required := range []string{
		"publisher_prepare_rotation",
		"publisher_begin_rotation_delivery",
		"publisher_mark_rotation_delivered",
		"publisher_abort_rotation",
		"protocol_version = 2",
		"interval '40 seconds'",
		"predecessor.overlap_until > pg_catalog.clock_timestamp()",
		"status = 'RETIRED'",
		"status = 'ABORTED'",
		"intent.status <> 'DELIVERING'",
		"intent.status NOT IN ('DELIVERED', 'PROMOTED')",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("authority rotation migration is missing %q", required)
		}
	}
	if strings.Contains(text, "DROP TABLE") || strings.Contains(text, "TRUNCATE") {
		t.Fatal("authority rotation migration contains destructive state reset")
	}
}

// TestBaselineWrapsEveryFunctionForGoose counts CREATE FUNCTION declarations and their Goose StatementBegin/StatementEnd wrappers, requiring nonzero equal counts. The regex supports the fixture's dollar delimiters; it is not a general SQL parser.
// TestBaselineWrapsEveryFunctionForGoose считает объявления CREATE FUNCTION и их обёртки Goose StatementBegin/StatementEnd и требует ненулевое равенство. Регулярное выражение поддерживает dollar-разделители эталона; это не общий SQL-парсер.
func TestBaselineWrapsEveryFunctionForGoose(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(baselineMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(content)
	createFunctionPrefix := "CREATE " + "FUNCTION "
	functionCount := strings.Count(text, createFunctionPrefix)
	wrappedFunctionPattern := regexp.MustCompile(
		`(?s)-- \+goose StatementBegin\s+` +
			regexp.QuoteMeta(createFunctionPrefix) +
			`.*?(?:\$_\$|\$\$);\s+-- \+goose StatementEnd`,
	)
	wrappedFunctionCount := len(wrappedFunctionPattern.FindAllString(text, -1))
	if functionCount == 0 || wrappedFunctionCount != functionCount {
		t.Fatalf(
			"Goose function boundaries = %d, want %d",
			wrappedFunctionCount,
			functionCount,
		)
	}
}

// TestBaselineMaterializesCurrentWorkloadPrincipals requires current principal names, forbids retired identity-lifecycle names and requires exactly one publication-function fixture plus its timestamp signature in baseline bytes. It does not create roles or verify a real session_user.
// TestBaselineMaterializesCurrentWorkloadPrincipals требует в исходных байтах текущие имена субъектов, запрещает устаревшие имена жизненного цикла идентичности и требует ровно один эталон функции публикации и её временную сигнатуру. Роли не создаются и реальный session_user не проверяется.
func TestBaselineMaterializesCurrentWorkloadPrincipals(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(baselineMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(content)
	for _, required := range []string{
		"ira_role_image_builder_issuer_g1",
		"ira_image_admission_issuer_g1",
		"ira_image_promotion_issuer_g1",
		"ira_automation_scheduler_issuer_g1",
		"ira_secret_broker_issuer_g1",
		"ira_control_plane_issuer_g1",
		"ira_secret_broker_verifier_g1",
		"ira_control_api_gateway_issuer_g1",
		"ira_control_plane_verifier_g1",
		"ira_control_plane_resolver_g1",
		"ira_integration_gateway_issuer_g1",
		"ira_interaction_gateway_issuer_g1",
		"ira_runtime_controller_issuer_g1",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("baseline is missing current workload principal %q", required)
		}
	}
	for _, retired := range []string{
		"ira_database_credential_reconciler",
		"ira_publisher_g3",
		"ira_readback_attestor_g3",
		"reconcile_runtime_database_identity",
		"retire_runtime_database_identity",
	} {
		if strings.Contains(text, retired) {
			t.Fatalf("baseline still contains retired credential lifecycle %q", retired)
		}
	}
	if strings.Count(text, strings.TrimSpace(publicationFunctionDeclaration)) != 1 ||
		!strings.Contains(text, `"p_published_at" timestamp with time zone) RETURNS boolean`) {
		t.Fatal("baseline does not contain exactly one current snapshot publication function")
	}
}

// TestBaselineGrantsDatabaseConnectToExactRuntimePrincipals isolates the baseline's first database CONNECT grant, requires all listed runtime principals and forbids PUBLIC and owner role there. It is a bounded source assertion, not a database connection attempt.
// TestBaselineGrantsDatabaseConnectToExactRuntimePrincipals выделяет первое назначение CONNECT к БД в исходной миграции, требует всех перечисленных субъектов runtime и запрещает там PUBLIC и роль владельца. Это ограниченная проверка исходника, а не попытка подключения к БД.
func TestBaselineGrantsDatabaseConnectToExactRuntimePrincipals(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(baselineMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := string(content)
	connectStart := strings.Index(text, "GRANT CONNECT ON DATABASE internal_rpc_authority")
	if connectStart < 0 {
		t.Fatal("baseline does not grant database CONNECT to runtime principals")
	}
	connectEnd := strings.Index(text[connectStart:], ";")
	if connectEnd < 0 {
		t.Fatal("database CONNECT grant is incomplete")
	}
	grant := text[connectStart : connectStart+connectEnd]
	for _, principal := range []string{
		"ira_restore_controller_g1",
		"ira_publisher_g4",
		"ira_readback_attestor_g4",
		"ira_role_image_builder_issuer_g1",
		"ira_image_admission_issuer_g1",
		"ira_image_promotion_issuer_g1",
		"ira_automation_scheduler_issuer_g1",
		"ira_secret_broker_issuer_g1",
		"ira_control_plane_issuer_g1",
		"ira_secret_broker_verifier_g1",
		"ira_control_api_gateway_issuer_g1",
		"ira_control_plane_verifier_g1",
		"ira_control_plane_resolver_g1",
		"ira_integration_gateway_issuer_g1",
		"ira_interaction_gateway_issuer_g1",
		"ira_runtime_controller_issuer_g1",
	} {
		if !strings.Contains(grant, principal) {
			t.Fatalf("database CONNECT grant misses %q", principal)
		}
	}
	if strings.Contains(grant, "PUBLIC") || strings.Contains(grant, "internal_rpc_authority_owner") {
		t.Fatal("database CONNECT grant widened beyond exact runtime principals")
	}
}

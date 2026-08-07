// Package postgres реализует два узких PostgreSQL adapter migration job.
package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/inventory"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql
var queryFiles embed.FS

type Repository struct {
	pool *pgxpool.Pool
}

type ConnectionConfig struct {
	DSN           string
	TLSServerName string
	CAFile        string
	RequiredRole  string
}

func Open(ctx context.Context, input ConnectionConfig) (*Repository, error) {
	if err := validateQueries(); err != nil {
		return nil, err
	}
	config, err := securePoolConfig(input)
	if err != nil {
		return nil, err
	}
	config.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("open database pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("check database readiness")
	}
	var encrypted bool
	var version, cipherName string
	if err := pool.QueryRow(ctx, mustQuery("transport__readback.sql"), pgx.StrictNamedArgs{}).
		Scan(&encrypted, &version, &cipherName); err != nil || !encrypted || version != "TLSv1.3" || cipherName == "" {
		pool.Close()
		return nil, errors.New("database TLS readback is invalid")
	}
	var sameIdentity, safeRole, requiredMember bool
	if err := pool.QueryRow(ctx, mustQuery("principal__readback.sql"),
		pgx.StrictNamedArgs{"required_role": input.RequiredRole}).Scan(&sameIdentity, &safeRole, &requiredMember); err != nil ||
		!sameIdentity || !safeRole || !requiredMember {
		pool.Close()
		return nil, errors.New("database principal readback is invalid")
	}
	return &Repository{pool: pool}, nil
}

func validateQueries() error {
	entries, err := queryFiles.ReadDir("sql")
	if err != nil {
		return errors.New("read named SQL contracts")
	}
	positional := regexp.MustCompile(`\$[0-9]+`)
	header := regexp.MustCompile(`^-- name: ([a-z0-9_]+) :(one|many|exec)\n`)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			return errors.New("named SQL corpus is invalid")
		}
		content, readErr := queryFiles.ReadFile("sql/" + entry.Name())
		contract := header.FindSubmatch(content)
		if readErr != nil || len(contract) != 3 || string(contract[1]) != strings.TrimSuffix(entry.Name(), ".sql") ||
			positional.Match(content) || strings.Count(string(content), ";") != 1 {
			return errors.New("named SQL contract is invalid")
		}
	}
	return nil
}

func securePoolConfig(input ConnectionConfig) (*pgxpool.Config, error) {
	if input.DSN == "" || input.TLSServerName == "" || !filepath.IsAbs(input.CAFile) ||
		filepath.Clean(input.CAFile) != input.CAFile || strings.ContainsAny(input.TLSServerName, "*/") {
		return nil, errors.New("database TLS configuration is invalid")
	}
	parsed, err := url.Parse(input.DSN)
	if err != nil || parsed == nil {
		return nil, errors.New("database DSN must use sslmode=verify-full")
	}
	query := parsed.Query()
	if (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" ||
		len(query["sslmode"]) != 1 || query.Get("sslmode") != "verify-full" {
		return nil, errors.New("database DSN must use sslmode=verify-full")
	}
	for key, values := range query {
		if (key != "sslmode" && key != "sslrootcert" && key != "connect_timeout" && key != "application_name") ||
			len(values) != 1 {
			return nil, errors.New("database DSN contains unsupported connection parameters")
		}
	}
	host := parsed.Hostname()
	if host != input.TLSServerName || net.ParseIP(host) != nil {
		return nil, errors.New("database host and exact TLS server name mismatch")
	}
	if configuredCA := query.Get("sslrootcert"); configuredCA != "" && configuredCA != input.CAFile {
		return nil, errors.New("database DSN CA path mismatch")
	}
	caFile, err := os.Open(input.CAFile)
	if err != nil {
		return nil, errors.New("read database CA")
	}
	caPEM, readErr := io.ReadAll(io.LimitReader(caFile, 1024*1024+1))
	closeErr := caFile.Close()
	if readErr != nil || closeErr != nil || len(caPEM) > 1024*1024 {
		return nil, errors.New("read database CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("database CA is invalid")
	}
	config, err := pgxpool.ParseConfig(input.DSN)
	if err != nil {
		return nil, errors.New("parse database configuration")
	}
	if config.ConnConfig.Host != input.TLSServerName {
		return nil, errors.New("parsed database host and exact TLS server name mismatch")
	}
	config.ConnConfig.TLSConfig = &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		ServerName: input.TLSServerName, RootCAs: roots, InsecureSkipVerify: false,
	}
	config.ConnConfig.Fallbacks = nil
	return config, nil
}

func (repository *Repository) Close() { repository.pool.Close() }

func (repository *Repository) VerifyEmptyRestoreTarget(ctx context.Context) error {
	var database string
	var objects uint64
	if err := repository.pool.QueryRow(ctx, mustQuery("restore_target__empty.sql"), pgx.StrictNamedArgs{}).
		Scan(&database, &objects); err != nil {
		return errors.New("inspect restore verification database")
	}
	matched, err := regexp.MatchString(`^mattercodex_restore_[a-f0-9]{12,32}$`, database)
	if err != nil || !matched || objects != 0 {
		return errors.New("restore verification database must be isolated and empty")
	}
	return nil
}

type Snapshot struct {
	Tx           pgx.Tx
	Rows         []model.SnapshotRow
	ExportedID   string
	SourceSHA256 string
	Counts       map[string]uint64
}

type TargetSnapshot struct {
	Tx        pgx.Tx
	Resources []model.TargetResource
}

func (repository *Repository) BeginSourceSnapshot(ctx context.Context, export, lock bool) (Snapshot, error) {
	return repository.beginSnapshot(ctx, export, lock, "source_snapshot__rows.sql")
}

// BeginRestoredSnapshot читает exact restored corpus без зависимости от
// source SECURITY DEFINER functions, которые намеренно не входят в archive.
func (repository *Repository) BeginRestoredSnapshot(ctx context.Context) (Snapshot, error) {
	return repository.beginSnapshot(ctx, false, false, "restore_snapshot__rows.sql")
}

func (repository *Repository) beginSnapshot(ctx context.Context, export, lock bool, rowsQuery string) (Snapshot, error) {
	isolation := pgx.RepeatableRead
	if lock {
		// В READ COMMITTED snapshot следующего statement создаётся уже после
		// ожидания SECURITY DEFINER table locks; сами locks затем держат state.
		isolation = pgx.ReadCommitted
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: isolation,
	})
	if err != nil {
		return Snapshot{}, errors.New("begin source snapshot")
	}
	fail := func(cause error) (Snapshot, error) {
		_ = tx.Rollback(ctx)
		return Snapshot{}, cause
	}
	if lock {
		if err := tx.QueryRow(ctx, mustQuery("source_snapshot__lock.sql"), pgx.StrictNamedArgs{}).Scan(new(any)); err != nil {
			return fail(errors.New("lock source business tables"))
		}
	}
	exportedID := ""
	if export {
		if err := tx.QueryRow(ctx, mustQuery("source_snapshot__export.sql"), pgx.StrictNamedArgs{}).Scan(&exportedID); err != nil {
			return fail(errors.New("export source snapshot"))
		}
	}
	rows, err := tx.Query(ctx, mustQuery(rowsQuery), pgx.StrictNamedArgs{})
	if err != nil {
		return fail(errors.New("read source snapshot"))
	}
	defer rows.Close()
	result := make([]model.SnapshotRow, 0, 1024)
	hash := sha256.New()
	counts := make(map[string]uint64)
	lastTable := ""
	var lastPayload []byte
	for rows.Next() {
		var table string
		var payload *string
		if err := rows.Scan(&table, &payload); err != nil {
			return fail(errors.New("scan source snapshot"))
		}
		var raw []byte
		if payload != nil {
			raw = []byte(*payload)
		}
		if !validSourceTableName(table) || table < lastTable {
			return fail(errors.New("source snapshot table order is invalid"))
		}
		if table != lastTable {
			if len(raw) != 0 {
				return fail(errors.New("source snapshot table sentinel is missing"))
			}
			lastPayload = nil
			result = append(result, model.SnapshotRow{Table: table})
		} else if len(raw) == 0 || lastPayload != nil && bytes.Compare(raw, lastPayload) < 0 {
			return fail(errors.New("source snapshot row order is invalid"))
		}
		lastTable = table
		writeFramed(hash, []byte(table))
		if _, exists := counts[table]; !exists {
			counts[table] = 0
		}
		if len(raw) == 0 {
			continue
		}
		writeFramed(hash, raw)
		lastPayload = raw
		counts[table]++
		projected, retained, projectionErr := safeSourceProjection(table, raw)
		if projectionErr != nil {
			return fail(projectionErr)
		}
		if retained {
			result = append(result, model.SnapshotRow{Table: table, Payload: projected})
		}
	}
	if err := rows.Err(); err != nil {
		return fail(errors.New("stream source snapshot"))
	}
	if len(counts) == 0 {
		return fail(errors.New("source snapshot inventory is empty"))
	}
	return Snapshot{Tx: tx, Rows: result, ExportedID: exportedID,
		SourceSHA256: hex.EncodeToString(hash.Sum(nil)), Counts: counts}, nil
}

var sourceProjectionKeys = map[string][]string{
	"matter_codex_projects": {"id", "name", "slug", "description", "mattermost_team_id",
		"github_account_name", "created_at", "updated_at"},
	"matter_codex_chats": {"id", "project_id", "mattermost_channel_id", "name", "slug", "status",
		"chat_type", "system_purpose", "work_policy", "created_at", "updated_at"},
	"matter_codex_agent_roles": {"id", "project_id", "name", "description", "role_type",
		"github_account_name", "openai_account_name", "bot_identity", "prompt_mode", "enabled",
		"created_at", "updated_at"},
	"matter_codex_agent_sessions": {"id", "project_id", "chat_id", "role_id", "session_key", "status",
		"active_turn_id", "active_run_id", "binding_version", "openai_account_name"},
	"matter_codex_agent_session_turns": {"id", "session_id", "run_id", "status", "binding_version",
		"mattermost_channel_id", "mattermost_root_post_id", "mattermost_post_id", "user_id", "created_at",
		"updated_at", "artifacts"},
	"matter_codex_agent_runs":             {"id", "run_id", "flow_id", "status"},
	"matter_codex_agent_profiles":         {"id", "name", "openai_account_name", "github_account_name", "enabled"},
	"matter_codex_agent_prompt_templates": {"id", "profile_name", "template_key"},
	"matter_codex_audit_events":           {"id"},
	"matter_codex_process_runs": {"id", "public_id", "project_id", "policy_revision_id", "root_role_id",
		"root_initiator_user_id", "root_trigger_post_id", "root_channel_id", "root_thread_post_id", "status"},
	"matter_codex_process_turns": {"process_run_id", "turn_id", "parent_turn_id", "launch_post_id"},
	"matter_codex_runtime_agent_binding_outbox": {"id", "state", "agent_session_id", "agent_session_key",
		"agent_session_version", "agent_session_turn_id", "agent_run_id", "agent_session_turn_version",
		"control_session_id", "control_session_version", "control_turn_id", "control_turn_version", "attempt",
		"input_sha256", "runtime_revision_id", "runtime_revision_version", "runtime_revision_sha256",
		"agent_session_binding_sha256", "agent_turn_binding_sha256"},
	"matter_codex_runtime_agent_binding_discoveries": {"id", "state", "agent_session_turn_id"},
	"matter_codex_mattermost_bot_identities": {"id", "project_id", "role_id", "username",
		"mattermost_user_id", "status"},
	"matter_codex_chat_participants":            {"id", "chat_id", "role_id", "enabled"},
	"matter_codex_project_repositories":         {"id", "project_id", "repository_id"},
	"matter_codex_chat_repositories":            {"id", "chat_id", "repository_id"},
	"matter_codex_repositories":                 {"id", "github_account_name", "status"},
	"matter_codex_credentials":                  {"id", "status"},
	"matter_codex_openai_accounts":              {"id", "name", "credential_id", "status"},
	"matter_codex_github_accounts":              {"id", "name", "credential_id", "status"},
	"matter_codex_agent_flows":                  {"id", "flow_id", "status"},
	"matter_codex_project_runtime_variables":    {"id", "project_id"},
	"matter_codex_agent_role_runtime_variables": {"id", "role_id", "variable_id"},
	"matter_codex_policy_revisions":             {"id", "project_id", "version", "status", "created_at", "activated_at"},
	"matter_codex_role_capabilities":            {"id", "policy_revision_id", "role_id", "capability", "enabled"},
	"matter_codex_role_relationship_policies": {"id", "policy_revision_id", "source_role_id", "action",
		"target_role_id", "enabled"},
	"matter_codex_work_claims":              {"id", "process_run_id", "turn_id", "role_id", "status"},
	"matter_codex_owner_attention_requests": {"id", "process_run_id", "turn_id", "status"},
	"matter_codex_agent_delegations": {"id", "project_id", "source_session_id", "source_turn_id", "target_chat_id",
		"target_role_id", "target_session_id", "target_turn_id", "target_run_id", "status", "callback_turn_id", "callback_run_id"},
	"matter_codex_agent_delegation_callback_deliveries":         {"id", "delegation_id", "callback_run_id", "destination", "status"},
	"matter_codex_agent_delegation_callback_delivery_manifests": {"delegation_id", "callback_run_id", "expected_count"},
	"matter_codex_thread_contexts":                              {"id", "project_id", "chat_id", "status"},
	"matter_codex_memory_records":                               {"id", "project_id", "scope", "role_id", "created_by_role_id", "source_turn_id", "status"},
	"matter_codex_memory_record_versions":                       {"id", "record_id", "version", "content_hash", "supersedes_version_id"},
	"matter_codex_memory_embeddings":                            {"version_id", "model_revision", "dimensions"},
	"matter_codex_interaction_capabilities":                     {"status"},
	"matter_codex_automation_schedules": {"id", "public_id", "project_id", "target_agent_role_id", "target_chat_id",
		"preset", "local_time", "time_zone", "enabled", "next_run_at", "playbook_key", "prompt_version",
		"prompt_sha256", "callback_contract_version", "command_hash", "created_at", "updated_at"},
	"matter_codex_schedule_occurrences": {"id", "schedule_id", "project_id", "status"},
	"matter_codex_scheduled_runs": {"id", "occurrence_id", "schedule_id", "project_id", "target_agent_role_id",
		"target_chat_id", "runtime_session_id", "runtime_turn_id", "runtime_run_id", "status"},
	"matter_codex_automation_audit_events":                 {"id", "project_id", "schedule_id", "scheduled_run_id"},
	"matter_codex_cluster_admin_subjects":                  {"subject_type", "subject_key", "project_id", "profile_name"},
	"matter_codex_cluster_admin_revocations":               {"resource_type", "resource_key"},
	"matter_codex_cluster_admin_bindings":                  {"role_id", "project_id", "chat_id"},
	"matter_codex_cluster_admin_session_bindings":          {"role_id", "project_id", "chat_id", "session_key"},
	"matter_codex_cluster_admin_bot_bindings":              {"role_id", "project_id"},
	"matter_codex_cluster_admin_runtime_variable_bindings": {"role_id", "variable_id"},
	"matter_codex_cluster_admin_prompt_templates":          {"profile_name", "template_key"},
	"matter_codex_cluster_admin_dependencies":              {"role_id", "resource_type", "resource_key"},
	"matter_codex_cluster_admin_delivery_fences":           {"session_key"},
}

func safeSourceProjection(table string, raw []byte) ([]byte, bool, error) {
	keys, retained := sourceProjectionKeys[table]
	if !retained {
		return nil, false, nil
	}
	var source map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&source); err != nil {
		return nil, false, errors.New("decode source inventory projection")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, false, errors.New("source inventory projection has trailing data")
	}
	projected := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, exists := source[key]; exists {
			projected[key] = value
		}
	}
	if table == "matter_codex_agent_session_turns" {
		switch artifacts := source["artifacts"].(type) {
		case map[string]any:
			projected["artifacts"] = len(artifacts)
		case []any:
			projected["artifacts"] = len(artifacts)
		default:
			projected["artifacts"] = 0
		}
	}
	if table == "matter_codex_agent_roles" {
		if prompt, ok := source["prompt_template"].(string); ok && strings.TrimSpace(prompt) != "" {
			digest := sha256.Sum256([]byte(prompt))
			projected["prompt_sha256"] = hex.EncodeToString(digest[:])
		}
	}
	if table == "matter_codex_automation_schedules" {
		for _, key := range []string{"prompt_sha256", "command_hash"} {
			if encoded, ok := projected[key].(string); ok {
				projected[key] = strings.TrimPrefix(encoded, `\x`)
			}
		}
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		return nil, false, errors.New("encode source inventory projection")
	}
	return encoded, true, nil
}

func validSourceTableName(value string) bool {
	return inventory.Contains(value)
}

func writeFramed(hash interface{ Write([]byte) (int, error) }, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write(value)
}

func (repository *Repository) TargetResources(ctx context.Context) ([]model.TargetResource, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, errors.New("begin target inventory snapshot")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	resources, err := targetResources(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errors.New("close target inventory snapshot")
	}
	return resources, nil
}

func (repository *Repository) BeginTargetSnapshot(ctx context.Context) (TargetSnapshot, error) {
	// Первый SELECT только получает locks. READ COMMITTED гарантирует, что
	// inventory statements видят snapshot после ожидания concurrent writers.
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return TargetSnapshot{}, errors.New("begin target snapshot")
	}
	fail := func(cause error) (TargetSnapshot, error) {
		_ = tx.Rollback(ctx)
		return TargetSnapshot{}, cause
	}
	if err := tx.QueryRow(ctx, mustQuery("target_snapshot__lock.sql"), pgx.StrictNamedArgs{}).Scan(new(any)); err != nil {
		return fail(errors.New("lock target resources"))
	}
	resources, err := targetResources(ctx, tx)
	if err != nil {
		return fail(err)
	}
	return TargetSnapshot{Tx: tx, Resources: resources}, nil
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func targetResources(ctx context.Context, database queryer) ([]model.TargetResource, error) {
	result := make([]model.TargetResource, 0, 1024)
	for _, name := range []string{
		"target_resources__list.sql",
		"target_runtime_derived_resources__list.sql",
		"target_protected_history__list.sql",
		"target_turn_attempts__list.sql",
		"target_runtime_executions__list.sql",
	} {
		rows, err := database.Query(ctx, mustQuery(name), pgx.StrictNamedArgs{})
		if err != nil {
			return nil, errors.New("read target inventory")
		}
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				rows.Close()
				return nil, errors.New("scan target inventory")
			}
			var resource model.TargetResource
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.UseNumber()
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&resource); err != nil {
				rows.Close()
				return nil, errors.New("decode target inventory")
			}
			if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
				rows.Close()
				return nil, errors.New("target inventory has trailing data")
			}
			resource.Canonical = []byte(raw)
			switch resource.Kind {
			case "ARTIFACT":
				resource.ProjectionSHA256, err = artifactProjectionSHA(resource)
				if err != nil {
					rows.Close()
					return nil, errors.New("derive target artifact projection")
				}
			case "ROLE_IMAGE_RECIPE":
				resource.ProjectionSHA256, err = roleImageRecipeProjectionSHA(resource)
				if err != nil {
					rows.Close()
					return nil, errors.New("derive target role image recipe projection")
				}
			}
			result = append(result, resource)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, errors.New("stream target inventory")
		}
		rows.Close()
	}
	return result, nil
}

func artifactProjectionSHA(resource model.TargetResource) (string, error) {
	type artifactSpec struct {
		ArtifactKind       string    `json:"kind"`
		Direction          string    `json:"direction"`
		StorageRef         string    `json:"storageRef"`
		SizeBytes          uint64    `json:"sizeBytes"`
		MediaType          string    `json:"mediaType"`
		SHA256             string    `json:"sha256"`
		ScanStatus         string    `json:"scanStatus"`
		RetentionPolicyRef string    `json:"retentionPolicyRef"`
		ScanPolicyRevision uint64    `json:"scanPolicyRevision,omitempty"`
		ScanEvidenceSHA256 string    `json:"scanEvidenceSha256,omitempty"`
		ScannerWorkloadID  string    `json:"scannerWorkloadId,omitempty"`
		ScannedAt          time.Time `json:"scannedAt,omitempty"`
	}
	encodedSpec, err := json.Marshal(resource.Spec)
	if err != nil {
		return "", err
	}
	var spec artifactSpec
	decoder := json.NewDecoder(bytes.NewReader(encodedSpec))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil || spec.ArtifactKind == "" || spec.Direction == "" ||
		spec.StorageRef == "" || spec.SizeBytes == 0 || spec.MediaType == "" || !validSHA256(spec.SHA256) ||
		spec.ScanStatus != "CLEAN" || spec.RetentionPolicyRef == "" || spec.ScanPolicyRevision == 0 ||
		!validSHA256(spec.ScanEvidenceSHA256) || spec.ScannerWorkloadID == "" || spec.ScannedAt.IsZero() ||
		resource.Name == "" || resource.CreatedAt.IsZero() || resource.UpdatedAt.IsZero() {
		return "", errors.New("artifact projection is invalid")
	}
	projection, err := json.Marshal(struct {
		ID             string       `json:"id"`
		OrganizationID string       `json:"organizationId"`
		ProjectID      string       `json:"projectId"`
		ParentID       string       `json:"parentId,omitempty"`
		OwnerActorID   string       `json:"ownerActorId"`
		Kind           string       `json:"kind"`
		Name           string       `json:"name"`
		State          string       `json:"state"`
		Version        uint64       `json:"version"`
		Spec           artifactSpec `json:"spec"`
		CreatedAt      time.Time    `json:"createdAt"`
		UpdatedAt      time.Time    `json:"updatedAt"`
	}{resource.ID, resource.OrganizationID, resource.ProjectID, resource.ParentID,
		resource.OwnerActorID, resource.Kind, resource.Name, resource.State, resource.Version,
		spec, resource.CreatedAt, resource.UpdatedAt})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(projection)
	return hex.EncodeToString(digest[:]), nil
}

type roleImagePlatform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant,omitempty"`
}

type roleImagePackage struct {
	Manager   string `json:"manager"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Digest    string `json:"digest"`
	SourceRef string `json:"sourceRef"`
}

type roleImageTool struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	SourceRef string `json:"sourceRef"`
	SHA256    string `json:"sha256"`
}

type roleImageRecipeInput struct {
	BaseImageReference string              `json:"baseImageReference"`
	BaseImageDigest    string              `json:"baseImageDigest"`
	SourceRef          string              `json:"sourceRef"`
	SourceRevision     string              `json:"sourceRevision"`
	SourceSHA256       string              `json:"sourceSha256"`
	ContextRef         string              `json:"contextRef"`
	ContextSHA256      string              `json:"contextSha256"`
	BuilderSHA256      string              `json:"builderSha256"`
	FrontendSHA256     string              `json:"frontendSha256"`
	Platforms          []roleImagePlatform `json:"platforms"`
	Packages           []roleImagePackage  `json:"packages"`
	Tools              []roleImageTool     `json:"tools"`
	InstallationBlock  string              `json:"installationBlock"`
	ToolchainSHA256    string              `json:"toolchainSha256"`
}

type roleImageRecipeSpec struct {
	Input                       roleImageRecipeInput `json:"input"`
	Generation                  uint64               `json:"generation"`
	SpecSHA256                  string               `json:"specSha256"`
	PolicyRevision              uint64               `json:"policyRevision"`
	PolicySHA256                string               `json:"policySha256"`
	RoleRuntimeContractRevision uint64               `json:"roleRuntimeContractRevision"`
	RoleRuntimeContractSHA256   string               `json:"roleRuntimeContractSha256"`
}

func roleImageRecipeProjectionSHA(resource model.TargetResource) (string, error) {
	encodedSpec, err := json.Marshal(resource.Spec)
	if err != nil {
		return "", err
	}
	var spec roleImageRecipeSpec
	decoder := json.NewDecoder(bytes.NewReader(encodedSpec))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil || spec.Generation == 0 || !validSHA256(spec.SpecSHA256) ||
		spec.PolicyRevision == 0 || !validSHA256(spec.PolicySHA256) ||
		spec.RoleRuntimeContractRevision == 0 || !validSHA256(spec.RoleRuntimeContractSHA256) ||
		resource.Name == "" || resource.CreatedAt.IsZero() || resource.UpdatedAt.IsZero() {
		return "", errors.New("role image recipe projection is invalid")
	}
	projection, err := json.Marshal(struct {
		ID             string              `json:"id"`
		OrganizationID string              `json:"organizationId"`
		ProjectID      string              `json:"projectId"`
		ParentID       string              `json:"parentId,omitempty"`
		OwnerActorID   string              `json:"ownerActorId"`
		Kind           string              `json:"kind"`
		Name           string              `json:"name"`
		State          string              `json:"state"`
		Version        uint64              `json:"version"`
		Spec           roleImageRecipeSpec `json:"spec"`
		CreatedAt      time.Time           `json:"createdAt"`
		UpdatedAt      time.Time           `json:"updatedAt"`
	}{resource.ID, resource.OrganizationID, resource.ProjectID, resource.ParentID,
		resource.OwnerActorID, resource.Kind, resource.Name, resource.State, resource.Version,
		spec, resource.CreatedAt, resource.UpdatedAt})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(projection)
	return hex.EncodeToString(digest[:]), nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, symbol := range value {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}

func FreezeSourceSnapshot(ctx context.Context, tx pgx.Tx, receipt model.Receipt) error {
	return applyReceipt(ctx, tx, "source_cutover__freeze.sql", receipt, nil, "FROZEN", "COMMITTED")
}

func CommitTargetSnapshot(ctx context.Context, tx pgx.Tx, receipt model.Receipt) error {
	return applyReceipt(ctx, tx, "target_cutover__commit.sql", receipt, nil, "COMMITTED")
}

func (repository *Repository) PrepareSource(ctx context.Context, receipt model.Receipt) error {
	return repository.withReceiptTx(ctx, "source_cutover__prepare.sql", receipt, nil, "PREPARED")
}

func (repository *Repository) PrepareTarget(ctx context.Context, receipt model.Receipt, counts any,
	commands []model.MaterializationCommand,
) error {
	encoded, err := json.Marshal(counts)
	if err != nil {
		return errors.New("encode mapping counts")
	}
	materialization, err := json.Marshal(commands)
	if err != nil {
		return errors.New("encode materialization intent")
	}
	return repository.withReceiptTx(ctx, "target_cutover__prepare.sql", receipt, pgx.StrictNamedArgs{
		"mapping_counts": string(encoded), "materialization_plan": string(materialization),
	}, "PREPARED")
}

func (repository *Repository) MarkTargetRestoreVerified(ctx context.Context, receipt model.Receipt) error {
	if err := repository.withReceiptTx(ctx, "target_cutover__verify_restore.sql", receipt, nil, "PREPARED"); err != nil {
		return err
	}
	stored, err := repository.GetTargetReceipt(ctx, receipt.PlanID)
	if err != nil || !sameImmutableReceipt(receipt, stored) || stored.State != "PREPARED" || !stored.RestoreVerified {
		return errors.New("restore verification receipt readback mismatch")
	}
	return nil
}

func (repository *Repository) FreezeSource(ctx context.Context, receipt model.Receipt) error {
	return repository.withReceiptTx(ctx, "source_cutover__freeze.sql", receipt, nil, "FROZEN", "COMMITTED")
}

func (repository *Repository) CommitSource(ctx context.Context, receipt model.Receipt) error {
	return repository.withReceiptTx(ctx, "source_cutover__commit.sql", receipt, nil, "COMMITTED")
}

func (repository *Repository) CommitTarget(ctx context.Context, receipt model.Receipt) error {
	return repository.withReceiptTx(ctx, "target_cutover__commit.sql", receipt, nil, "COMMITTED")
}

func (repository *Repository) AbortSource(ctx context.Context, receipt model.Receipt) error {
	return repository.withReceiptTx(ctx, "source_cutover__abort.sql", receipt, nil, "ABORTED")
}

func (repository *Repository) AbortTarget(ctx context.Context, receipt model.Receipt) error {
	return repository.withReceiptTx(ctx, "target_cutover__abort.sql", receipt, nil, "ABORTED")
}

func (repository *Repository) GetSourceReceipt(ctx context.Context, planID string) (model.Receipt, error) {
	return repository.getReceipt(ctx, "source_cutover__get.sql", planID)
}

func (repository *Repository) GetTargetReceipt(ctx context.Context, planID string) (model.Receipt, error) {
	return repository.getReceipt(ctx, "target_cutover__get.sql", planID)
}

func (repository *Repository) FindSourceReceipt(ctx context.Context, planID string) (model.Receipt, bool, error) {
	return repository.findReceipt(ctx, "source_cutover__get.sql", planID)
}

func (repository *Repository) FindTargetReceipt(ctx context.Context, planID string) (model.Receipt, bool, error) {
	return repository.findReceipt(ctx, "target_cutover__get.sql", planID)
}

func (repository *Repository) findReceipt(ctx context.Context, name, planID string) (model.Receipt, bool, error) {
	receipt, err := repository.getReceipt(ctx, name, planID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Receipt{}, false, nil
	}
	return receipt, err == nil, err
}

func (repository *Repository) getReceipt(ctx context.Context, name, planID string) (model.Receipt, error) {
	return scanReceipt(repository.pool.QueryRow(ctx, mustQuery(name), pgx.StrictNamedArgs{"plan_id": planID}))
}

func (repository *Repository) withReceiptTx(
	ctx context.Context,
	name string,
	receipt model.Receipt,
	extra pgx.StrictNamedArgs,
	expectedStates ...string,
) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return errors.New("begin cutover transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tx.QueryRow(ctx, mustQuery("cutover__lock.sql"), pgx.StrictNamedArgs{}).Scan(new(any)); err != nil {
		return errors.New("acquire cutover fence")
	}
	if err := applyReceipt(ctx, tx, name, receipt, extra, expectedStates...); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("commit cutover transaction")
	}
	return nil
}

func applyReceipt(ctx context.Context, tx pgx.Tx, name string, receipt model.Receipt, extra pgx.StrictNamedArgs,
	expectedStates ...string,
) error {
	args := pgx.StrictNamedArgs{
		"plan_id": receipt.PlanID, "plan_sha256": receipt.PlanSHA256,
		"source_sha256": receipt.SourceSHA256, "target_sha256": receipt.TargetSHA256,
		"backup_sha256": receipt.BackupSHA256, "manifest_sha256": receipt.ManifestSHA256,
		"materialization_sha256": receipt.MaterializationSHA256,
		"materialization_count":  receipt.MaterializationCount,
	}
	for key, value := range extra {
		args[key] = value
	}
	stored, err := scanReceipt(tx.QueryRow(ctx, mustQuery(name), args))
	if err != nil {
		return fmt.Errorf("apply cutover receipt: %w", err)
	}
	if !sameImmutableReceipt(receipt, stored) || !contains(expectedStates, stored.State) {
		return errors.New("cutover receipt readback mismatch")
	}
	return nil
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func scanReceipt(row pgx.Row) (model.Receipt, error) {
	var receipt model.Receipt
	err := row.Scan(&receipt.PlanID, &receipt.PlanSHA256, &receipt.SourceSHA256, &receipt.TargetSHA256,
		&receipt.BackupSHA256, &receipt.ManifestSHA256, &receipt.MaterializationSHA256,
		&receipt.MaterializationCount, &receipt.State, &receipt.RestoreVerified)
	return receipt, err
}

func sameImmutableReceipt(left, right model.Receipt) bool {
	return left.PlanID == right.PlanID && left.PlanSHA256 == right.PlanSHA256 &&
		left.SourceSHA256 == right.SourceSHA256 && left.TargetSHA256 == right.TargetSHA256 &&
		left.BackupSHA256 == right.BackupSHA256 && left.ManifestSHA256 == right.ManifestSHA256 &&
		left.MaterializationSHA256 == right.MaterializationSHA256 &&
		left.MaterializationCount == right.MaterializationCount
}

func mustQuery(name string) string {
	content, err := queryFiles.ReadFile("sql/" + name)
	if err != nil {
		panic(err)
	}
	header := "-- name: " + strings.TrimSuffix(name, ".sql") + " "
	if !strings.HasPrefix(string(content), header) {
		panic("invalid named SQL contract: " + name)
	}
	return string(content)
}

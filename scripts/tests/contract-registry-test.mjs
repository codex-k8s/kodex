// Реестр активных контрактов не может ссылаться на отсутствующие исходники или generated code.
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const root = fileURLToPath(new URL('../../', import.meta.url));
const registry = JSON.parse(execFileSync('yq', ['-o=json', '.', `${root}contracts/registry.yaml`], { timeout: 10000 }));
const tracked = execFileSync('git', ['ls-files', '-z'], { cwd: root, timeout: 10000 }).toString().split('\0').filter(Boolean);

function checkPaths(packages, files) {
  const ids = new Set();
  for (const entry of packages) {
    assert.equal(typeof entry.id, 'string', 'PACKAGE_ID_REQUIRED');
    assert.equal(ids.has(entry.id), false, `DUPLICATE_PACKAGE: ${entry.id}`);
    ids.add(entry.id);
    for (const [kind, path] of [['source', entry.source], ...Object.entries(entry.generated ?? {})]) {
      assert.equal(typeof path, 'string', `CONTRACT_PATH_REQUIRED: ${entry.id}/${kind}`);
      assert.equal(path.split('/').every(part => part && part !== '.' && part !== '..'), true, `CONTRACT_PATH_INVALID: ${entry.id}/${kind}`);
      assert.equal(files.some(file => file === path || file.startsWith(`${path}/`)), true, `CONTRACT_PATH_MISSING: ${entry.id}/${kind}`);
    }
  }
}

test('active registry source and generated paths contain tracked files', () => {
  assert.equal(registry.version, 1);
  assert.ok(registry.packages.length > 0);
  checkPaths(registry.packages, tracked);
});

test('missing source and generated paths fail independently; adjacent names do not match', () => {
  const entry = { id: 'fixture-v1', source: 'contracts/fixture/v1', generated: { go: 'libs/go/fixture/gen' } };
  const files = ['contracts/fixture/v1/api.proto', 'libs/go/fixture/gen/api.pb.go'];
  checkPaths([entry], files);
  assert.throws(() => checkPaths([entry], files.slice(1)), /CONTRACT_PATH_MISSING: fixture-v1\/source/);
  assert.throws(() => checkPaths([entry], files.slice(0, 1)), /CONTRACT_PATH_MISSING: fixture-v1\/go/);
  assert.throws(() => checkPaths([entry], ['contracts/fixture/v10/api.proto', files[1]]), /CONTRACT_PATH_MISSING/);
  assert.throws(() => checkPaths([entry, entry], files), /DUPLICATE_PACKAGE/);
});

test('integration worker consumes existing CP, package and email contracts', () => {
  for (const id of ['control-plane-v1', 'integration-package-v1', 'email-bridge-api-v1']) {
    const entry = registry.packages.find(value => value.id === id);
    assert.ok(entry?.consumers.includes('integration-gateway'), `INTEGRATION_CONSUMER_MISSING: ${id}`);
  }
  assert.equal(registry.packages.some(entry => entry.format === 'proto' && entry.owner === 'integration-gateway'), false);
});

const riskPolicy = JSON.parse(execFileSync('node', ['-e', 'process.stdout.write(require("node:fs").readFileSync(process.argv[1],"utf8"))', `${root}deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json`], { timeout: 10000 }));
const riskAPI = JSON.parse(execFileSync('yq', ['-o=json', '.', `${root}contracts/openapi/control-api-gateway/v1/openapi.yaml`], { timeout: 10000 }));
const riskOperations = [
  ['platform.query.organization.role-images.vulnerability-report.get', 'getOrganizationImageVulnerabilityReport', 'get'],
  ['platform.query.role-images.vulnerability-report.get', 'getImageVulnerabilityReport', 'get'],
  ['platform.command.organization.role-images.risk.decide', 'decideOrganizationImageAdmissionRisk', 'post'],
  ['platform.command.role-images.risk.decide', 'decideImageAdmissionRisk', 'post'],
];

function checkRiskContract(api, policy) {
  // Canonical policygen revision включает PROJECT self-grant и required Workflow launch.
  assert.equal(policy.policy_revision, 92, 'RISK_POLICY_REVISION');
  for (const [permission, operation, method] of riskOperations) {
    const bindings = policy.policy.operation_bindings.filter(item => item.operation_id === permission);
    assert.equal(bindings.length, 1, 'RISK_METHOD_EXACT_CALLER');
    const binding = bindings[0];
    assert.equal(binding.caller_workload_id, 'control-api-gateway', 'RISK_HUMAN_GATEWAY');
    assert.equal(binding.authority_proof_producer_id, 'control-plane.oidc', 'RISK_OIDC_SOURCE');
    assert.equal(binding.permission, permission, 'RISK_DEDICATED_PERMISSION');
    assert.equal(binding.project_required, false, 'RISK_NO_PROJECT_AUTHORITY');
    assert.deepEqual(binding.authority_sources, ['OIDC_SESSION', 'DOMAIN_STATE'], 'RISK_NO_DELEGATION');
    assert.deepEqual(binding.request_profile, {
      mode: 'UNARY_PROTO_SHA256', resource: 'REQUIRED', version: method === 'post' ? 'REQUIRED' : 'FORBIDDEN',
      attempt: 'FORBIDDEN', idempotency: method === 'post' ? 'REQUIRED' : 'FORBIDDEN',
    }, 'RISK_REQUEST_PROFILE');
    const routes = Object.values(api.paths).map(path => path[method]).filter(value => value?.operationId === operation);
    assert.equal(routes.length, 1, 'RISK_OPERATION_UNIQUE');
    if (method === 'post') {
      for (const header of ['IfMatch', 'IdempotencyKey', 'CsrfToken']) {
        assert.ok(routes[0].parameters.some(value => value.$ref === '#/components/parameters/' + header), 'RISK_OWNER_HEADERS');
      }
    }
  }
  const schemas = api.components.schemas;
  assert.deepEqual(schemas.RoleImageArtifact.properties.admissionVerdict.enum, ['ACCEPTED', 'REJECTED', 'PENDING'], 'RISK_PENDING_PUBLIC_ONLY');
  assert.equal(schemas.ImageVulnerabilityFinding.properties.packageName.maxLength, 320, 'RISK_PACKAGE_BOUND');
  assert.equal(schemas.ImageVulnerabilityFinding.properties.ecosystem.maxLength, 320, 'RISK_ECOSYSTEM_BOUND');
  assert.deepEqual(schemas.ImageVulnerabilitySeverity.enum, ['CRITICAL', 'HIGH', 'MEDIUM', 'LOW', 'NEGLIGIBLE', 'UNKNOWN'], 'RISK_ALL_SEVERITIES');
  assert.equal(schemas.ImageVulnerabilityFinding.additionalProperties, false, 'RISK_FINDING_CLOSED');
  assert.ok(schemas.ImageVulnerabilityFinding.required.includes('occurrences'), 'RISK_GROUP_COUNTS');
  assert.ok(schemas.ImageVulnerabilityFinding.required.includes('ignored'), 'RISK_SUPPRESSED_PRESERVED');
  assert.equal(schemas.ImageVulnerabilityReportResponse.properties.findings.maxItems, 100, 'RISK_PAGE_BOUND');
  for (const name of ['matchCount', 'suppressedMatchCount', 'severityCounts', 'complete', 'projectionSha256']) {
    assert.ok(schemas.ImageVulnerabilityReport.required.includes(name), 'RISK_COMPLETE_REPORT_METADATA');
  }
  const input = schemas.ImageAdmissionRiskDecisionInput;
  assert.equal(input.additionalProperties, false, 'RISK_INPUT_CLOSED');
  for (const name of ['manifestDigest', 'vulnerabilityEvidenceSha256', 'projectionSha256',
    'priorAdmissionReceiptSha256', 'priorEvidenceManifestDigest', 'expectedArtifactVersion',
    'expectedAdmissionRevision', 'expectedRecipeGeneration', 'expectedBuildRef', 'expectedBuildAttempt',
    'policyRevision', 'policySha256', 'action', 'reason']) {
    assert.ok(input.required.includes(name), 'RISK_EXACT_OWNER_INTENT');
  }
  for (const name of ['owner', 'actor', 'approved', 'accepted', 'scannerIgnore', 'rawReport', 'rawUrl', 'locations']) {
    assert.equal(Object.hasOwn(input.properties, name), false, 'RISK_NO_CALLER_AUTHORITY_OR_RAW_DATA');
  }
  assert.equal(input.properties.reason.minLength, 1, 'RISK_REASON_REQUIRED');
  assert.equal(input.properties.reason.maxLength, 2048, 'RISK_REASON_BOUND');
  assert.deepEqual(schemas.ImageAdmissionRiskAction.enum, ['ACCEPT_RISK', 'REJECT_RISK'], 'RISK_TYPED_DECISION');
}

test('image risk contract has exact human-only methods and full safe report pages', () => {
  checkRiskContract(riskAPI, riskPolicy);
});

test('image risk contract rejects delegated caller, project authority, missing pins and hidden severity', () => {
  for (const mutate of [
    (api, policy) => { policy.policy_revision = 91; },
    (api, policy) => { policy.policy_revision = 93; },
    (api, policy) => { policy.policy.operation_bindings.find(value => value.operation_id === riskOperations[0][0]).caller_workload_id = 'agent-runner'; },
    (api, policy) => { policy.policy.operation_bindings.find(value => value.operation_id === riskOperations[2][0]).project_required = true; },
    (api) => { api.components.schemas.ImageVulnerabilitySeverity.enum.pop(); },
    (api) => { api.components.schemas.ImageAdmissionRiskDecisionInput.required = api.components.schemas.ImageAdmissionRiskDecisionInput.required.filter(value => value !== 'projectionSha256'); },
  ]) {
    const api = structuredClone(riskAPI), policy = structuredClone(riskPolicy);
    mutate(api, policy);
    assert.throws(() => checkRiskContract(api, policy), /RISK_/);
  }
});

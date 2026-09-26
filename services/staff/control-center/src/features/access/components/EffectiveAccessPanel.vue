<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import AccessScopeEditor from "@/features/access/components/AccessScopeEditor.vue";
import {
  accessRoleOptions,
  accessSubjectOptions,
} from "@/features/access/entity-pickers";
import {
  emptyScopeDraft,
  toAccessScope,
  validScope,
} from "@/features/access/model";
import {
  accessScopeKind,
  permissionMessage,
} from "@/features/access/presentation";
import type {
  AccessRole,
  AccessSubject,
  Agent,
  EffectiveAccessPage,
  ExplainAccessResult,
  IntegrationConnection,
  PermissionDefinition,
  Project,
  SimulateAccessResult,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

type Mode = "QUERY" | "EXPLAIN" | "SIMULATE";

const props = defineProps<{
  initialSubjectRef?: string;
  subjects: AccessSubject[];
  permissions: PermissionDefinition[];
  roles: AccessRole[];
  projects: Project[];
  agents: Agent[];
  workflows: Workflow[];
  integrations: IntegrationConnection[];
  effective?: EffectiveAccessPage;
  explanation?: ExplainAccessResult;
  simulation?: SimulateAccessResult;
  loading?: boolean;
  problem?: AppProblem;
}>();
const emit = defineEmits<{
  query: [
    input: {
      subjectRef: string;
      permissionKeys: string[];
      target: ReturnType<typeof toAccessScope>;
    },
  ];
  explain: [
    input: {
      subjectRef: string;
      permissionKey: string;
      target: ReturnType<typeof toAccessScope>;
    },
  ];
  simulate: [
    input: {
      subjectRef: string;
      permissionKey: string;
      target: ReturnType<typeof toAccessScope>;
      role: {
        permissionKeys: string[];
        allowedScopes: AccessRole["currentVersion"]["allowedScopes"];
      };
      binding: {
        subjectKind: AccessSubject["kind"];
        subjectRef: string;
        scope: ReturnType<typeof toAccessScope>;
        conditions: { requireOwner: boolean };
      };
    },
  ];
  "load-project-resources": [projectRef: string];
  clear: [];
}>();
const i18n = useI18n();
const permissionMessages = computed(() =>
  i18n.tm("access.permissionsRegistry"),
);

const mode = ref<Mode>("QUERY");
const form = reactive({
  subjectRef: "",
  permissionKey: "",
  roleRef: "",
  scope: emptyScopeDraft(),
});
watch(
  () => props.initialSubjectRef,
  (ref) => {
    if (ref) form.subjectRef = ref;
  },
  { immediate: true },
);
const subjectRows = new Map<string, AccessSubject>();
const roleRows = new Map<string, AccessRole>();
const selectedSubject = computed(
  () =>
    subjectRows.get(form.subjectRef) ??
    props.subjects.find((subject) => subject.ref === form.subjectRef),
);
const selectedRole = computed(
  () =>
    roleRows.get(form.roleRef) ??
    props.roles.find((role) => role.ref === form.roleRef),
);
const selectedSubjectOption = computed<AsyncEntityOption | undefined>(() =>
  selectedSubject.value
    ? {
        ref: selectedSubject.value.ref,
        title: selectedSubject.value.displayName,
      }
    : undefined,
);
const selectedRoleOption = computed<AsyncEntityOption | undefined>(() =>
  selectedRole.value
    ? {
        ref: selectedRole.value.ref,
        title: selectedRole.value.currentVersion.name,
        description: selectedRole.value.currentVersion.description,
        meta: `v${String(selectedRole.value.currentVersion.revision)}`,
      }
    : undefined,
);
const selectedPermission = computed(() =>
  props.permissions.find((permission) => permission.key === form.permissionKey),
);
function permissionName(key: string): string {
  const name = permissionMessage(permissionMessages.value, key, "name");
  return name === key ? i18n.t("access.roleEditor.unknownPermission") : name;
}
function riskLabel(key: string): string {
  const risk = props.permissions.find(
    (permission) => permission.key === key,
  )?.risk;
  return risk
    ? i18n.t(`access.risk.${risk}`)
    : i18n.t("access.effective.unknownRisk");
}
const valid = computed(
  () =>
    Boolean(form.subjectRef) &&
    (mode.value === "QUERY"
      ? props.permissions.length > 0 && props.permissions.length <= 100
      : Boolean(form.permissionKey)) &&
    validScope(form.scope) &&
    (mode.value !== "SIMULATE" || Boolean(selectedRole.value)),
);
const decision = computed(() =>
  mode.value === "QUERY"
    ? props.effective?.items.find(
        (item) => item.permissionKey === form.permissionKey,
      )
    : mode.value === "EXPLAIN"
      ? props.explanation?.result
      : undefined,
);

function submit(): void {
  if (!valid.value || props.loading) return;
  const target = toAccessScope(form.scope);
  if (mode.value === "QUERY") {
    emit("query", {
      subjectRef: form.subjectRef,
      permissionKeys: props.permissions.map((permission) => permission.key),
      target,
    });
    return;
  }
  if (mode.value === "EXPLAIN") {
    emit("explain", {
      subjectRef: form.subjectRef,
      permissionKey: form.permissionKey,
      target,
    });
    return;
  }
  if (!selectedSubject.value || !selectedRole.value) return;
  emit("simulate", {
    subjectRef: form.subjectRef,
    permissionKey: form.permissionKey,
    target,
    role: {
      permissionKeys: selectedRole.value.currentVersion.permissionKeys,
      allowedScopes: selectedRole.value.currentVersion.allowedScopes,
    },
    binding: {
      subjectKind: selectedSubject.value.kind,
      subjectRef: form.subjectRef,
      scope: target,
      conditions: { requireOwner: false },
    },
  });
}

function selection(value: string | null | readonly string[]): string {
  return typeof value === "string" ? value : "";
}

function pickerLabels(label: string, searchPlaceholder: string) {
  return {
    label,
    searchPlaceholder,
    loading: i18n.t("common.loading"),
    loadingMore: i18n.t("common.loading"),
    empty: i18n.t("common.empty"),
    error: i18n.t("errors.default"),
    retry: i18n.t("common.retry"),
  };
}

function loadSubjects(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize?: number,
): Promise<AsyncEntityOptionPage> {
  return accessSubjectOptions(
    undefined,
    query,
    cursor,
    signal,
    pageSize,
    (items) => items.forEach((item) => subjectRows.set(item.ref, item)),
  );
}

function loadRoles(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize?: number,
): Promise<AsyncEntityOptionPage> {
  return accessRoleOptions(query, cursor, signal, pageSize, (items) =>
    items.forEach((item) => roleRows.set(item.ref, item)),
  );
}

watch(mode, () => emit("clear"));
watch(
  () => [
    form.subjectRef,
    form.scope.kind,
    form.scope.projectRef,
    form.scope.resourceKind,
    form.scope.resourceRef,
    mode.value === "QUERY" ? "" : form.permissionKey,
    mode.value === "SIMULATE" ? form.roleRef : "",
  ],
  () => emit("clear"),
);
watch(
  () => props.effective,
  (page) => {
    if (mode.value !== "QUERY" || !page?.items.length) return;
    if (!page.items.some((item) => item.permissionKey === form.permissionKey))
      form.permissionKey = page.items[0]?.permissionKey ?? "";
  },
);
</script>

<template>
  <section>
    <header class="effective-header">
      <div>
        <h2>{{ $t("access.effective.title") }}</h2>
        <p>{{ $t("access.effective.subtitle") }}</p>
      </div>
      <div
        class="mode-switch"
        role="group"
        :aria-label="$t('access.effective.mode')"
      >
        <button
          v-for="value in ['QUERY', 'EXPLAIN', 'SIMULATE'] as const"
          :key="value"
          type="button"
          :class="{ active: mode === value }"
          :aria-pressed="mode === value"
          @click="mode = value"
        >
          {{ $t(`access.effective.modes.${value}`) }}
        </button>
      </div>
    </header>

    <div
      class="effective-layout"
      :class="{ 'effective-layout--query': mode === 'QUERY' }"
    >
      <form class="effective-form panel" @submit.prevent="submit">
        <div class="field">
          <span>{{ $t("access.effective.subject") }}</span>
          <AsyncEntityPicker
            :model-value="form.subjectRef"
            :selected="selectedSubjectOption"
            :load-page="loadSubjects"
            :labels="
              pickerLabels(
                $t('access.effective.subject'),
                $t('access.effective.chooseSubject'),
              )
            "
            :placeholder="$t('access.effective.chooseSubject')"
            :trigger-label="$t('access.effective.subject')"
            :clearable="false"
            @update:model-value="form.subjectRef = selection($event)"
          />
        </div>
        <label v-if="mode !== 'QUERY'" class="field">
          <span>{{ $t("access.effective.permission") }}</span>
          <select
            v-model="form.permissionKey"
            name="access-effective-permission"
            required
          >
            <option value="" disabled>
              {{ $t("access.effective.choosePermission") }}
            </option>
            <option
              v-for="permission in permissions"
              :key="permission.key"
              :value="permission.key"
            >
              {{
                permissionMessage(permissionMessages, permission.key, "name")
              }}
            </option>
          </select>
        </label>
        <p v-else class="query-hint">
          {{
            $t("access.effective.queryAllHint", { count: permissions.length })
          }}
        </p>
        <div v-if="mode === 'SIMULATE'" class="field">
          <span>{{ $t("access.effective.role") }}</span>
          <AsyncEntityPicker
            :model-value="form.roleRef"
            :selected="selectedRoleOption"
            :load-page="loadRoles"
            :labels="
              pickerLabels(
                $t('access.effective.role'),
                $t('access.effective.chooseRole'),
              )
            "
            :placeholder="$t('access.effective.chooseRole')"
            :trigger-label="$t('access.effective.role')"
            :clearable="false"
            @update:model-value="form.roleRef = selection($event)"
          />
        </div>
        <AccessScopeEditor
          v-model="form.scope"
          :projects="projects"
          :agents="agents"
          :workflows="workflows"
          :integrations="integrations"
          :allowed-scopes="selectedPermission?.allowedScopes"
          :allowed-resource-kinds="selectedPermission?.resourceKinds"
          :show-contract-boundary="mode !== 'QUERY'"
          @load-project-resources="emit('load-project-resources', $event)"
        />
        <ProblemNotice v-if="problem" :problem="problem" compact />
        <button
          class="button button--primary"
          type="submit"
          :disabled="loading || !valid"
        >
          {{ $t(`access.effective.actions.${mode}`) }}
        </button>
      </form>

      <section class="result-panel panel" aria-live="polite">
        <div v-if="loading" class="skeleton-stack" role="status">
          <span /><span /><span />
        </div>
        <div v-else-if="mode === 'QUERY' && effective" class="effective-matrix">
          <div class="effective-matrix__summary">
            <h3>
              {{ $t("access.effective.matrixTitle") }}
              <span>{{ effective.items.length }}</span>
            </h3>
            <p>
              {{
                $t("access.effective.matrixSummary", {
                  allowed: effective.items.filter(
                    (item) => item.decision === "ALLOWED",
                  ).length,
                  denied: effective.items.filter(
                    (item) => item.decision !== "ALLOWED",
                  ).length,
                })
              }}
            </p>
          </div>
          <div
            class="effective-matrix__table"
            role="table"
            :aria-label="$t('access.effective.matrixTitle')"
          >
            <header
              class="effective-matrix__row effective-matrix__row--head"
              role="row"
            >
              <span>{{ $t("access.effective.permission") }}</span>
              <span>{{ $t("access.effective.risk") }}</span>
              <span>{{ $t("access.effective.result") }}</span>
              <span>{{ $t("access.effective.source") }}</span>
            </header>
            <button
              v-for="item in effective.items"
              :key="item.permissionKey"
              class="effective-matrix__row effective-matrix__option"
              :class="{
                'effective-matrix__option--selected':
                  item.permissionKey === form.permissionKey,
              }"
              type="button"
              :aria-pressed="item.permissionKey === form.permissionKey"
              @click="form.permissionKey = item.permissionKey"
            >
              <strong>{{ permissionName(item.permissionKey) }}</strong>
              <span>{{ riskLabel(item.permissionKey) }}</span>
              <StatusBadge
                :state="item.decision"
                :tone="item.decision === 'ALLOWED' ? 'success' : 'danger'"
              />
              <span>{{
                item.explanation.length
                  ? $t(`access.explanation.${item.explanation[0]?.code}`)
                  : $t("access.effective.noSource")
              }}</span>
            </button>
          </div>
          <aside
            class="effective-matrix__detail"
            :aria-label="$t('access.effective.selectedDecision')"
          >
            <template v-if="decision">
              <header class="decision-header">
                <div>
                  <h3>{{ permissionName(decision.permissionKey) }}</h3>
                  <p>{{ effective.subject.displayName }}</p>
                </div>
                <StatusBadge
                  :state="decision.decision"
                  :tone="decision.decision === 'ALLOWED' ? 'success' : 'danger'"
                />
              </header>
              <p class="matrix-evaluated">
                {{ $t("access.effective.evaluatedAt") }}:
                {{ new Date(effective.evaluatedAt).toLocaleString() }}
              </p>
              <h4>{{ $t("access.effective.explanationTitle") }}</h4>
              <ol class="explanation-list">
                <li
                  v-for="(step, index) in decision.explanation"
                  :key="`${step.code}-${index}`"
                >
                  <strong>{{ $t(`access.explanation.${step.code}`) }}</strong
                  ><small v-if="accessScopeKind(step.scope)">{{
                    $t(`access.scope.values.${accessScopeKind(step.scope)}`)
                  }}</small>
                </li>
              </ol>
              <p v-if="!decision.explanation.length" class="muted">
                {{ $t("access.effective.noSource") }}
              </p>
            </template>
          </aside>
        </div>
        <template v-else-if="simulation">
          <h3>{{ $t("access.effective.simulationTitle") }}</h3>
          <div class="decision-comparison">
            <article>
              <span>{{ $t("access.effective.current") }}</span>
              <StatusBadge
                :state="simulation.current.decision"
                :tone="
                  simulation.current.decision === 'ALLOWED'
                    ? 'success'
                    : 'danger'
                "
              />
            </article>
            <article>
              <span>{{ $t("access.effective.after") }}</span>
              <StatusBadge
                :state="simulation.simulated.decision"
                :tone="
                  simulation.simulated.decision === 'ALLOWED'
                    ? 'success'
                    : 'danger'
                "
              />
            </article>
          </div>
          <p>{{ $t("access.effective.readOnlySimulation") }}</p>
        </template>
        <template v-else-if="decision">
          <header class="decision-header">
            <div>
              <h3>{{ $t("access.effective.result") }}</h3>
              <p>{{ selectedSubject?.displayName }}</p>
            </div>
            <StatusBadge
              :state="decision.decision"
              :tone="decision.decision === 'ALLOWED' ? 'success' : 'danger'"
            />
          </header>
          <dl class="decision-context">
            <div>
              <dt>{{ $t("access.effective.who") }}</dt>
              <dd>{{ selectedSubject?.displayName }}</dd>
            </div>
            <div>
              <dt>{{ $t("access.effective.what") }}</dt>
              <dd>
                {{
                  selectedPermission
                    ? permissionMessage(
                        permissionMessages,
                        selectedPermission.key,
                        "name",
                      )
                    : form.permissionKey
                }}
              </dd>
            </div>
            <div>
              <dt>{{ $t("access.effective.where") }}</dt>
              <dd>{{ $t("access.scope.values." + decision.target.kind) }}</dd>
            </div>
            <div>
              <dt>{{ $t("access.effective.target") }}</dt>
              <dd>
                {{
                  decision.target.resourceKind
                    ? $t("access.resourceKinds." + decision.target.resourceKind)
                    : $t("access.resourceKinds.ORGANIZATION")
                }}
              </dd>
            </div>
          </dl>
          <ol class="explanation-list">
            <li
              v-for="(step, index) in decision.explanation"
              :key="`${step.code}-${index}`"
            >
              <strong>{{ $t(`access.explanation.${step.code}`) }}</strong>
              <small v-if="accessScopeKind(step.scope)">{{
                $t(`access.scope.values.${accessScopeKind(step.scope)}`)
              }}</small>
            </li>
          </ol>
        </template>
        <div v-else class="result-empty">
          <h3>{{ $t("access.effective.noResult") }}</h3>
          <p>{{ $t("access.effective.noResultHint") }}</p>
        </div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.effective-layout.effective-layout--query {
  grid-template-columns: minmax(0, 1fr);
}
.effective-layout--query .effective-form {
  grid-template-columns: minmax(240px, 320px) minmax(320px, 1fr) auto;
  align-items: end;
}
.effective-layout--query .effective-form > .scope-editor {
  grid-column: 2;
  grid-row: 1;
  padding: 0;
  border: 0;
  background: transparent;
}
.effective-layout--query .effective-form > .query-hint,
.effective-layout--query .effective-form > .problem-notice {
  grid-column: 1 / -1;
}
.effective-layout--query .effective-form > button {
  grid-column: 3;
  grid-row: 1;
  justify-self: end;
  min-width: 190px;
}
.query-hint {
  margin: 0;
  color: var(--muted);
  font-size: 0.85rem;
}
.effective-matrix {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 380px;
  gap: 16px;
  min-width: 0;
}
.effective-matrix__summary {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.effective-matrix__summary h3,
.effective-matrix__summary p {
  margin: 0;
}
.effective-matrix__summary h3 span {
  margin-left: 4px;
  color: var(--muted);
}
.effective-matrix__table {
  max-height: min(560px, calc(100vh - 300px));
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.effective-matrix__row {
  display: grid;
  grid-template-columns: minmax(200px, 1.3fr) 145px 135px minmax(230px, 0.9fr);
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 10px 12px;
  text-align: left;
}
.effective-matrix__row--head {
  position: sticky;
  top: 0;
  z-index: 1;
  color: var(--muted);
  background: #f4f6f8;
  font-size: 0.78rem;
  font-weight: 600;
}
.effective-matrix__option {
  min-height: 50px;
  border: 0;
  border-top: 1px solid var(--border);
  background: var(--surface);
  cursor: pointer;
}
.effective-matrix__option--selected {
  background: var(--accent-soft);
  box-shadow: inset 3px 0 var(--accent);
}
.effective-matrix__option strong {
  overflow-wrap: anywhere;
}
.effective-matrix__detail {
  min-width: 0;
  max-height: min(560px, calc(100vh - 300px));
  overflow-y: auto;
  padding: 14px 16px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.effective-matrix__detail h4 {
  margin: 18px 0 8px;
  font-size: 0.85rem;
}
.matrix-evaluated {
  margin: 12px 0 0;
  color: var(--muted);
  font-size: 0.8rem;
}
.muted {
  color: var(--muted);
}
@media (max-width: 1200px) {
  .effective-matrix {
    grid-template-columns: 1fr;
  }
}
.effective-header,
.decision-header,
.decision-comparison,
.decision-comparison article {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
}
.decision-context {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
}
.decision-context div {
  padding: 9px 10px;
  border: 1px solid var(--hairline);
  border-radius: 7px;
  background: var(--panel);
}
.decision-context dt,
.decision-context dd {
  margin: 0;
}
.decision-context dt {
  color: var(--muted);
  font-size: 0.75rem;
}
.decision-context dd {
  margin-top: 3px;
  font-weight: 600;
}
.effective-header {
  margin-bottom: 14px;
}
.effective-header h2,
.effective-header p,
.decision-header h3,
.decision-header p,
.result-panel h3,
.result-panel p {
  margin: 0;
}
.effective-header p,
.decision-header p,
.result-panel p,
.explanation-list small {
  color: var(--muted);
}
.mode-switch {
  display: inline-flex;
  padding: 3px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: #f2f4f6;
}
.mode-switch button {
  min-height: 30px;
  padding: 4px 9px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  cursor: pointer;
}
.mode-switch button.active {
  background: var(--surface);
  box-shadow: 0 1px 3px rgb(20 32 44 / 12%);
  font-weight: 600;
}
.effective-layout {
  display: grid;
  grid-template-columns: minmax(420px, 1.1fr) minmax(320px, 0.9fr);
  gap: 14px;
  align-items: start;
}
.effective-form,
.result-panel {
  display: grid;
  gap: 14px;
}
.result-panel {
  min-height: 310px;
}
.decision-comparison article {
  flex: 1;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.explanation-list {
  display: grid;
  gap: 9px;
  margin: 0;
  padding-left: 22px;
}
.explanation-list li {
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 7px;
}
.explanation-list small {
  display: block;
  margin-top: 3px;
}
.result-empty {
  display: grid;
  place-content: center;
  min-height: 250px;
  text-align: center;
}
@media (max-width: 900px) {
  .effective-header {
    align-items: stretch;
    flex-direction: column;
  }
  .mode-switch {
    overflow-x: auto;
  }
  .effective-layout {
    grid-template-columns: 1fr;
  }
}
</style>

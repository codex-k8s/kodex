<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import AccessScopeEditor from "@/features/access/components/AccessScopeEditor.vue";
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
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

type Mode = "QUERY" | "EXPLAIN" | "SIMULATE";

const props = defineProps<{
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

const mode = ref<Mode>("EXPLAIN");
const form = reactive({
  subjectRef: "",
  permissionKey: "",
  roleRef: "",
  scope: emptyScopeDraft(),
});
const selectedSubject = computed(() =>
  props.subjects.find((subject) => subject.ref === form.subjectRef),
);
const selectedRole = computed(() =>
  props.roles.find((role) => role.ref === form.roleRef),
);
const selectedPermission = computed(() =>
  props.permissions.find((permission) => permission.key === form.permissionKey),
);
const valid = computed(
  () =>
    Boolean(form.subjectRef) &&
    Boolean(form.permissionKey) &&
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
      permissionKeys: [form.permissionKey],
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

watch(mode, () => emit("clear"));
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

    <div class="effective-layout">
      <form class="effective-form panel" @submit.prevent="submit">
        <label class="field">
          <span>{{ $t("access.effective.subject") }}</span>
          <select v-model="form.subjectRef" required>
            <option value="" disabled>
              {{ $t("access.effective.chooseSubject") }}
            </option>
            <option
              v-for="subject in subjects"
              :key="subject.ref"
              :value="subject.ref"
            >
              {{ subject.displayName }} ·
              {{ $t(`access.subjectKinds.${subject.kind}`) }}
            </option>
          </select>
        </label>
        <label class="field">
          <span>{{ $t("access.effective.permission") }}</span>
          <select v-model="form.permissionKey" required>
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
        <label v-if="mode === 'SIMULATE'" class="field">
          <span>{{ $t("access.effective.role") }}</span>
          <select v-model="form.roleRef" required>
            <option value="" disabled>
              {{ $t("access.effective.chooseRole") }}
            </option>
            <option
              v-for="role in roles.filter((item) => item.state === 'ACTIVE')"
              :key="role.ref"
              :value="role.ref"
            >
              {{ role.currentVersion.name }} · v{{
                role.currentVersion.revision
              }}
            </option>
          </select>
        </label>
        <AccessScopeEditor
          v-model="form.scope"
          :projects="projects"
          :agents="agents"
          :workflows="workflows"
          :integrations="integrations"
          :allowed-scopes="selectedPermission?.allowedScopes"
          :allowed-resource-kinds="selectedPermission?.resourceKinds"
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

<script setup lang="ts">
import { computed, ref } from "vue";

import type {
  AccessBinding,
  Agent,
  Project,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  bindings: AccessBinding[];
  projects: Project[];
  agentsByProject: Record<string, Agent[]>;
  loading?: boolean;
  problem?: AppProblem;
  hasMore?: boolean;
}>();
const emit = defineEmits<{
  create: [];
  edit: [binding: AccessBinding];
  revoke: [binding: AccessBinding];
  more: [];
  retry: [];
}>();
const stateFilter = ref<"ACTIVE" | "ALL">("ACTIVE");
const visible = computed(() =>
  stateFilter.value === "ALL"
    ? props.bindings
    : props.bindings.filter((binding) => binding.state === "ACTIVE"),
);

function projectName(ref?: string): string {
  return props.projects.find((project) => project.ref === ref)?.name ?? "";
}
function resourceName(binding: AccessBinding): string {
  const scope = binding.scope;
  if (scope.kind === "ORGANIZATION") return "";
  if (scope.kind === "PROJECT") return projectName(scope.projectRef);
  if (scope.kind === "RESOURCE_KIND") {
    return [projectName(scope.projectRef), scope.resourceKind]
      .filter(Boolean)
      .join(" · ");
  }
  if (scope.resourceKind === "AGENT" && scope.projectRef) {
    return (
      props.agentsByProject[scope.projectRef]?.find(
        (agent) => agent.ref === scope.resourceRef,
      )?.name ?? ""
    );
  }
  return scope.resourceKind ?? "";
}
</script>

<template>
  <section>
    <header class="bindings-header">
      <div>
        <h2>{{ $t("access.bindingsWorkspace.title") }}</h2>
        <p>{{ $t("access.bindingsWorkspace.subtitle") }}</p>
      </div>
      <div class="bindings-actions">
        <select
          v-model="stateFilter"
          :aria-label="$t('access.bindingsWorkspace.filter')"
        >
          <option value="ACTIVE">{{ $t("common.active") }}</option>
          <option value="ALL">{{ $t("common.all") }}</option>
        </select>
        <button
          class="button button--primary"
          type="button"
          @click="emit('create')"
        >
          {{ $t("access.bindingsWorkspace.create") }}
        </button>
      </div>
    </header>
    <AsyncState
      :loading="loading"
      :problem="problem"
      :empty="visible.length === 0"
      :empty-title="$t('access.bindingsWorkspace.empty')"
      :empty-text="$t('access.bindingsWorkspace.emptyHint')"
      @retry="emit('retry')"
    >
      <div class="binding-list">
        <article
          v-for="binding in visible"
          :key="binding.ref"
          class="binding-card"
        >
          <header>
            <div>
              <strong>{{ binding.subject.displayName }}</strong>
              <small>{{
                $t(`access.subjectKinds.${binding.subject.kind}`)
              }}</small>
            </div>
            <StatusBadge :state="binding.state" />
          </header>
          <div class="binding-arrow" aria-hidden="true">→</div>
          <div>
            <strong>{{ binding.roleVersion.name }}</strong>
            <small>v{{ binding.roleVersion.revision }}</small>
          </div>
          <div>
            <strong>{{
              $t(`access.scope.values.${binding.scope.kind}`)
            }}</strong>
            <small>{{
              resourceName(binding) ||
              $t("access.bindingsWorkspace.wholeOrganization")
            }}</small>
          </div>
          <div class="binding-conditions">
            <span v-if="binding.conditions.requireOwner">{{
              $t("access.bindingsWorkspace.ownerOnly")
            }}</span>
            <span v-if="binding.conditions.validUntil">{{
              $t("access.bindingsWorkspace.until", {
                date: new Date(binding.conditions.validUntil).toLocaleString(),
              })
            }}</span>
            <span
              v-if="
                !binding.conditions.requireOwner &&
                !binding.conditions.validUntil
              "
              >{{ $t("access.bindingsWorkspace.noConditions") }}</span
            >
          </div>
          <footer>
            <button
              class="button"
              type="button"
              :disabled="binding.state !== 'ACTIVE'"
              @click="emit('edit', binding)"
            >
              {{ $t("common.edit") }}
            </button>
            <button
              class="button button--danger"
              type="button"
              :disabled="binding.state !== 'ACTIVE'"
              @click="emit('revoke', binding)"
            >
              {{ $t("access.revoke") }}
            </button>
          </footer>
        </article>
      </div>
      <button
        v-if="hasMore"
        class="button load-more"
        type="button"
        :disabled="loading"
        @click="emit('more')"
      >
        {{ $t("access.loadMore") }}
      </button>
    </AsyncState>
  </section>
</template>

<style scoped>
.bindings-header,
.bindings-actions,
.binding-card header,
.binding-card footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.bindings-header {
  margin-bottom: 14px;
}
.bindings-header h2,
.bindings-header p {
  margin: 0;
}
.bindings-header p,
.binding-card small,
.binding-conditions {
  color: var(--muted);
}
.bindings-actions select {
  min-width: 140px;
}
.binding-list {
  display: grid;
  gap: 10px;
}
.binding-card {
  display: grid;
  grid-template-columns:
    minmax(180px, 1.2fr) auto minmax(180px, 1fr) minmax(170px, 1fr)
    minmax(150px, 0.8fr) auto;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.binding-card header {
  justify-content: flex-start;
}
.binding-card small {
  display: block;
  margin-top: 2px;
}
.binding-conditions {
  display: grid;
  gap: 2px;
}
.load-more {
  display: flex;
  margin: 14px auto 0;
}
@media (max-width: 1050px) {
  .binding-card {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .binding-card > :not(header):not(footer):not(.binding-arrow) {
    grid-column: 1 / -1;
  }
}
@media (max-width: 650px) {
  .bindings-header,
  .bindings-actions {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";

import type {
  AccessBinding,
  AccessRole,
  Agent,
  Project,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";

const props = defineProps<{
  bindings: AccessBinding[];
  roles: AccessRole[];
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
  search: [query: string, includeRevoked: boolean, pageSize: number];
  more: [query: string, includeRevoked: boolean, pageSize: number];
  retry: [];
}>();
const stateFilter = ref<"ACTIVE" | "ALL">("ACTIVE");
const query = ref("");
const searchId = useId();
let timer: ReturnType<typeof setTimeout> | undefined;
const visible = computed(() =>
  stateFilter.value === "ALL"
    ? props.bindings
    : props.bindings.filter((binding) => binding.state === "ACTIVE"),
);
const listRoot = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: listRoot,
  itemSelector: ".binding-card",
  itemCount: () => visible.value.length,
  estimatedItemHeight: 170,
});
useCursorInfiniteScroll({
  root: listRoot,
  sentinel,
  enabled: () => props.hasMore && !props.loading,
  loadMore: () =>
    emit(
      "more",
      query.value.trim(),
      stateFilter.value === "ALL",
      pageSize.value,
    ),
});
watch([query, stateFilter], ([value, state]) => {
  if (timer) clearTimeout(timer);
  timer = setTimeout(
    () => emit("search", value.trim(), state === "ALL", pageSize.value),
    250,
  );
});
onBeforeUnmount(() => {
  if (timer) clearTimeout(timer);
});

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
function assignmentKind(
  binding: AccessBinding,
): "PLATFORM_ROLE" | "PROJECT_MEMBERSHIP" | "SCOPED_GRANT" {
  const role = props.roles.find(
    (item) => item.ref === binding.roleVersion.roleRef,
  );
  if (binding.scope.kind === "ORGANIZATION" && role?.kind === "SYSTEM")
    return "PLATFORM_ROLE";
  if (binding.scope.kind === "PROJECT") return "PROJECT_MEMBERSHIP";
  return "SCOPED_GRANT";
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
        <label class="sr-only" :for="searchId">
          {{ $t("access.bindingsWorkspace.search") }}
        </label>
        <input
          :id="searchId"
          v-model="query"
          class="bindings-search"
          name="access-binding-search"
          type="search"
          autocomplete="off"
          :placeholder="$t('access.bindingsWorkspace.searchPlaceholder')"
        />
        <select
          v-model="stateFilter"
          name="access-binding-state-filter"
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
      :empty-title="
        $t(
          query.trim()
            ? 'access.bindingsWorkspace.searchEmpty'
            : 'access.bindingsWorkspace.empty',
        )
      "
      :empty-text="
        $t(
          query.trim()
            ? 'access.bindingsWorkspace.searchEmptyHint'
            : 'access.bindingsWorkspace.emptyHint',
        )
      "
      @retry="emit('retry')"
    >
      <div ref="listRoot" class="binding-list">
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
            <small
              >v{{ binding.roleVersion.revision }} ·
              {{
                $t("access.bindingsWorkspace.permissionCount", {
                  count: binding.roleVersion.permissionKeys.length,
                })
              }}</small
            >
          </div>
          <div>
            <span class="assignment-kind">{{
              $t(
                "access.bindingsWorkspace.assignmentKinds." +
                  assignmentKind(binding),
              )
            }}</span>
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
      <div v-if="hasMore" ref="sentinel" class="cursor-sentinel" />
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
.bindings-search {
  width: min(340px, 28vw);
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
.assignment-kind {
  display: block;
  width: max-content;
  margin-bottom: 4px;
  padding: 2px 6px;
  border-radius: 6px;
  color: var(--accent-strong);
  background: var(--accent-soft);
  font-size: 0.72rem;
  font-weight: 600;
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

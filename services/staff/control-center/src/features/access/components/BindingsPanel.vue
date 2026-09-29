<script setup lang="ts">
import { LockKeyhole, Pencil, Trash2, UsersRound } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";

import type {
  AccessBinding,
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
  projects: Project[];
  agentsByProject: Record<string, Agent[]>;
  loading?: boolean;
  problem?: AppProblem;
  hasMore?: boolean;
}>();
const { t } = useI18n();
const emit = defineEmits<{
  create: [];
  edit: [binding: AccessBinding];
  revoke: [binding: AccessBinding];
  "manage-membership": [binding: AccessBinding];
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
  itemSelector: ".binding-row",
  itemCount: () => visible.value.length,
  estimatedItemHeight: 72,
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
    return [
      projectName(scope.projectRef),
      scope.resourceKind ? t(`access.resourceKinds.${scope.resourceKind}`) : "",
    ]
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
  return scope.resourceKind
    ? t(`access.resourceKinds.${scope.resourceKind}`)
    : "";
}
function assignmentKind(
  binding: AccessBinding,
): "PLATFORM_ROLE" | "PROJECT_MEMBERSHIP" | "SCOPED_GRANT" {
  if (binding.managementKind === "PLATFORM_MEMBERSHIP") return "PLATFORM_ROLE";
  if (binding.managementKind === "PROJECT_MEMBERSHIP")
    return "PROJECT_MEMBERSHIP";
  return "SCOPED_GRANT";
}

function isDirect(binding: AccessBinding): boolean {
  return binding.managementKind === "DIRECT";
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
      <div
        ref="listRoot"
        class="binding-table"
        role="table"
        :aria-label="$t('access.bindingsWorkspace.title')"
      >
        <div class="binding-table__head" role="row">
          <span>{{ $t("access.bindingsWorkspace.columns.subject") }}</span>
          <span>{{ $t("access.bindingsWorkspace.columns.role") }}</span>
          <span>{{ $t("access.bindingsWorkspace.columns.assignment") }}</span>
          <span>{{ $t("access.bindingsWorkspace.columns.conditions") }}</span>
          <span>{{ $t("common.status") }}</span>
          <span class="sr-only">{{ $t("common.actions") }}</span>
        </div>
        <article
          v-for="binding in visible"
          :key="binding.ref"
          class="binding-row"
          role="row"
        >
          <div class="binding-cell">
            <strong>{{ binding.subject.displayName }}</strong>
            <small>{{
              $t(`access.subjectKinds.${binding.subject.kind}`)
            }}</small>
          </div>
          <div class="binding-cell">
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
          <div class="binding-cell binding-scope">
            <span class="assignment-kind">{{
              $t(
                "access.bindingsWorkspace.assignmentKinds." +
                  assignmentKind(binding),
              )
            }}</span>
            <strong>{{
              resourceName(binding) ||
              $t(`access.scope.values.${binding.scope.kind}`)
            }}</strong>
            <small v-if="binding.scope.kind !== 'ORGANIZATION'">{{
              $t(`access.scope.values.${binding.scope.kind}`)
            }}</small>
          </div>
          <div class="binding-cell binding-conditions">
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
          <StatusBadge :state="binding.state" />
          <div class="binding-row__actions">
            <button
              v-if="!isDirect(binding)"
              class="icon-button"
              type="button"
              :title="$t('access.bindingsWorkspace.manageMembership')"
              :aria-label="$t('access.bindingsWorkspace.manageMembership')"
              @click="emit('manage-membership', binding)"
            >
              <UsersRound :size="17" aria-hidden="true" />
            </button>
            <span
              v-if="!isDirect(binding)"
              class="binding-protected"
              :title="$t('access.bindingsWorkspace.membershipProtected')"
            >
              <LockKeyhole :size="14" aria-hidden="true" />
              <span class="sr-only">{{
                $t("access.bindingsWorkspace.membershipProtected")
              }}</span>
            </span>
            <button
              v-if="isDirect(binding)"
              class="icon-button"
              type="button"
              :disabled="binding.state !== 'ACTIVE'"
              :title="$t('common.edit')"
              :aria-label="$t('common.edit')"
              @click="emit('edit', binding)"
            >
              <Pencil :size="17" aria-hidden="true" />
            </button>
            <button
              v-if="isDirect(binding)"
              class="icon-button binding-row__revoke"
              type="button"
              :disabled="binding.state !== 'ACTIVE'"
              :title="$t('access.revoke')"
              :aria-label="$t('access.revoke')"
              @click="emit('revoke', binding)"
            >
              <Trash2 :size="17" aria-hidden="true" />
            </button>
          </div>
        </article>
      </div>
      <div v-if="hasMore" ref="sentinel" class="cursor-sentinel" />
    </AsyncState>
  </section>
</template>

<style scoped>
.bindings-header,
.bindings-actions {
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
.binding-row small,
.binding-conditions {
  color: var(--muted);
}
.bindings-actions select {
  min-width: 140px;
}
.bindings-search {
  width: min(340px, 28vw);
}
.binding-table {
  min-width: 1040px;
  max-height: min(720px, calc(100dvh - 330px));
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.binding-table__head,
.binding-row {
  display: grid;
  grid-template-columns:
    minmax(170px, 1.1fr) minmax(170px, 1fr) minmax(230px, 1.35fr)
    minmax(170px, 0.9fr) 110px 82px;
  align-items: center;
  gap: 14px;
  min-height: 68px;
  padding: 10px 14px;
}
.binding-table__head {
  position: sticky;
  z-index: 1;
  top: 0;
  min-height: 42px;
  border-bottom: 1px solid var(--border);
  color: var(--muted);
  background: var(--surface-subtle, #f8fafc);
  font-size: 0.76rem;
  font-weight: 600;
}
.binding-row {
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.binding-row:last-child {
  border-bottom: 0;
}
.binding-cell {
  min-width: 0;
}
.binding-cell strong,
.binding-cell small {
  overflow: hidden;
  text-overflow: ellipsis;
}
.binding-cell strong {
  display: block;
  white-space: nowrap;
}
.binding-row small {
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
.binding-row__actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 2px;
}
.binding-row__revoke {
  color: var(--danger);
}
.binding-protected {
  display: inline-grid;
  place-items: center;
  width: 24px;
  height: 24px;
  color: var(--muted);
}
@media (max-width: 650px) {
  .bindings-header,
  .bindings-actions {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>

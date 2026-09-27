<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";

import { permissionMessage } from "@/features/access/presentation";
import type {
  AccessRole,
  PermissionDefinition,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";

const props = defineProps<{
  roles: AccessRole[];
  permissions: PermissionDefinition[];
  permissionRegistryUnavailable?: boolean;
  loading?: boolean;
  problem?: AppProblem;
  hasMore?: boolean;
}>();
const emit = defineEmits<{
  create: [];
  edit: [role: AccessRole];
  archive: [role: AccessRole];
  search: [query: string, pageSize: number];
  more: [query: string, pageSize: number];
  retry: [];
}>();
const i18n = useI18n();
const permissionMessages = computed(() =>
  i18n.tm("access.permissionsRegistry"),
);
const query = ref("");
const selectedRef = ref("");
const searchId = useId();
let timer: ReturnType<typeof setTimeout> | undefined;
const listRoot = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: listRoot,
  itemSelector: ".role-table__row",
  itemCount: () => props.roles.length,
  estimatedItemHeight: 60,
});
useCursorInfiniteScroll({
  root: listRoot,
  sentinel,
  enabled: () => props.hasMore && !props.loading,
  loadMore: () => emit("more", query.value.trim(), pageSize.value),
});
watch(query, (value) => {
  if (timer) clearTimeout(timer);
  timer = setTimeout(() => emit("search", value.trim(), pageSize.value), 250);
});
onBeforeUnmount(() => {
  if (timer) clearTimeout(timer);
});

function permissionDefinition(key: string): PermissionDefinition | undefined {
  return props.permissions.find((permission) => permission.key === key);
}
function hasDuplicateName(role: AccessRole): boolean {
  return props.roles.some(
    (other) =>
      other.ref !== role.ref &&
      other.kind === role.kind &&
      other.currentVersion.name === role.currentVersion.name,
  );
}
const selectedRole = computed(() =>
  props.roles.find((role) => role.ref === selectedRef.value),
);
watch(
  () => props.roles,
  (roles) => {
    if (!roles.some((role) => role.ref === selectedRef.value))
      selectedRef.value =
        roles.find((role) => role.kind === "CUSTOM")?.ref ??
        roles[0]?.ref ??
        "";
  },
  { immediate: true },
);
</script>

<template>
  <section class="roles-workspace">
    <header class="roles-header">
      <p>{{ $t("access.rolesWorkspace.subtitle") }}</p>
      <div class="roles-actions">
        <label class="sr-only" :for="searchId">
          {{ $t("access.rolesWorkspace.search") }}
        </label>
        <input
          :id="searchId"
          v-model="query"
          class="roles-search"
          name="access-role-search"
          type="search"
          autocomplete="off"
          :placeholder="$t('access.rolesWorkspace.searchPlaceholder')"
        />
        <button
          class="button button--primary"
          type="button"
          :disabled="permissionRegistryUnavailable"
          @click="emit('create')"
        >
          {{ $t("access.rolesWorkspace.create") }}
        </button>
      </div>
    </header>
    <AsyncState
      :loading="loading"
      :problem="problem"
      :empty="roles.length === 0"
      :empty-title="
        $t(
          query.trim()
            ? 'access.rolesWorkspace.searchEmpty'
            : 'access.rolesWorkspace.empty',
        )
      "
      :empty-text="
        $t(
          query.trim()
            ? 'access.rolesWorkspace.searchEmptyHint'
            : 'access.rolesWorkspace.emptyHint',
        )
      "
      @retry="emit('retry')"
    >
      <div class="roles-layout">
        <div ref="listRoot" class="role-groups">
          <section
            v-for="kind in ['SYSTEM', 'CUSTOM'] as const"
            :key="kind"
            class="role-table"
            :aria-label="$t(`access.roleKinds.${kind}`)"
          >
            <header class="role-kind-header">
              <h3>{{ $t(`access.roleKinds.${kind}`) }}</h3>
              <span class="role-kind-count">{{
                $t("access.rolesWorkspace.loadedCount", {
                  count: roles.filter((role) => role.kind === kind).length,
                })
              }}</span>
              <small v-if="kind === 'SYSTEM'">{{
                $t("access.rolesWorkspace.systemImmutable")
              }}</small>
            </header>
            <template v-if="roles.some((role) => role.kind === kind)">
              <div class="role-table__head" role="row">
                <span>{{ $t("access.rolesWorkspace.roleColumn") }}</span>
                <span>{{ $t("access.rolesWorkspace.descriptionColumn") }}</span>
                <span>{{ $t("access.rolesWorkspace.scopeColumn") }}</span>
                <span>{{ $t("access.rolesWorkspace.bindingsColumn") }}</span>
                <span class="sr-only">{{ $t("common.actions") }}</span>
              </div>
              <div
                v-for="role in roles.filter((item) => item.kind === kind)"
                :key="role.ref"
                class="role-table__row"
                :class="{
                  'role-table__row--selected': role.ref === selectedRef,
                }"
                role="row"
                :aria-selected="role.ref === selectedRef"
              >
                <div>
                  <strong>{{ role.currentVersion.name }}</strong
                  ><small
                    >v{{ role.currentVersion.revision
                    }}<template v-if="hasDuplicateName(role)">
                      · {{ role.ref.slice(-8) }}</template
                    ></small
                  >
                </div>
                <span class="role-table__description">{{
                  role.currentVersion.description
                }}</span>
                <span>{{
                  role.currentVersion.allowedScopes
                    .map((scope) => $t(`access.scope.values.${scope}`))
                    .join(", ")
                }}</span>
                <span>{{ role.bindingCount ?? 0 }}</span>
                <button
                  class="button"
                  type="button"
                  :aria-label="
                    $t('access.rolesWorkspace.selectRole', {
                      name: role.currentVersion.name,
                    })
                  "
                  @click="selectedRef = role.ref"
                >
                  {{ $t("access.participants.details") }}
                </button>
              </div>
            </template>
          </section>
        </div>
        <aside
          class="role-detail"
          :aria-label="$t('access.rolesWorkspace.selectedRole')"
        >
          <template v-if="selectedRole">
            <header class="role-detail__head">
              <div>
                <h3>{{ selectedRole.currentVersion.name }}</h3>
                <small
                  >{{ $t(`access.roleKinds.${selectedRole.kind}`) }} · v{{
                    selectedRole.currentVersion.revision
                  }}</small
                >
                <small v-if="hasDuplicateName(selectedRole)" class="mono">{{
                  selectedRole.ref
                }}</small>
              </div>
              <StatusBadge :state="selectedRole.state" />
            </header>
            <div class="role-detail__body">
              <p>{{ selectedRole.currentVersion.description }}</p>
              <dl>
                <div>
                  <dt>{{ $t("access.rolesWorkspace.bindingsColumn") }}</dt>
                  <dd>{{ selectedRole.bindingCount ?? 0 }}</dd>
                </div>
                <div>
                  <dt>{{ $t("access.rolesWorkspace.scopeColumn") }}</dt>
                  <dd>
                    {{
                      selectedRole.currentVersion.allowedScopes
                        .map((scope) => $t(`access.scope.values.${scope}`))
                        .join(", ")
                    }}
                  </dd>
                </div>
              </dl>
              <h4>
                {{
                  $t("access.rolesWorkspace.showPermissions", {
                    count: selectedRole.currentVersion.permissionKeys.length,
                  })
                }}
              </h4>
              <ul class="permission-list">
                <li
                  v-for="permissionKey in selectedRole.currentVersion
                    .permissionKeys"
                  :key="permissionKey"
                >
                  <div>
                    <strong>{{
                      permissionDefinition(permissionKey)
                        ? permissionMessage(
                            permissionMessages,
                            permissionKey,
                            "name",
                          )
                        : permissionMessage(
                              permissionMessages,
                              permissionKey,
                              "name",
                            ) === permissionKey
                          ? $t("access.roleEditor.unknownPermission")
                          : permissionMessage(
                              permissionMessages,
                              permissionKey,
                              "name",
                            )
                    }}</strong>
                    <small v-if="permissionDefinition(permissionKey)">{{
                      permissionMessage(
                        permissionMessages,
                        permissionKey,
                        "description",
                      )
                    }}</small>
                    <small v-else class="unavailable">{{
                      $t("access.rolesWorkspace.permissionUnavailable")
                    }}</small>
                  </div>
                  <span
                    v-if="permissionDefinition(permissionKey)"
                    :class="`risk risk--${permissionDefinition(permissionKey)!.risk.toLowerCase()}`"
                    >{{
                      $t(
                        `access.risk.${permissionDefinition(permissionKey)!.risk}`,
                      )
                    }}</span
                  >
                </li>
              </ul>
            </div>
            <footer class="role-detail__foot">
              <button
                class="button"
                type="button"
                :disabled="
                  selectedRole.kind === 'SYSTEM' ||
                  selectedRole.state !== 'ACTIVE' ||
                  permissionRegistryUnavailable
                "
                @click="emit('edit', selectedRole)"
              >
                {{
                  $t(
                    selectedRole.kind === "SYSTEM"
                      ? "access.rolesWorkspace.systemImmutable"
                      : "common.edit",
                  )
                }}
              </button>
              <button
                v-if="
                  selectedRole.kind === 'CUSTOM' &&
                  selectedRole.state === 'ACTIVE'
                "
                class="button button--danger"
                type="button"
                @click="emit('archive', selectedRole)"
              >
                {{ $t("common.archive") }}
              </button>
            </footer>
          </template>
        </aside>
      </div>
      <div v-if="hasMore" ref="sentinel" class="cursor-sentinel" />
    </AsyncState>
  </section>
</template>

<style scoped>
.roles-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 340px;
  align-items: start;
  gap: 16px;
  min-width: 0;
}
.role-groups {
  display: grid;
  gap: 14px;
}
.role-table,
.role-detail {
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  overflow: hidden;
}
.role-kind-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
}
.role-kind-header h3 {
  margin: 0;
  font-size: 0.9rem;
}
.role-kind-header small {
  margin-left: auto;
  color: var(--muted);
}
.role-table__head,
.role-table__row {
  display: grid;
  grid-template-columns:
    minmax(140px, 1.05fr) minmax(210px, 1.5fr) minmax(140px, 1fr)
    72px 100px;
  gap: 12px;
  align-items: center;
  padding: 10px 13px;
}
.role-table__head {
  color: var(--muted);
  background: #f4f6f8;
  font-size: 0.78rem;
  font-weight: 600;
}
.role-table__row {
  min-height: 58px;
  border-top: 1px solid var(--border);
}
.role-table__row--selected {
  background: var(--accent-soft);
  box-shadow: inset 3px 0 var(--accent);
}
.role-table__row strong,
.role-table__row small {
  display: block;
}
.role-table__row small,
.role-table__description {
  color: var(--muted);
}
.role-table__description {
  line-height: 1.35;
}
.role-detail {
  display: flex;
  flex-direction: column;
  max-height: calc(100vh - 310px);
}
.role-detail__head,
.role-detail__body,
.role-detail__foot {
  padding: 14px 16px;
}
.role-detail__head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  border-bottom: 1px solid var(--border);
}
.role-detail__head > div {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.role-detail__head h3 {
  margin: 0;
  font-size: 1rem;
}
.role-detail__head small {
  color: var(--muted);
}
.role-detail__head .mono {
  overflow-wrap: anywhere;
}
.role-detail__body {
  min-height: 0;
  overflow-y: auto;
}
.role-detail__body p {
  margin: 0 0 14px;
  color: var(--muted);
}
.role-detail__body dl {
  margin: 0;
}
.role-detail__body dl > div {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  padding: 9px 0;
  border-bottom: 1px solid var(--border);
}
.role-detail__body dt {
  color: var(--muted);
}
.role-detail__body dd {
  margin: 0;
  text-align: right;
  overflow-wrap: anywhere;
}
.role-detail__body h4 {
  margin: 18px 0 8px;
  font-size: 0.85rem;
}
.permission-list {
  padding: 0;
  margin: 0;
  list-style: none;
}
.permission-list li {
  display: flex;
  justify-content: space-between;
  align-items: start;
  gap: 8px;
  padding: 9px 0;
  border-top: 1px solid var(--border);
}
.permission-list small {
  display: block;
  margin-top: 3px;
  color: var(--muted);
}
.role-detail__foot {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  border-top: 1px solid var(--border);
}
@media (max-width: 1100px) {
  .roles-layout {
    grid-template-columns: 1fr;
  }
}
.roles-header,
.role-kind-header,
.role-card header,
.role-card footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
}
.roles-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}
.roles-search {
  width: min(360px, 32vw);
}
.permission-details summary {
  cursor: pointer;
  font-weight: 600;
}
.permission-details ul {
  display: grid;
  gap: 6px;
  margin: 9px 0 0;
  padding: 0;
  list-style: none;
}
.permission-details li {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  padding: 8px;
  border: 1px solid var(--hairline);
  border-radius: 6px;
  background: var(--panel);
}
.permission-details small {
  display: block;
  margin-top: 2px;
  color: var(--muted);
}
.permission-details .unavailable {
  color: var(--warning);
}
.risk {
  flex: 0 0 auto;
  padding: 2px 6px;
  border-radius: 999px;
  color: var(--muted);
  background: #edf1f5;
  font-size: 0.72rem;
}
.risk--write,
.risk--approve {
  color: #725100;
  background: #fff0c7;
}
.risk--admin {
  color: #8a2626;
  background: #fde2e2;
}
.roles-header {
  margin-bottom: 16px;
}
.roles-header h2,
.roles-header p,
.role-kind-header h3,
.role-card h3,
.role-card p {
  margin: 0;
}
.roles-header p,
.role-card small {
  color: var(--muted);
}
.role-groups {
  display: grid;
  gap: 24px;
}
.role-kind-header {
  justify-content: flex-start;
  margin-bottom: 10px;
}
.role-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(330px, 100%), 1fr));
  gap: 12px;
}
.role-card {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.role-card p {
  min-height: 42px;
  color: var(--muted);
}
.role-tags {
  display: flex;
  align-items: flex-start;
  align-self: start;
  flex-wrap: wrap;
  gap: 6px;
}
.scope-tag {
  display: inline-flex;
  align-items: center;
  width: fit-content;
  height: fit-content;
  align-self: flex-start;
  padding: 3px 7px;
  border-radius: 999px;
  background: #edf1f5;
  font-size: 0.75rem;
  white-space: nowrap;
}
.role-card footer {
  justify-content: flex-start;
}
.load-more {
  display: flex;
  margin: 14px auto 0;
}
@media (max-width: 620px) {
  .roles-header {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>

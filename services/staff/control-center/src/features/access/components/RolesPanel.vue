<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { permissionMessage } from "@/features/access/presentation";
import type {
  AccessRole,
  PermissionDefinition,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

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
  more: [];
  retry: [];
}>();
const i18n = useI18n();
const permissionMessages = computed(() =>
  i18n.tm("access.permissionsRegistry"),
);

function permissionDefinition(key: string): PermissionDefinition | undefined {
  return props.permissions.find((permission) => permission.key === key);
}
</script>

<template>
  <section>
    <header class="roles-header">
      <div>
        <h2>{{ $t("access.rolesWorkspace.title") }}</h2>
        <p>{{ $t("access.rolesWorkspace.subtitle") }}</p>
      </div>
      <button
        class="button button--primary"
        type="button"
        :disabled="permissionRegistryUnavailable"
        @click="emit('create')"
      >
        {{ $t("access.rolesWorkspace.create") }}
      </button>
    </header>
    <AsyncState
      :loading="loading"
      :problem="problem"
      :empty="roles.length === 0"
      :empty-title="$t('access.rolesWorkspace.empty')"
      :empty-text="$t('access.rolesWorkspace.emptyHint')"
      @retry="emit('retry')"
    >
      <div class="role-groups">
        <section v-for="kind in ['CUSTOM', 'SYSTEM'] as const" :key="kind">
          <header class="role-kind-header">
            <h3>{{ $t(`access.roleKinds.${kind}`) }}</h3>
            <span class="count-badge">{{
              roles.filter((role) => role.kind === kind).length
            }}</span>
          </header>
          <div class="role-grid">
            <article
              v-for="role in roles.filter((item) => item.kind === kind)"
              :key="role.ref"
              class="role-card"
            >
              <header>
                <div>
                  <h3>{{ role.currentVersion.name }}</h3>
                  <small
                    >v{{ role.currentVersion.revision }} ·
                    {{ role.bindingCount }}
                    {{ $t("access.rolesWorkspace.bindingsShort") }}</small
                  >
                </div>
                <StatusBadge :state="role.state" />
              </header>
              <p>{{ role.currentVersion.description }}</p>
              <div class="role-tags">
                <span
                  v-for="scope in role.currentVersion.allowedScopes"
                  :key="scope"
                  class="scope-tag"
                  >{{ $t(`access.scope.values.${scope}`) }}</span
                >
                <span class="scope-tag">{{
                  $t("access.rolesWorkspace.permissionCount", {
                    count: role.currentVersion.permissionKeys.length,
                  })
                }}</span>
              </div>
              <details class="permission-details">
                <summary>
                  {{
                    $t("access.rolesWorkspace.showPermissions", {
                      count: role.currentVersion.permissionKeys.length,
                    })
                  }}
                </summary>
                <ul>
                  <li
                    v-for="permissionKey in role.currentVersion.permissionKeys"
                    :key="permissionKey"
                  >
                    <div>
                      <strong>
                        {{
                          permissionDefinition(permissionKey)
                            ? permissionMessage(
                                permissionMessages,
                                permissionKey,
                                "name",
                              )
                            : permissionKey
                        }}
                      </strong>
                      <small v-if="permissionDefinition(permissionKey)">
                        {{
                          permissionMessage(
                            permissionMessages,
                            permissionKey,
                            "description",
                          )
                        }}
                      </small>
                      <small v-else class="unavailable">
                        {{ $t("access.rolesWorkspace.permissionUnavailable") }}
                      </small>
                    </div>
                    <span
                      v-if="permissionDefinition(permissionKey)"
                      :class="`risk risk--${permissionDefinition(permissionKey)!.risk.toLowerCase()}`"
                    >
                      {{
                        $t(
                          `access.risk.${permissionDefinition(permissionKey)!.risk}`,
                        )
                      }}
                    </span>
                  </li>
                </ul>
              </details>
              <footer>
                <button
                  class="button"
                  type="button"
                  :disabled="
                    role.kind === 'SYSTEM' ||
                    role.state !== 'ACTIVE' ||
                    permissionRegistryUnavailable
                  "
                  @click="emit('edit', role)"
                >
                  {{
                    $t(
                      role.kind === "SYSTEM"
                        ? "access.rolesWorkspace.systemImmutable"
                        : "common.edit",
                    )
                  }}
                </button>
                <button
                  v-if="role.kind === 'CUSTOM' && role.state === 'ACTIVE'"
                  class="button button--danger"
                  type="button"
                  @click="emit('archive', role)"
                >
                  {{ $t("common.archive") }}
                </button>
              </footer>
            </article>
          </div>
        </section>
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
.roles-header,
.role-kind-header,
.role-card header,
.role-card footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
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

<script setup lang="ts">
import type { AccessRole } from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

defineProps<{
  roles: AccessRole[];
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
              <footer>
                <button
                  class="button"
                  type="button"
                  :disabled="role.kind === 'SYSTEM' || role.state !== 'ACTIVE'"
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
  flex-wrap: wrap;
  gap: 6px;
}
.scope-tag {
  padding: 3px 7px;
  border-radius: 999px;
  background: #edf1f5;
  font-size: 0.75rem;
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

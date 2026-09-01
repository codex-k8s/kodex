<script setup lang="ts">
import { LockKeyhole, Plus, ShieldCheck } from "@lucide/vue";
import { useI18n } from "vue-i18n";

import type { SystemRolePresentation } from "@/features/access/ui/model";

defineProps<{
  roles: readonly SystemRolePresentation[];
  organizationScope: boolean;
}>();

const { t } = useI18n();
</script>

<template>
  <section class="roles-panel" aria-labelledby="roles-title">
    <header class="panel-heading">
      <div>
        <h2 id="roles-title">{{ t("accessRedesign.rolesTitle") }}</h2>
        <p>{{ t("accessRedesign.rolesDescription") }}</p>
      </div>
      <button
        class="button button--primary"
        type="button"
        disabled
        :title="t('accessRedesign.customRolesUnavailable')"
      >
        <Plus :size="15" aria-hidden="true" />
        {{ t("accessRedesign.createRole") }}
      </button>
    </header>

    <section class="system-roles" aria-labelledby="system-roles-title">
      <header class="section-heading">
        <div>
          <h3 id="system-roles-title">
            {{ t("accessRedesign.systemRoles") }}
          </h3>
          <p>{{ t("accessRedesign.systemRolesHint") }}</p>
        </div>
        <span class="immutable-badge">
          <LockKeyhole :size="13" aria-hidden="true" />
          {{ t("accessRedesign.immutable") }}
        </span>
      </header>
      <div class="role-list" role="list">
        <article v-for="item in roles" :key="item.role" class="role-row">
          <div class="role-name">
            <span class="role-icon" aria-hidden="true">
              <ShieldCheck :size="17" />
            </span>
            <div>
              <h3>{{ t(`access.roles.${item.role}`) }}</h3>
              <p>{{ t(`accessRedesign.roleDescription.${item.role}`) }}</p>
            </div>
          </div>
          <span class="role-scope">{{
            t(
              organizationScope
                ? "accessRedesign.organizationScope"
                : "accessRedesign.projectScope",
            )
          }}</span>
          <div class="role-counts">
            <strong>{{ item.memberCount }}</strong>
            <span>{{ t("accessRedesign.assignedMembers") }}</span>
          </div>
          <div class="role-counts">
            <strong>{{ item.activeMemberCount }}</strong>
            <span>{{ t("accessRedesign.activeMembers") }}</span>
          </div>
        </article>
      </div>
    </section>

    <section class="custom-roles" aria-labelledby="custom-roles-title">
      <header class="section-heading">
        <div>
          <h3 id="custom-roles-title">
            {{ t("accessRedesign.customRoles") }}
          </h3>
          <p>{{ t("accessRedesign.customRolesHint") }}</p>
        </div>
        <span class="unavailable-badge">
          <LockKeyhole :size="13" aria-hidden="true" />
          {{ t("accessRedesign.backendUnavailableShort") }}
        </span>
      </header>
      <div class="custom-role-empty">
        <LockKeyhole :size="25" aria-hidden="true" />
        <strong>{{ t("accessRedesign.customRolesUnavailable") }}</strong>
        <p>{{ t("accessRedesign.customRolesGap") }}</p>
      </div>
    </section>
  </section>
</template>

<style scoped>
.roles-panel {
  display: grid;
  gap: 14px;
}
.panel-heading,
.section-heading,
.role-name,
.immutable-badge,
.unavailable-badge {
  display: flex;
  align-items: center;
  gap: 9px;
}
.panel-heading,
.section-heading {
  justify-content: space-between;
  align-items: flex-start;
}
.panel-heading h2,
.panel-heading p,
.section-heading h3,
.section-heading p,
.role-name h3,
.role-name p,
.custom-role-empty p {
  margin-bottom: 0;
}
.panel-heading p,
.section-heading p,
.role-name p,
.custom-role-empty p {
  color: var(--muted);
}
.system-roles,
.custom-roles {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.section-heading {
  min-height: 58px;
  padding: 12px 13px;
  border-bottom: 1px solid var(--border);
  background: var(--panel);
}
.immutable-badge,
.unavailable-badge,
.role-scope {
  width: max-content;
  padding: 4px 7px;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--muted);
  background: var(--surface);
  font-size: 0.75rem;
  white-space: nowrap;
}
.unavailable-badge {
  color: var(--warning);
  background: var(--warning-soft);
}
.role-row {
  display: grid;
  grid-template-columns: minmax(260px, 1fr) auto minmax(80px, auto) minmax(
      80px,
      auto
    );
  align-items: center;
  gap: 16px;
  min-height: 78px;
  padding: 11px 13px;
  border-top: 1px solid var(--hairline);
}
.role-row:first-child {
  border-top: 0;
}
.role-name {
  min-width: 0;
}
.role-name > div {
  min-width: 0;
}
.role-icon {
  display: inline-grid;
  place-items: center;
  width: 32px;
  height: 32px;
  flex: 0 0 32px;
  border-radius: 7px;
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.role-counts {
  display: grid;
  gap: 2px;
  padding-left: 11px;
  border-left: 1px solid var(--border);
}
.role-counts strong {
  font-family: var(--font-mono);
  font-size: 1rem;
}
.role-counts span {
  color: var(--muted);
  font-size: 0.72rem;
}
.custom-role-empty {
  display: grid;
  justify-items: center;
  gap: 8px;
  padding: 42px 20px;
  text-align: center;
}
@media (max-width: 800px) {
  .panel-heading,
  .section-heading {
    align-items: stretch;
    flex-direction: column;
  }
  .panel-heading .button {
    align-self: flex-start;
  }
  .role-row {
    grid-template-columns: minmax(0, 1fr) auto auto;
  }
  .role-scope {
    grid-column: 1 / -1;
  }
}
@media (max-width: 560px) {
  .role-row {
    grid-template-columns: 1fr 1fr;
  }
  .role-name,
  .role-scope {
    grid-column: 1 / -1;
  }
}
</style>

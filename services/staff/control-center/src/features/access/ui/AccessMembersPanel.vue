<script setup lang="ts">
import { Pencil, Search, ShieldX } from "@lucide/vue";
import { useI18n } from "vue-i18n";

import {
  membershipAllows,
  orderedProjectPermissions,
} from "@/features/access/ui/model";
import type { Membership } from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

defineProps<{
  memberships: readonly Membership[];
  organizationScope: boolean;
  search: string;
  busy: boolean;
}>();

const emit = defineEmits<{
  "update:search": [value: string];
  edit: [membership: Membership];
  revoke: [membership: Membership];
}>();

const { t } = useI18n();
</script>

<template>
  <section class="members-panel" aria-labelledby="members-title">
    <header class="panel-heading">
      <div>
        <h2 id="members-title">{{ t("accessRedesign.membersTitle") }}</h2>
        <p>
          {{
            t(
              organizationScope
                ? "accessRedesign.organizationMembersDescription"
                : "accessRedesign.projectMembersDescription",
            )
          }}
        </p>
      </div>
      <span class="result-count">{{
        t("accessRedesign.memberCount", { count: memberships.length })
      }}</span>
    </header>

    <div class="members-toolbar">
      <label class="search-field">
        <Search :size="16" aria-hidden="true" />
        <span class="sr-only">{{ t("access.searchMember") }}</span>
        <input
          type="search"
          :value="search"
          :placeholder="t('access.searchMemberPlaceholder')"
          @input="
            emit('update:search', ($event.target as HTMLInputElement).value)
          "
        />
      </label>
      <span class="source-chip">OIDC</span>
    </div>

    <div v-if="memberships.length" class="members-table" role="list">
      <article
        v-for="membership in memberships"
        :key="membership.ref"
        class="member-row entity-row"
        role="listitem"
      >
        <div class="member-identity">
          <span class="member-avatar" aria-hidden="true">{{
            membership.user.displayName
              .split(" ")
              .slice(0, 2)
              .map((part) => part[0])
              .join("")
          }}</span>
          <div>
            <h3>{{ membership.user.displayName }}</h3>
            <p>{{ membership.user.emailHint ?? t("common.noData") }}</p>
          </div>
        </div>

        <div class="role-cell">
          <strong>{{ t(`access.roles.${membership.platformRole}`) }}</strong>
          <span>{{ t("accessRedesign.systemRole") }}</span>
        </div>

        <div class="permission-cell">
          <template v-if="!organizationScope">
            <span
              v-for="permission in orderedProjectPermissions(membership).slice(
                0,
                3,
              )"
              :key="permission"
              class="permission-token"
            >
              {{ t(`access.permission.${permission}`) }}
            </span>
            <span v-if="membership.permissions.length > 3" class="more-token"
              >+{{ membership.permissions.length - 3 }}</span
            >
          </template>
          <span v-else class="scope-copy">{{
            t("accessRedesign.projectBindingsUnavailable")
          }}</span>
        </div>

        <StatusBadge :state="membership.active ? 'ACTIVE' : 'DISABLED'" />

        <div class="member-actions">
          <button
            v-if="membershipAllows(membership, 'EDIT')"
            class="button"
            type="button"
            @click="emit('edit', membership)"
          >
            <Pencil :size="15" aria-hidden="true" />
            {{ t("common.edit") }}
          </button>
          <button
            v-if="membershipAllows(membership, 'REVOKE')"
            class="button button--danger"
            type="button"
            :disabled="busy"
            @click="emit('revoke', membership)"
          >
            <ShieldX :size="15" aria-hidden="true" />
            {{ t("access.revoke") }}
          </button>
        </div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.members-panel {
  display: grid;
  gap: 12px;
}
.panel-heading,
.members-toolbar,
.member-identity,
.member-actions,
.permission-cell {
  display: flex;
  align-items: center;
  gap: 9px;
}
.panel-heading,
.members-toolbar {
  justify-content: space-between;
}
.panel-heading {
  align-items: flex-start;
}
.panel-heading h2,
.panel-heading p,
.member-identity h3,
.member-identity p {
  margin-bottom: 0;
}
.panel-heading p,
.member-identity p,
.role-cell span,
.result-count,
.scope-copy {
  color: var(--muted);
}
.members-toolbar {
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.search-field {
  position: relative;
  flex: 0 1 360px;
  min-width: 180px;
}
.search-field > svg {
  position: absolute;
  top: 50%;
  left: 10px;
  color: var(--subtle);
  transform: translateY(-50%);
}
.search-field input {
  padding-left: 34px;
}
.source-chip,
.permission-token,
.more-token {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  padding: 3px 7px;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--muted);
  background: var(--surface);
  font-size: 0.75rem;
}
.source-chip {
  font-family: var(--font-mono);
}
.members-table {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.member-row {
  display: grid;
  grid-template-columns:
    minmax(210px, 1fr) minmax(130px, 0.5fr) minmax(230px, 1fr)
    auto auto;
  align-items: center;
  gap: 14px;
  min-height: 76px;
  padding: 10px 13px;
  border-top: 1px solid var(--hairline);
}
.member-row:first-child {
  border-top: 0;
}
.member-identity,
.member-identity > div,
.role-cell {
  min-width: 0;
}
.member-avatar {
  display: inline-grid;
  place-items: center;
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  border-radius: 50%;
  color: var(--accent-strong);
  background: var(--accent-soft);
  font-weight: 600;
}
.member-identity h3,
.member-identity p {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.role-cell {
  display: grid;
  gap: 2px;
}
.role-cell span,
.scope-copy {
  font-size: 0.76rem;
}
.permission-cell {
  flex-wrap: wrap;
}
.member-actions {
  justify-content: flex-end;
  flex-wrap: wrap;
}
@media (max-width: 1050px) {
  .member-row {
    grid-template-columns: minmax(210px, 1fr) minmax(130px, 0.6fr) auto;
  }
  .permission-cell,
  .member-actions {
    grid-column: 1 / -1;
  }
  .member-actions {
    justify-content: flex-start;
  }
}
@media (max-width: 640px) {
  .panel-heading,
  .members-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .member-row {
    grid-template-columns: 1fr auto;
  }
  .role-cell {
    grid-column: 1 / 2;
  }
  .permission-cell,
  .member-actions {
    grid-column: 1 / -1;
  }
  .member-actions .button {
    flex: 1;
  }
}
</style>

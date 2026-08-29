<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";

import {
  membershipForSubject,
  subjectBindings,
  uniquePermissionKeys,
} from "@/features/access/model";

import type {
  AccessBinding,
  AccessSubject,
  Membership,
  OidcGroup,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  subjects: AccessSubject[];
  groups: OidcGroup[];
  bindings: AccessBinding[];
  platformMemberships: Membership[];
  projectMemberships: Membership[];
  projectRef?: string;
  platformMembershipsUnavailable?: boolean;
  projectMembershipsUnavailable?: boolean;
  loading?: boolean;
  problem?: AppProblem;
  hasMore?: boolean;
}>();
const emit = defineEmits<{
  search: [query: string];
  more: [query: string];
  bind: [subject: AccessSubject];
  retry: [];
}>();
const query = ref("");
let timer: ReturnType<typeof setTimeout> | undefined;

const groupNames = computed(
  () => new Map(props.groups.map((group) => [group.ref, group.displayName])),
);
function bindingCount(subject: AccessSubject): number {
  return subjectBindings(subject, props.bindings).length;
}
function groupsFor(subject: AccessSubject): string {
  return subject.oidcGroupRefs
    .map((ref) => groupNames.value.get(ref))
    .filter(Boolean)
    .join(", ");
}

function platformRole(subject: AccessSubject): Membership["platformRole"] | "" {
  return (
    membershipForSubject(subject, props.platformMemberships)?.platformRole ?? ""
  );
}

function projectMembership(subject: AccessSubject): Membership | undefined {
  return membershipForSubject(subject, props.projectMemberships);
}

function scopedBindings(subject: AccessSubject): AccessBinding[] {
  return subjectBindings(subject, props.bindings).filter(
    (binding) =>
      binding.scope.kind === "RESOURCE_KIND" ||
      binding.scope.kind === "RESOURCE_INSTANCE",
  );
}

function assignedRoleNames(subject: AccessSubject): string[] {
  const roleNames = subjectBindings(subject, props.bindings)
    .filter((binding) => binding.scope.kind !== "ORGANIZATION")
    .map((binding) => binding.roleVersion.name);
  return [...new Set(roleNames)].slice(0, 2);
}

function permissionCount(subject: AccessSubject): number {
  const membership = projectMembership(subject);
  if (membership) return membership.permissions.length;
  return uniquePermissionKeys(subjectBindings(subject, props.bindings)).length;
}

watch(query, (value) => {
  if (timer) clearTimeout(timer);
  timer = setTimeout(() => emit("search", value.trim()), 250);
});
onBeforeUnmount(() => {
  if (timer) clearTimeout(timer);
});
</script>

<template>
  <section>
    <header class="section-toolbar">
      <div>
        <h2>{{ $t("access.participants.title") }}</h2>
        <p>
          {{
            $t(
              projectRef
                ? "access.participants.projectSubtitle"
                : "access.participants.subtitle",
            )
          }}
        </p>
      </div>
      <label class="search-field">
        <span class="sr-only">{{ $t("access.participants.search") }}</span>
        <input
          v-model="query"
          type="search"
          autocomplete="off"
          :placeholder="$t('access.participants.searchPlaceholder')"
        />
      </label>
    </header>

    <AsyncState
      :loading="loading"
      :problem="problem"
      :empty="subjects.length === 0"
      :empty-title="$t('access.participants.empty')"
      :empty-text="$t('access.participants.emptyHint')"
      @retry="emit('retry')"
    >
      <div class="access-table" role="table">
        <div class="access-table__head" role="row">
          <span>{{ $t("access.participants.participant") }}</span>
          <span>{{ $t("access.participants.identity") }}</span>
          <span>{{ $t("access.participants.platformRole") }}</span>
          <span>{{ $t("access.participants.projectAccess") }}</span>
          <span>{{ $t("common.status") }}</span>
          <span class="sr-only">{{ $t("common.actions") }}</span>
        </div>
        <article
          v-for="subject in subjects"
          :key="subject.ref"
          class="access-table__row"
          role="row"
        >
          <div>
            <strong>{{ subject.displayName }}</strong>
            <small>{{ $t(`access.subjectKinds.${subject.kind}`) }}</small>
          </div>
          <div>
            <span v-if="groupsFor(subject)">{{ groupsFor(subject) }}</span>
            <span v-else class="muted">{{
              $t("access.participants.directIdentity")
            }}</span>
          </div>
          <div class="assignment-summary">
            <span v-if="platformMembershipsUnavailable" class="unavailable">{{
              $t("access.participants.presentationUnavailable")
            }}</span>
            <strong v-else-if="platformRole(subject)">{{
              $t(`access.platformRoles.${platformRole(subject)}`)
            }}</strong>
            <span v-else class="muted">{{
              $t("access.participants.noPlatformRole")
            }}</span>
            <small>{{ $t("access.participants.organizationWide") }}</small>
          </div>
          <div class="assignment-summary">
            <span
              v-if="projectRef && projectMembershipsUnavailable"
              class="unavailable"
              >{{ $t("access.participants.presentationUnavailable") }}</span
            >
            <template v-else-if="projectRef && projectMembership(subject)">
              <strong>{{
                $t("access.participants.permissionCount", {
                  count: permissionCount(subject),
                })
              }}</strong>
              <small v-if="scopedBindings(subject).length">{{
                $t("access.participants.scopedBindingCount", {
                  count: scopedBindings(subject).length,
                })
              }}</small>
            </template>
            <template v-else-if="assignedRoleNames(subject).length">
              <strong>{{ assignedRoleNames(subject).join(", ") }}</strong>
              <small>{{
                $t("access.participants.bindingCount", {
                  count: bindingCount(subject),
                })
              }}</small>
            </template>
            <span v-else class="muted">{{
              $t("access.participants.noProjectAccess")
            }}</span>
          </div>
          <StatusBadge :state="subject.active ? 'ACTIVE' : 'DISABLED'" />
          <button
            class="button"
            type="button"
            :disabled="!subject.active"
            @click="emit('bind', subject)"
          >
            {{ $t("access.participants.createBinding") }}
          </button>
        </article>
      </div>
      <button
        v-if="hasMore"
        class="button load-more"
        type="button"
        :disabled="loading"
        @click="emit('more', query.trim())"
      >
        {{ $t("access.loadMore") }}
      </button>
    </AsyncState>
  </section>
</template>

<style scoped>
.section-toolbar {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 18px;
  margin-bottom: 14px;
}
.section-toolbar h2,
.section-toolbar p {
  margin: 0;
}
.section-toolbar p,
.muted,
.access-table small {
  color: var(--muted);
}
.search-field {
  width: min(360px, 100%);
}
.access-table {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.access-table__head,
.access-table__row {
  display: grid;
  grid-template-columns:
    minmax(190px, 1.15fr) minmax(160px, 0.9fr)
    minmax(150px, 0.8fr) minmax(190px, 1.1fr) 105px auto;
  align-items: center;
  gap: 14px;
  padding: 10px 13px;
}
.access-table__head {
  color: var(--muted);
  background: #f4f6f8;
  font-size: 0.78rem;
  font-weight: 600;
}
.access-table__row + .access-table__row {
  border-top: 1px solid var(--border);
}
.access-table__row > div:first-child {
  min-width: 0;
}
.access-table__row small {
  display: block;
  margin-top: 2px;
}
.assignment-summary {
  min-width: 0;
}
.assignment-summary strong,
.assignment-summary small {
  display: block;
}
.assignment-summary strong {
  overflow-wrap: anywhere;
}
.assignment-summary .unavailable {
  display: block;
  width: max-content;
  padding: 2px 6px;
  border-radius: 6px;
  color: var(--warning);
  background: var(--warning-soft);
  font-size: 0.75rem;
}
.load-more {
  display: flex;
  margin: 14px auto 0;
}
@media (max-width: 840px) {
  .section-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .search-field {
    width: 100%;
  }
  .access-table__head {
    display: none;
  }
  .access-table__row {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .access-table__row > :nth-child(2),
  .access-table__row > :nth-child(3) {
    grid-column: 1 / -1;
  }
}
</style>

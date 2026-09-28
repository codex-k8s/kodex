<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";

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
  Project,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";

const props = defineProps<{
  subjects: AccessSubject[];
  groups: OidcGroup[];
  projects: Project[];
  bindings: AccessBinding[];
  platformMemberships: Membership[];
  projectMemberships: Membership[];
  projectRef?: string;
  initialQuery?: string;
  selectedSubjectRef?: string;
  platformMembershipsUnavailable?: boolean;
  projectMembershipsUnavailable?: boolean;
  loading?: boolean;
  problem?: AppProblem;
  hasMore?: boolean;
  mutationBusy?: boolean;
}>();
const emit = defineEmits<{
  search: [query: string, pageSize: number];
  more: [query: string, pageSize: number];
  bind: [subject: AccessSubject];
  "inspect-effective": [subject: AccessSubject];
  "edit-platform-membership": [membership: Membership];
  "revoke-platform-membership": [membership: Membership];
  "edit-membership": [membership: Membership];
  "revoke-membership": [membership: Membership];
  retry: [];
}>();
const query = ref(props.initialQuery ?? "");
const selectedRef = ref(props.selectedSubjectRef ?? "");
const searchId = useId();
let timer: ReturnType<typeof setTimeout> | undefined;
const listRoot = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: listRoot,
  itemSelector: ".access-table__row",
  itemCount: () => props.subjects.length,
  estimatedItemHeight: 88,
});
useCursorInfiniteScroll({
  root: listRoot,
  sentinel,
  enabled: () => props.hasMore && !props.loading,
  loadMore: () => emit("more", query.value.trim(), pageSize.value),
});

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

function platformMembership(subject: AccessSubject): Membership | undefined {
  return membershipForSubject(subject, props.platformMemberships);
}

function participantActive(subject: AccessSubject): boolean {
  if (!subject.active) return false;
  if (subject.kind !== "USER") return true;
  return platformMembership(subject)?.active === true;
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

const selectedSubject = computed(() =>
  props.subjects.find((subject) => subject.ref === selectedRef.value),
);
const selectedMembership = computed(() =>
  selectedSubject.value ? projectMembership(selectedSubject.value) : undefined,
);
const selectedPlatformMembership = computed(() =>
  selectedSubject.value
    ? membershipForSubject(selectedSubject.value, props.platformMemberships)
    : undefined,
);
const selectedBindings = computed(() =>
  selectedSubject.value
    ? subjectBindings(selectedSubject.value, props.bindings)
    : [],
);
function bindingSource(binding: AccessBinding): string {
  return binding.subject.kind === "OIDC_GROUP"
    ? (groupNames.value.get(binding.subject.ref) ?? binding.subject.displayName)
    : "";
}
function bindingScope(binding: AccessBinding): string {
  return (
    props.projects.find((project) => project.ref === binding.scope.projectRef)
      ?.name ?? ""
  );
}

function bindingPresentationKey(binding: AccessBinding): string {
  if (binding.managementKind === "PLATFORM_MEMBERSHIP") {
    return "access.bindingsWorkspace.assignmentKinds.PLATFORM_ROLE";
  }
  if (binding.managementKind === "PROJECT_MEMBERSHIP") {
    return "access.bindingsWorkspace.assignmentKinds.PROJECT_MEMBERSHIP";
  }
  return "access.participants.directBinding";
}

watch(
  [() => props.subjects, () => props.selectedSubjectRef],
  ([subjects, preferred]) => {
    if (preferred && subjects.some((subject) => subject.ref === preferred)) {
      selectedRef.value = preferred;
    } else if (!subjects.some((subject) => subject.ref === selectedRef.value)) {
      selectedRef.value =
        subjects.find((subject) => subject.kind === "USER")?.ref ??
        subjects[0]?.ref ??
        "";
    }
  },
  { immediate: true },
);

watch(query, (value) => {
  if (value === (props.initialQuery ?? "")) return;
  if (timer) clearTimeout(timer);
  timer = setTimeout(() => emit("search", value.trim(), pageSize.value), 250);
});
watch(
  () => props.initialQuery,
  (value) => {
    query.value = value ?? "";
  },
);
onBeforeUnmount(() => {
  if (timer) clearTimeout(timer);
});
</script>

<template>
  <section class="participants-workspace">
    <header class="section-toolbar">
      <label class="search-field" :for="searchId">
        <span class="sr-only">{{ $t("access.participants.search") }}</span>
        <input
          :id="searchId"
          v-model="query"
          :name="searchId"
          type="search"
          autocomplete="off"
          :placeholder="$t('access.participants.searchPlaceholder')"
        />
      </label>
      <span class="participants-count">{{
        $t("access.participants.loadedCount", { count: subjects.length })
      }}</span>
    </header>

    <AsyncState
      :loading="loading"
      :problem="problem"
      :empty="subjects.length === 0"
      :empty-title="
        $t(
          query
            ? 'access.participants.searchEmpty'
            : 'access.participants.empty',
        )
      "
      :empty-text="
        $t(
          query
            ? 'access.participants.searchEmptyHint'
            : 'access.participants.emptyHint',
        )
      "
      @retry="emit('retry')"
    >
      <div class="participants-layout">
        <div
          ref="listRoot"
          class="access-table"
          role="table"
          :aria-label="$t('access.participants.title')"
        >
          <div class="access-table__head" role="row">
            <span>{{ $t("access.participants.participant") }}</span>
            <span>{{ $t("access.participants.platformRole") }}</span>
            <span>{{ $t("access.participants.identity") }}</span>
            <span>{{ $t("access.participants.projectAccess") }}</span>
            <span>{{ $t("common.status") }}</span>
            <span class="sr-only">{{ $t("common.actions") }}</span>
          </div>
          <article
            v-for="subject in subjects"
            :key="subject.ref"
            class="access-table__row"
            :class="{
              'access-table__row--selected': subject.ref === selectedRef,
            }"
            role="row"
            :aria-selected="subject.ref === selectedRef"
            tabindex="0"
            @click="selectedRef = subject.ref"
            @keydown.enter="selectedRef = subject.ref"
            @keydown.space.prevent="selectedRef = subject.ref"
          >
            <div>
              <strong>{{ subject.displayName }}</strong>
              <small
                >{{ $t(`access.subjectKinds.${subject.kind}`)
                }}<template v-if="subject.kind === 'USER'">
                  · {{ $t("access.participants.oidcSource") }}</template
                ></small
              >
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
            <div>
              <span v-if="groupsFor(subject)">{{ groupsFor(subject) }}</span>
              <span v-else class="muted">{{
                $t("access.participants.directIdentity")
              }}</span>
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
            <StatusBadge
              :state="participantActive(subject) ? 'ACTIVE' : 'DISABLED'"
            />
            <div class="access-table__actions">
              <button
                class="button"
                type="button"
                :aria-label="
                  $t('access.participants.inspectEffectiveFor', {
                    name: subject.displayName,
                  })
                "
                @click.stop="emit('inspect-effective', subject)"
              >
                {{ $t("access.participants.access") }}
              </button>
            </div>
          </article>
        </div>
        <aside
          class="participant-detail"
          :aria-label="$t('access.participants.selectedSubject')"
        >
          <template v-if="selectedSubject">
            <header class="participant-detail__header">
              <div>
                <h3>{{ selectedSubject.displayName }}</h3>
                <small
                  >{{ $t(`access.subjectKinds.${selectedSubject.kind}`)
                  }}<template v-if="selectedSubject.kind === 'USER'">
                    · {{ $t("access.participants.oidcSource") }}</template
                  ></small
                >
              </div>
              <StatusBadge
                :state="
                  participantActive(selectedSubject) ? 'ACTIVE' : 'DISABLED'
                "
              />
            </header>
            <div class="participant-detail__body">
              <dl>
                <div>
                  <dt>{{ $t("access.participants.platformRole") }}</dt>
                  <dd>
                    {{
                      platformRole(selectedSubject)
                        ? $t(
                            `access.platformRoles.${platformRole(selectedSubject)}`,
                          )
                        : $t("access.participants.noPlatformRole")
                    }}
                  </dd>
                </div>
                <div>
                  <dt>{{ $t("access.participants.identity") }}</dt>
                  <dd>
                    {{
                      groupsFor(selectedSubject) ||
                      $t("access.participants.directIdentity")
                    }}
                  </dd>
                </div>
                <div v-if="projectRef">
                  <dt>{{ $t("access.participants.projectAccess") }}</dt>
                  <dd>
                    {{
                      selectedMembership
                        ? $t("access.participants.permissionCount", {
                            count: selectedMembership.permissions.length,
                          })
                        : $t("access.participants.noProjectAccess")
                    }}
                  </dd>
                </div>
              </dl>
              <h4>
                {{ $t("access.participants.loadedBindings") }} ·
                {{ selectedBindings.length }}
              </h4>
              <ul v-if="selectedBindings.length" class="participant-bindings">
                <li v-for="binding in selectedBindings" :key="binding.ref">
                  <strong>{{ binding.roleVersion.name }}</strong>
                  <span>{{
                    bindingScope(binding) ||
                    $t(`access.scope.values.${binding.scope.kind}`)
                  }}</span>
                  <small>{{
                    bindingSource(binding) ||
                    $t(bindingPresentationKey(binding))
                  }}</small>
                </li>
              </ul>
              <p v-else class="muted">
                {{ $t("access.participants.noBindings") }}
              </p>
            </div>
            <footer class="participant-detail__actions">
              <button
                class="button"
                type="button"
                @click="emit('inspect-effective', selectedSubject)"
              >
                {{ $t("access.sections.effective") }}
              </button>
              <button
                v-if="
                  !projectRef &&
                  selectedPlatformMembership?.nextActions.includes('EDIT')
                "
                class="button"
                type="button"
                :disabled="mutationBusy"
                @click="
                  emit('edit-platform-membership', selectedPlatformMembership!)
                "
              >
                {{ $t("access.participants.editPlatformRole") }}
              </button>
              <button
                v-if="
                  !projectRef &&
                  selectedPlatformMembership?.nextActions.includes('REVOKE')
                "
                class="button button--danger"
                type="button"
                :disabled="mutationBusy"
                @click="
                  emit(
                    'revoke-platform-membership',
                    selectedPlatformMembership!,
                  )
                "
              >
                {{ $t("access.participants.removeFromOrganization") }}
              </button>
              <button
                class="button button--primary"
                type="button"
                :disabled="!participantActive(selectedSubject) || mutationBusy"
                @click="emit('bind', selectedSubject)"
              >
                {{ $t("access.participants.createBinding") }}
              </button>
              <button
                v-if="
                  projectRef && selectedMembership?.nextActions.includes('EDIT')
                "
                class="button"
                type="button"
                :disabled="mutationBusy"
                @click="emit('edit-membership', selectedMembership!)"
              >
                {{ $t("access.projectMembershipEditor.edit") }}
              </button>
              <button
                v-if="
                  projectRef &&
                  selectedMembership?.nextActions.includes('REVOKE')
                "
                class="button button--danger"
                type="button"
                :disabled="mutationBusy"
                @click="emit('revoke-membership', selectedMembership!)"
              >
                {{ $t("access.projectMembershipEditor.revoke") }}
              </button>
            </footer>
          </template>
          <p v-else class="muted">{{ $t("access.participants.selectHint") }}</p>
        </aside>
      </div>
      <div v-if="hasMore" ref="sentinel" class="cursor-sentinel" />
    </AsyncState>
  </section>
</template>

<style scoped>
.participants-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  align-items: start;
  gap: 16px;
  min-width: 0;
}
.participant-detail {
  display: flex;
  flex-direction: column;
  min-width: 0;
  max-height: calc(100vh - 355px);
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.participant-detail__header,
.participant-detail__body,
.participant-detail__actions {
  padding: 14px 16px;
}
.participant-detail__header,
.participant-detail__actions {
  flex: 0 0 auto;
}
.participant-detail__body {
  min-height: 0;
  overflow-y: auto;
}
.participant-detail__header {
  display: flex;
  align-items: start;
  justify-content: space-between;
  gap: 10px;
  border-bottom: 1px solid var(--border);
}
.participant-detail__header h3 {
  margin: 0;
  font-size: 1rem;
  overflow-wrap: anywhere;
}
.participant-detail__header small {
  color: var(--muted);
}
.participant-detail__body dl {
  margin: 0;
}
.participant-detail__body dl > div {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  padding: 9px 0;
  border-bottom: 1px solid var(--border);
}
.participant-detail__body dt {
  color: var(--muted);
}
.participant-detail__body dd {
  margin: 0;
  text-align: right;
  overflow-wrap: anywhere;
}
.participant-detail__body h4 {
  margin: 18px 0 8px;
  font-size: 0.85rem;
}
.participant-bindings {
  padding: 0;
  margin: 0;
  list-style: none;
}
.participant-bindings li {
  display: grid;
  gap: 3px;
  padding: 9px 0;
  border-top: 1px solid var(--border);
}
.participant-bindings li span,
.participant-bindings li small {
  color: var(--muted);
}
.participant-detail__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  border-top: 1px solid var(--border);
}
.participants-count {
  color: var(--muted);
  font-size: 0.82rem;
}
.access-table__row--selected {
  background: var(--accent-soft);
  box-shadow: inset 3px 0 var(--accent);
}
.access-table__row {
  cursor: pointer;
}
.access-table__row:hover:not(.access-table__row--selected) {
  background: var(--panel);
}
.access-table__row:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}
.section-toolbar {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 18px;
  margin-bottom: 12px;
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
    minmax(150px, 1.1fr) minmax(130px, 0.8fr)
    minmax(130px, 0.8fr) minmax(145px, 1fr) 96px 100px;
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
.access-table__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  justify-content: flex-end;
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
  .participants-layout {
    grid-template-columns: 1fr;
  }
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

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";

import type {
  AccessBinding,
  AccessSubject,
  OidcGroup,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  subjects: AccessSubject[];
  groups: OidcGroup[];
  bindings: AccessBinding[];
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
  return props.bindings.filter(
    (binding) =>
      binding.state === "ACTIVE" && binding.subject.ref === subject.ref,
  ).length;
}
function groupsFor(subject: AccessSubject): string {
  return subject.oidcGroupRefs
    .map((ref) => groupNames.value.get(ref))
    .filter(Boolean)
    .join(", ");
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
        <p>{{ $t("access.participants.subtitle") }}</p>
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
          <span>{{ $t("access.participants.bindings") }}</span>
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
          <span>{{ bindingCount(subject) }}</span>
          <StatusBadge :state="subject.active ? 'ACTIVE' : 'DISABLED'" />
          <button
            class="button"
            type="button"
            :disabled="!subject.active"
            @click="emit('bind', subject)"
          >
            {{ $t("access.participants.assignRole") }}
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
    minmax(200px, 1.3fr) minmax(180px, 1fr)
    100px 110px auto;
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

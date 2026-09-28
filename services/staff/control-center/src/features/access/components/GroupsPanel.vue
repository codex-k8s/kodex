<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";

import type {
  AccessBinding,
  OidcGroup,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";

const props = defineProps<{
  groups: OidcGroup[];
  bindings: AccessBinding[];
  bindingsUnavailable?: boolean;
  loading?: boolean;
  problem?: AppProblem;
  hasMore?: boolean;
}>();
const emit = defineEmits<{
  search: [query: string, pageSize: number];
  more: [query: string, pageSize: number];
  retry: [];
  bind: [group: OidcGroup];
}>();
const query = ref("");
const selectedRef = ref("");
let timer: ReturnType<typeof setTimeout> | undefined;
const listRoot = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: listRoot,
  itemSelector: ".group-table__row",
  itemCount: () => props.groups.length,
  estimatedItemHeight: 62,
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

function mappings(group: OidcGroup): AccessBinding[] {
  return props.bindings.filter(
    (binding) =>
      binding.state === "ACTIVE" && binding.subject.ref === group.ref,
  );
}
const selectedGroup = computed(() =>
  props.groups.find((group) => group.ref === selectedRef.value),
);
watch(
  () => props.groups,
  (groups) => {
    if (!groups.some((group) => group.ref === selectedRef.value))
      selectedRef.value = groups[0]?.ref ?? "";
  },
  { immediate: true },
);
</script>

<template>
  <section class="groups-workspace">
    <section class="oidc-note">
      <strong>{{ $t("access.groups.authorityTitle") }}</strong>
      <p>{{ $t("access.groups.authorityHint") }}</p>
    </section>
    <header class="section-toolbar">
      <input
        v-model="query"
        class="group-search"
        name="access-oidc-group-search"
        type="search"
        autocomplete="off"
        :placeholder="$t('access.groups.searchPlaceholder')"
        :aria-label="$t('access.groups.search')"
      />
      <span>{{
        $t("access.groups.loadedCount", { count: groups.length })
      }}</span>
    </header>
    <AsyncState
      :loading="loading"
      :problem="problem"
      :empty="groups.length === 0"
      :empty-title="
        $t(query ? 'access.groups.searchEmpty' : 'access.groups.empty')
      "
      :empty-text="
        $t(query ? 'access.groups.searchEmptyHint' : 'access.groups.emptyHint')
      "
      @retry="emit('retry')"
    >
      <div class="groups-layout">
        <div
          ref="listRoot"
          class="group-table"
          role="table"
          :aria-label="$t('access.groups.title')"
        >
          <div class="group-table__head" role="row">
            <span>{{ $t("access.groups.title") }}</span>
            <span>{{ $t("access.groups.members") }}</span>
            <span>{{ $t("access.groups.syncedAt") }}</span>
            <span>{{ $t("access.groups.bindings") }}</span>
            <span>{{ $t("common.status") }}</span>
            <span class="sr-only">{{ $t("common.actions") }}</span>
          </div>
          <div
            v-for="group in groups"
            :key="group.ref"
            class="group-table__row"
            :class="{ 'group-table__row--selected': group.ref === selectedRef }"
            role="row"
            :aria-selected="group.ref === selectedRef"
          >
            <strong>{{ group.displayName }}</strong>
            <span>{{ group.memberCount }}</span>
            <span>{{ new Date(group.syncedAt).toLocaleString() }}</span>
            <span>{{ group.bindingCount }}</span>
            <StatusBadge :state="group.state" />
            <button
              class="button"
              type="button"
              :aria-label="
                $t('access.groups.selectGroup', { name: group.displayName })
              "
              @click="selectedRef = group.ref"
            >
              {{ $t("access.participants.details") }}
            </button>
          </div>
        </div>
        <aside
          class="group-detail"
          :aria-label="$t('access.groups.selectedGroup')"
        >
          <template v-if="selectedGroup">
            <header class="group-detail__head">
              <div>
                <h3>{{ selectedGroup.displayName }}</h3>
                <small>{{ $t("access.groups.oidcSource") }}</small>
              </div>
              <StatusBadge :state="selectedGroup.state" />
            </header>
            <div class="group-detail__body">
              <dl>
                <div>
                  <dt>{{ $t("access.groups.members") }}</dt>
                  <dd>{{ selectedGroup.memberCount }}</dd>
                </div>
                <div>
                  <dt>{{ $t("access.groups.bindings") }}</dt>
                  <dd>{{ selectedGroup.bindingCount }}</dd>
                </div>
                <div>
                  <dt>{{ $t("access.groups.syncedAt") }}</dt>
                  <dd>
                    {{ new Date(selectedGroup.syncedAt).toLocaleString() }}
                  </dd>
                </div>
                <div>
                  <dt>{{ $t("access.groups.lastSeen") }}</dt>
                  <dd>
                    {{ new Date(selectedGroup.lastSeenAt).toLocaleString() }}
                  </dd>
                </div>
              </dl>
              <h4>{{ $t("access.groups.roleMappings") }}</h4>
              <p v-if="bindingsUnavailable" class="unavailable">
                {{ $t("access.groups.bindingsUnavailable") }}
              </p>
              <ul
                v-else-if="mappings(selectedGroup).length"
                class="group-mappings"
              >
                <li
                  v-for="binding in mappings(selectedGroup)"
                  :key="binding.ref"
                >
                  <strong>{{ binding.roleVersion.name }}</strong
                  ><small>{{
                    $t(`access.scope.values.${binding.scope.kind}`)
                  }}</small>
                </li>
              </ul>
              <p v-else class="muted">
                {{ $t("access.groups.noRoleMappings") }}
              </p>
            </div>
            <footer class="group-detail__foot">
              <button
                class="button button--primary"
                type="button"
                :disabled="selectedGroup.state !== 'ACTIVE'"
                @click="emit('bind', selectedGroup)"
              >
                {{ $t("access.groups.createMapping") }}
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
.oidc-note {
  margin-bottom: 12px;
  padding: 10px 14px;
  border-left: 3px solid var(--accent);
  border-radius: 6px;
  background: var(--accent-soft);
}
.oidc-note p {
  margin: 3px 0 0;
  color: var(--muted);
}
.section-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}
.section-toolbar span,
.group-detail small,
.muted,
dt {
  color: var(--muted);
}
.group-search {
  width: min(360px, 100%);
}
.groups-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 340px;
  gap: 16px;
  align-items: start;
  min-width: 0;
}
.group-table,
.group-detail {
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.group-table {
  overflow: hidden;
}
.group-table__head,
.group-table__row {
  display: grid;
  grid-template-columns:
    minmax(140px, 1.2fr) 90px minmax(150px, 0.9fr)
    90px 110px 100px;
  gap: 12px;
  align-items: center;
  padding: 10px 13px;
}
.group-table__head {
  color: var(--muted);
  background: #f4f6f8;
  font-size: 0.78rem;
  font-weight: 600;
}
.group-table__row {
  min-height: 58px;
}
.group-table__row + .group-table__row {
  border-top: 1px solid var(--border);
}
.group-table__row--selected {
  background: var(--accent-soft);
  box-shadow: inset 3px 0 var(--accent);
}
.group-table__row strong {
  overflow-wrap: anywhere;
}
.group-detail {
  display: flex;
  flex-direction: column;
  max-height: calc(100vh - 320px);
}
.group-detail__head,
.group-detail__body,
.group-detail__foot {
  padding: 14px 16px;
}
.group-detail__head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  border-bottom: 1px solid var(--border);
}
.group-detail__head h3 {
  margin: 0;
  font-size: 1rem;
  overflow-wrap: anywhere;
}
.group-detail__body {
  min-height: 0;
  overflow-y: auto;
}
.group-detail__body dl {
  margin: 0;
}
.group-detail__body dl > div {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  padding: 9px 0;
  border-bottom: 1px solid var(--border);
}
.group-detail__body dd {
  margin: 0;
  text-align: right;
  overflow-wrap: anywhere;
}
.group-detail__body h4 {
  margin: 16px 0 8px;
  font-size: 0.85rem;
}
.group-mappings {
  margin: 0;
  padding: 0;
  list-style: none;
}
.group-mappings li {
  display: grid;
  gap: 3px;
  padding: 9px 0;
  border-top: 1px solid var(--border);
}
.group-detail__foot {
  border-top: 1px solid var(--border);
}
.unavailable {
  color: var(--warning);
}
@media (max-width: 1000px) {
  .groups-layout {
    grid-template-columns: 1fr;
  }
  .group-table__head,
  .group-table__row {
    grid-template-columns:
      minmax(130px, 1fr) 75px minmax(130px, 1fr)
      70px 100px 100px;
  }
}
</style>

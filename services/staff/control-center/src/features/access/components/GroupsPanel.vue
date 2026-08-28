<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";

import type { OidcGroup } from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

defineProps<{
  groups: OidcGroup[];
  loading?: boolean;
  problem?: AppProblem;
  hasMore?: boolean;
}>();
const emit = defineEmits<{
  search: [query: string];
  more: [query: string];
  retry: [];
  bind: [group: OidcGroup];
}>();
const query = ref("");
let timer: ReturnType<typeof setTimeout> | undefined;
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
        <h2>{{ $t("access.groups.title") }}</h2>
        <p>{{ $t("access.groups.subtitle") }}</p>
      </div>
      <input
        v-model="query"
        class="group-search"
        type="search"
        autocomplete="off"
        :placeholder="$t('access.groups.searchPlaceholder')"
        :aria-label="$t('access.groups.search')"
      />
    </header>
    <section class="oidc-note">
      <strong>{{ $t("access.groups.authorityTitle") }}</strong>
      <p>{{ $t("access.groups.authorityHint") }}</p>
    </section>
    <AsyncState
      :loading="loading"
      :problem="problem"
      :empty="groups.length === 0"
      :empty-title="$t('access.groups.empty')"
      :empty-text="$t('access.groups.emptyHint')"
      @retry="emit('retry')"
    >
      <div class="group-grid">
        <article v-for="group in groups" :key="group.ref" class="group-card">
          <header>
            <div>
              <h3>{{ group.displayName }}</h3>
              <small>{{ $t("access.groups.oidcSource") }}</small>
            </div>
            <StatusBadge :state="group.state" />
          </header>
          <dl>
            <div>
              <dt>{{ $t("access.groups.members") }}</dt>
              <dd>{{ group.memberCount }}</dd>
            </div>
            <div>
              <dt>{{ $t("access.groups.bindings") }}</dt>
              <dd>{{ group.bindingCount }}</dd>
            </div>
            <div>
              <dt>{{ $t("access.groups.lastSeen") }}</dt>
              <dd>{{ new Date(group.lastSeenAt).toLocaleString() }}</dd>
            </div>
          </dl>
          <button
            class="button group-bind"
            type="button"
            :disabled="group.state !== 'ACTIVE'"
            @click="emit('bind', group)"
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
.section-toolbar,
.group-card header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.section-toolbar {
  align-items: end;
  margin-bottom: 12px;
}
.section-toolbar h2,
.section-toolbar p,
.group-card h3 {
  margin: 0;
}
.section-toolbar p,
.group-card small,
.oidc-note p,
dt {
  color: var(--muted);
}
.group-search {
  width: min(360px, 100%);
}
.oidc-note {
  margin-bottom: 14px;
  padding: 12px 14px;
  border: 1px solid #c8d8eb;
  border-radius: 8px;
  background: #f2f7fd;
}
.oidc-note p {
  margin: 4px 0 0;
}
.group-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(320px, 100%), 1fr));
  gap: 12px;
}
.group-card {
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.group-card dl {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin: 14px 0 0;
}
.group-bind {
  margin-top: 12px;
}
.group-card dt,
.group-card dd {
  margin: 0;
}
.group-card dd {
  margin-top: 3px;
  font-weight: 600;
}
.load-more {
  display: flex;
  margin: 14px auto 0;
}
@media (max-width: 720px) {
  .section-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .group-search {
    width: 100%;
  }
  .group-card dl {
    grid-template-columns: 1fr;
  }
}
</style>

<script setup lang="ts">
import { Search } from "@lucide/vue";
import { onMounted, reactive, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { useSearchStore } from "@/features/search/store";
import type { ResourceKind } from "@/shared/api/generated/openapi/types.gen";
import { resourceKinds } from "@/shared/lib/resources";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const route = useRoute();
const router = useRouter();
const store = useSearchStore();
const form = reactive({ kind: "ALL" as ResourceKind | "ALL", query: "" });
const kinds: readonly (ResourceKind | "ALL")[] = ["ALL", ...resourceKinds];

async function submit(): Promise<void> {
  const query = form.query.trim();
  if (query.length < 2) return;
  await router.replace({ name: "search", query: { kind: form.kind, query } });
  await store.search(form.kind, query);
}

function syncRoute(): void {
  const kind = String(route.query.kind ?? "ALL") as ResourceKind | "ALL";
  form.kind = kinds.includes(kind) ? kind : "ALL";
  form.query = String(route.query.query ?? "");
  if (form.query.trim().length >= 2)
    void store.search(form.kind, form.query.trim());
}
watch(() => route.query, syncRoute);
onMounted(syncRoute);
</script>

<template>
  <div class="page">
    <PageHeader :title="$t('search.title')" :subtitle="$t('search.subtitle')" />
    <section class="panel">
      <div class="panel__body">
        <form class="form-grid" @submit.prevent="submit">
          <label class="form-field"
            ><span>{{ $t("search.kind") }}</span
            ><select v-model="form.kind">
              <option v-for="kind in kinds" :key="kind" :value="kind">
                {{ kind }}
              </option>
            </select></label
          ><label class="form-field"
            ><span>{{ $t("search.query") }}</span
            ><input
              v-model="form.query"
              type="search"
              required
              minlength="2"
              maxlength="128"
              :placeholder="$t('search.placeholder')"
          /></label>
          <div class="form-field form-field--full">
            <button class="button button--primary" type="submit">
              <Search :size="15" aria-hidden="true" />{{ $t("common.search") }}
            </button>
          </div>
        </form>
      </div>
    </section>
    <section class="panel" style="margin-top: 18px">
      <header class="panel__header">
        <h2>{{ $t("search.results") }}</h2>
      </header>
      <AsyncPanel
        :phase="store.results.phase"
        :problem="store.results.problem"
        @retry="submit"
        ><div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ $t("common.name") }}</th>
                <th>{{ $t("search.kind") }}</th>
                <th>{{ $t("common.state") }}</th>
                <th>{{ $t("common.revision") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in store.results.data" :key="item.key">
                <td class="data-table__name">{{ item.name }}</td>
                <td>{{ item.kind }}</td>
                <td><StatusBadge :state="item.state" /></td>
                <td>{{ item.version }}</td>
              </tr>
            </tbody>
          </table>
        </div></AsyncPanel
      >
    </section>
  </div>
</template>

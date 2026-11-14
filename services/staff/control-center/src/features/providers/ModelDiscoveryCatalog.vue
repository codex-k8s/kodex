<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { useModelDiscovery } from "./model-discovery";
import { modelDiscoveryMessages } from "./model-discovery-messages";

const { t } = useI18n({ useScope: "local", messages: modelDiscoveryMessages });
const {
  query,
  items,
  source,
  total,
  nextPageToken,
  loading,
  failed,
  loaded,
  search,
} = useModelDiscovery();

function opened(event: Event): void {
  if (
    (event.currentTarget as HTMLDetailsElement).open &&
    !loaded.value &&
    !loading.value
  )
    void search();
}
</script>

<template>
  <details class="model-discovery" @toggle="opened">
    <summary>{{ t("title") }}</summary>
    <p>{{ t("notice") }}</p>
    <p>{{ t("zai") }}</p>
    <form class="model-discovery__search" @submit.prevent="search()">
      <label>
        <span>{{ t("search") }}</span>
        <input v-model="query" type="search" maxlength="200" />
      </label>
      <button type="submit" :disabled="loading">{{ t("find") }}</button>
    </form>
    <p v-if="failed" role="alert">{{ t("failed") }}</p>
    <p v-if="loading" role="status">{{ t("loading") }}</p>
    <p v-if="loaded && !items.length && !loading">{{ t("empty") }}</p>
    <template v-if="source">
      <p>{{ t("total", { count: total }) }}</p>
      <details class="model-discovery__source">
        <summary>{{ t("source") }}</summary>
        <p>models.dev · MIT · {{ source.upstreamSchema }}</p>
        <code>{{ source.sourceRevision }}</code>
        <code>{{ source.catalogSha256 }}</code>
      </details>
    </template>
    <ul class="model-discovery__models">
      <li v-for="model in items" :key="`${model.providerId}/${model.modelId}`">
        <h3>{{ model.name }}</h3>
        <p>
          {{ model.providerName }} · <code>{{ model.modelId }}</code>
        </p>
        <p>
          {{ t("context") }}: {{ model.contextTokens }} /
          {{ model.outputTokens }}
        </p>
        <p>{{ t(model.routeConfigured ? "route" : "noRoute") }}</p>
        <dl>
          <template
            v-for="dimension in [
              'known',
              'adapter',
              'account',
              'allowed',
              'tested',
            ] as const"
            :key="dimension"
          >
            <dt>{{ t(dimension) }}</dt>
            <dd>{{ t(`statuses.${model[dimension]}`) }}</dd>
          </template>
        </dl>
      </li>
    </ul>
    <button
      v-if="nextPageToken"
      type="button"
      :disabled="loading"
      @click="search(true)"
    >
      {{ t("more") }}
    </button>
  </details>
</template>

<style scoped>
.model-discovery {
  margin-block: 1rem;
  padding: 1rem;
  border: 1px solid var(--border-color, #d9dce4);
  border-radius: 0.75rem;
}
.model-discovery__search {
  display: flex;
  align-items: end;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.model-discovery__search label {
  display: grid;
  gap: 0.4rem;
}
.model-discovery__models {
  list-style: none;
  padding: 0;
  display: grid;
  gap: 1rem;
}
.model-discovery__models li {
  border-block-start: 1px solid var(--border-color, #d9dce4);
  padding-block-start: 1rem;
}
dl {
  display: grid;
  grid-template-columns: minmax(8rem, 1fr) 2fr;
  gap: 0.4rem 1rem;
}
dd {
  margin: 0;
}
code {
  overflow-wrap: anywhere;
}
.model-discovery__source code {
  display: block;
}
</style>

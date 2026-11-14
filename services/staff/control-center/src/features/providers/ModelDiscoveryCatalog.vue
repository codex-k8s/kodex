<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { useModelDiscovery } from "./model-discovery";

const { t } = useI18n();
const {
  query,
  filters,
  groups,
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
    <summary>{{ t("modelDiscovery.title") }}</summary>
    <p>{{ t("modelDiscovery.notice") }}</p>
    <p>{{ t("modelDiscovery.zai") }}</p>
    <form class="model-discovery__search" @submit.prevent="search()">
      <label>
        <span>{{ t("modelDiscovery.search") }}</span>
        <input v-model="query" type="search" maxlength="200" />
      </label>
      <button type="submit" :disabled="loading">
        {{ t("modelDiscovery.find") }}
      </button>
      <fieldset class="model-discovery__filters">
        <label
          ><span>{{ t("modelDiscovery.provider") }}</span
          ><input v-model="filters.providerId" maxlength="128"
        /></label>
        <label
          ><span>{{ t("modelDiscovery.family") }}</span
          ><input v-model="filters.family" maxlength="128"
        /></label>
        <label
          ><span>{{ t("modelDiscovery.minimumContext") }}</span
          ><input
            v-model.number="filters.minimumContextTokens"
            required
            type="number"
            min="0"
            max="1000000000000"
            step="1"
        /></label>
        <label
          ><span>{{ t("modelDiscovery.inputModality") }}</span
          ><input
            v-model="filters.inputModality"
            maxlength="128"
            placeholder="text, image, image/png"
        /></label>
        <label
          ><span>{{ t("modelDiscovery.outputModality") }}</span
          ><input
            v-model="filters.outputModality"
            maxlength="128"
            placeholder="text, image, audio"
        /></label>
        <label
          v-for="dimension in ['tools', 'reasoning'] as const"
          :key="dimension"
          ><span>{{ t(`modelDiscovery.${dimension}`) }}</span
          ><select v-model="filters[dimension]">
            <option value="">{{ t("modelDiscovery.any") }}</option>
            <option
              v-for="state in ['SUPPORTED', 'UNSUPPORTED', 'UNKNOWN']"
              :key="state"
              :value="state"
            >
              {{ t(`modelDiscovery.capabilityStates.${state}`) }}
            </option>
          </select></label
        >
        <label
          ><span>{{ t("modelDiscovery.cache") }}</span
          ><select v-model="filters.cache">
            <option value="">{{ t("modelDiscovery.any") }}</option>
            <option
              v-for="kind in [
                'NONE',
                'IMPLICIT',
                'EXPLICIT',
                'BOTH',
                'UNKNOWN',
              ]"
              :key="kind"
              :value="kind"
            >
              {{ t(`modelDiscovery.cacheKinds.${kind}`) }}
            </option>
          </select></label
        >
        <label
          ><span>{{ t("modelDiscovery.groupBy") }}</span
          ><select v-model="filters.groupBy">
            <option value="PROVIDER">
              {{ t("modelDiscovery.providerGroup") }}
            </option>
            <option value="FAMILY">
              {{ t("modelDiscovery.familyGroup") }}
            </option>
          </select></label
        >
      </fieldset>
    </form>
    <p v-if="failed" role="alert">{{ t("modelDiscovery.failed") }}</p>
    <p v-if="loading" role="status">{{ t("modelDiscovery.loading") }}</p>
    <p v-if="loaded && !items.length && !loading">
      {{ t("modelDiscovery.empty") }}
    </p>
    <template v-if="source">
      <p>{{ t("modelDiscovery.total", { count: total }) }}</p>
      <details class="model-discovery__source">
        <summary>{{ t("modelDiscovery.source") }}</summary>
        <p>models.dev · MIT · {{ source.upstreamSchema }}</p>
        <code>{{ source.sourceRevision }}</code>
        <code>{{ source.catalogSha256 }}</code>
        <p>
          {{ t("modelDiscovery.retrievedAt") }}:
          <time :datetime="source.retrievedAt">{{ source.retrievedAt }}</time>
        </p>
        <p>{{ t("modelDiscovery.projectionOnly") }}</p>
      </details>
    </template>
    <section
      v-for="group in groups"
      :key="group.id"
      class="model-discovery__group"
    >
      <h3>
        {{
          group.label === "UNKNOWN" ? t("modelDiscovery.unknown") : group.label
        }}
      </h3>
      <ul class="model-discovery__models">
        <li
          v-for="model in group.items"
          :key="`${model.providerId}/${model.modelId}`"
        >
          <h4>{{ model.name }}</h4>
          <p>
            {{ model.providerName }} · <code>{{ model.providerId }}</code> /
            <code>{{ model.modelId }}</code>
          </p>
          <p>
            {{ t("modelDiscovery.context") }}:
            {{
              model.contextKnown
                ? model.contextTokens
                : t("modelDiscovery.unknown")
            }}
            /
            {{
              model.outputKnown
                ? model.outputTokens
                : t("modelDiscovery.unknown")
            }}
          </p>
          <p>
            {{ t("modelDiscovery.family") }}:
            {{
              model.family === "UNKNOWN"
                ? t("modelDiscovery.unknown")
                : model.family
            }}
          </p>
          <p>
            {{ t("modelDiscovery.tools") }}:
            {{ t(`modelDiscovery.capabilityStates.${model.tools}`) }} ·
            {{ t("modelDiscovery.reasoning") }}:
            {{ t(`modelDiscovery.capabilityStates.${model.reasoning}`) }}
          </p>
          <p>
            {{ t("modelDiscovery.cache") }}:
            {{ t(`modelDiscovery.cacheKinds.${model.cache}`) }}
          </p>
          <p>
            {{ t("modelDiscovery.modalities") }}:
            {{
              model.inputModalities.join(", ") || t("modelDiscovery.unknown")
            }}
            /
            {{
              model.outputModalities.join(", ") || t("modelDiscovery.unknown")
            }}
          </p>
          <p>
            {{
              t(
                model.routeConfigured
                  ? "modelDiscovery.route"
                  : "modelDiscovery.noRoute",
              )
            }}
          </p>
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
              <dt>{{ t(`modelDiscovery.${dimension}`) }}</dt>
              <dd>{{ t(`modelDiscovery.statuses.${model[dimension]}`) }}</dd>
            </template>
          </dl>
        </li>
      </ul>
    </section>
    <button
      v-if="nextPageToken"
      type="button"
      :disabled="loading"
      @click="search(true)"
    >
      {{ t("modelDiscovery.more") }}
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
.model-discovery__filters {
  display: grid;
  width: 100%;
  grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr));
  gap: 0.75rem;
  border: 0;
  padding: 0;
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

<script setup lang="ts">
import { Download, RefreshCw } from "@lucide/vue";
import { onBeforeUnmount, watch } from "vue";
import { useI18n } from "vue-i18n";
import type {
  Artifact,
  ArtifactRevision,
} from "@/shared/api/generated/openapi/types.gen";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { createRevisionHistory } from "./revision-history";

const props = defineProps<{ artifact: Artifact }>();
const { t, locale } = useI18n();
const history = createRevisionHistory();
const { items, loading, loaded, nextPageToken, error, downloading } = history;
function refresh() {
  history.reset(props.artifact);
  void history.load();
}
watch(
  () => [
    props.artifact.ref,
    props.artifact.projectRef,
    props.artifact.version,
    props.artifact.lifecycleState,
    props.artifact.nextActions.join(","),
  ],
  refresh,
  { immediate: true },
);
onBeforeUnmount(history.dispose);
function date(value: string) {
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "short",
    timeStyle: "short",
  }).format(new Date(value));
}
async function download(value: ArtifactRevision) {
  const body = await history.download(value);
  if (!body) return;
  const url = URL.createObjectURL(body);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = value.fileName;
  anchor.hidden = true;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
</script>

<template>
  <section class="artifact-history" :aria-label="t('fileRevision.history')">
    <header>
      <h3>{{ t("fileRevision.history") }}</h3>
      <button
        class="button"
        type="button"
        :disabled="loading"
        :aria-label="t('common.refresh')"
        :title="t('common.refresh')"
        @click="refresh"
      >
        <RefreshCw :size="15" aria-hidden="true" />
      </button>
    </header>
    <ProblemNotice v-if="error" :problem="error" @retry="refresh" />
    <p v-if="loading && !items.length" role="status">
      {{ t("common.loading") }}
    </p>
    <p v-else-if="loaded && !items.length">{{ t("fileRevision.empty") }}</p>
    <ol v-if="items.length" class="artifact-history__list">
      <li v-for="revision in items" :key="revision.ref">
        <div class="artifact-history__row">
          <span>
            <strong>{{
              t("fileRevision.number", { revision: revision.revision })
            }}</strong>
            <small>{{ date(revision.createdAt) }}</small>
          </span>
          <StatusBadge :state="revision.scanState" />
          <button
            class="button"
            type="button"
            :aria-label="
              t('fileRevision.download', { revision: revision.revision })
            "
            :title="t('fileRevision.download', { revision: revision.revision })"
            :disabled="
              !!downloading ||
              revision.scanState !== 'CLEAN' ||
              !artifact.nextActions.includes('DOWNLOAD')
            "
            @click="download(revision)"
          >
            <Download :size="15" aria-hidden="true" />
          </button>
        </div>
        <details>
          <summary>{{ t("common.details") }}</summary>
          <p>
            {{ revision.fileName }} · {{ revision.mediaType }} ·
            {{ t("fileRevision.bytes", { count: revision.sizeBytes }) }}
          </p>
          <code>{{ revision.digest }}</code>
        </details>
      </li>
    </ol>
    <button
      v-if="nextPageToken"
      class="button artifact-history__more"
      type="button"
      :disabled="loading"
      @click="history.load(true)"
    >
      {{ loading ? t("common.loading") : t("fileRevision.more") }}
    </button>
  </section>
</template>

<style scoped>
.artifact-history {
  min-width: 0;
  display: grid;
  gap: 8px;
}
.artifact-history header,
.artifact-history__row {
  display: flex;
  align-items: center;
  gap: 7px;
}
.artifact-history h3,
.artifact-history p {
  margin: 0;
}
.artifact-history header button {
  margin-left: auto;
}
.artifact-history .button {
  min-width: 32px;
  min-height: 32px;
  padding: 5px;
}
.artifact-history__list {
  max-height: 300px;
  overflow-y: auto;
  margin: 0;
  padding: 0;
  list-style: none;
}
.artifact-history__list > li {
  padding: 7px 0;
  border-bottom: 1px solid var(--border);
}
.artifact-history__row > span:first-child {
  display: grid;
  gap: 3px;
  flex: 1;
  min-width: 0;
}
.artifact-history small,
.artifact-history details {
  font-size: 0.75rem;
  color: var(--muted);
}
.artifact-history details {
  margin-top: 5px;
}
.artifact-history details p,
.artifact-history code {
  overflow-wrap: anywhere;
}
.artifact-history__more {
  width: 100%;
}
@media (max-width: 720px), (pointer: coarse) {
  .artifact-history .button {
    min-width: 44px;
    min-height: 44px;
  }
}
</style>

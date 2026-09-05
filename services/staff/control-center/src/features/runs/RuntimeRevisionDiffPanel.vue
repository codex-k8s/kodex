<script setup lang="ts">
import { ref, watch } from "vue";
import type {
  Run,
  RuntimeRevisionDiff,
  RuntimeRevisionDiffValue,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { loadRuntimeRevisionDiff } from "./runtime-revision-diff";
const props = defineProps<{
  run: Pick<Run, "ref" | "sessionRef" | "version">;
}>();
const diff = ref<RuntimeRevisionDiff>();
const loading = ref(false);
const problem = ref<AppProblem>();
const attempt = ref(0);
watch(
  () => [props.run.ref, props.run.sessionRef, props.run.version, attempt.value],
  async (_value, _old, cleanup) => {
    const controller = new AbortController();
    cleanup(() => controller.abort());
    diff.value = undefined;
    problem.value = undefined;
    loading.value = true;
    try {
      const result = await loadRuntimeRevisionDiff(
        props.run.ref,
        props.run.sessionRef,
        controller.signal,
      );
      if (!controller.signal.aborted) diff.value = result;
    } catch (error) {
      if (!controller.signal.aborted) problem.value = asProblem(error);
    } finally {
      if (!controller.signal.aborted) loading.value = false;
    }
  },
  { immediate: true },
);
function descriptors(value: RuntimeRevisionDiffValue | undefined): string[] {
  if (!value) return [];
  return [
    value.ref,
    value.revision,
    value.version === undefined ? undefined : `v${String(value.version)}`,
    value.digest,
  ].filter(
    (entry): entry is string => typeof entry === "string" && entry.length > 0,
  );
}
</script>
<template>
  <section
    class="runtime-diff"
    :aria-label="$t('runtimeDiff.title')"
    :aria-busy="loading"
  >
    <h3>{{ $t("runtimeDiff.title") }}</h3>
    <p>{{ $t("runtimeDiff.help") }}</p>
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <ProblemNotice
      v-else-if="problem"
      :problem="problem"
      compact
      @retry="attempt++"
    />
    <template v-else-if="diff">
      <div class="runtime-diff__identities">
        <div>
          <strong>{{ $t("runtimeDiff.previous") }}</strong>
          <p>{{ diff.previous?.ref ?? $t("runtimeDiff.first") }}</p>
          <code>{{ diff.previous?.revisionDigest }}</code>
        </div>
        <div>
          <strong>{{ $t("runtimeDiff.current") }}</strong>
          <p>{{ diff.current.ref }} · v{{ diff.current.version }}</p>
          <code>{{ diff.current.revisionDigest }}</code>
        </div>
      </div>
      <p v-if="!diff.changes.length">{{ $t("runtimeDiff.noChanges") }}</p>
      <article
        v-for="change in diff.changes"
        :key="change.component"
        class="runtime-diff__change"
      >
        <h4>{{ $t(`runtimeDiff.components.${change.component}`) }}</h4>
        <div class="runtime-diff__values">
          <div>
            <small>{{ $t("runtimeDiff.previous") }}</small
            ><code v-for="value in descriptors(change.previous)" :key="value">{{
              value
            }}</code
            ><span v-if="!descriptors(change.previous).length">{{
              $t("runtimeDiff.absent")
            }}</span>
          </div>
          <div>
            <small>{{ $t("runtimeDiff.current") }}</small
            ><code v-for="value in descriptors(change.current)" :key="value">{{
              value
            }}</code
            ><span v-if="!descriptors(change.current).length">{{
              $t("runtimeDiff.absent")
            }}</span>
          </div>
        </div>
      </article>
    </template>
  </section>
</template>
<style scoped>
.runtime-diff {
  min-width: 0;
}
.runtime-diff__identities,
.runtime-diff__values {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.runtime-diff__identities > div,
.runtime-diff__values > div {
  display: grid;
  align-content: start;
  gap: 6px;
  min-width: 0;
}
.runtime-diff code,
.runtime-diff p {
  overflow-wrap: anywhere;
}
.runtime-diff__change {
  border-top: 1px solid var(--border);
  margin-top: 16px;
}
@media (max-width: 600px) {
  .runtime-diff__identities,
  .runtime-diff__values {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>

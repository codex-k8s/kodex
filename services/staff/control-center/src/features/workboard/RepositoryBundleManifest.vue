<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import { readRepositoryBundlePage } from "./repository-bundle-api";
import {
  readCompleteRepositoryBundleManifest,
  type CompleteRepositoryBundleManifest,
} from "./repository-bundle-manifest";

const props = defineProps<{ gateRef: string; gateVersion: number }>();
const emit = defineEmits<{ verified: [value: boolean] }>();
const manifest = ref<CompleteRepositoryBundleManifest>();
const failed = ref(false);
const loading = ref(false);
let controller: AbortController | undefined;
let generation = 0;

async function load(): Promise<void> {
  const current = ++generation;
  controller?.abort();
  controller = new AbortController();
  const signal = AbortSignal.any([controller.signal, ownerRequestSignal()]);
  signal.addEventListener("abort", () => {
    if (current !== generation) return;
    manifest.value = undefined;
    emit("verified", false);
  }, { once: true });
  emit("verified", false);
  manifest.value = undefined;
  failed.value = false;
  loading.value = true;
  const gateRef = props.gateRef;
  try {
    const result = await readCompleteRepositoryBundleManifest(gateRef, (page) => readRepositoryBundlePage(gateRef, page, signal), signal);
    signal.throwIfAborted();
    if (current !== generation || gateRef !== props.gateRef) return;
    manifest.value = result;
    emit("verified", true);
  } catch {
    if (current === generation && !signal.aborted) failed.value = true;
  } finally {
    if (current === generation) loading.value = false;
  }
}

watch([() => props.gateRef, () => props.gateVersion], () => { void load(); }, { immediate: true });
onBeforeUnmount(() => { generation += 1; controller?.abort(); emit("verified", false); });
</script>

<template>
  <section class="bundle-manifest" :aria-busy="loading">
    <h3>{{ $t("repositoryBundle.title") }}</h3>
    <p>{{ $t("repositoryBundle.effect") }}</p>
    <p v-if="loading" role="status">{{ $t("repositoryBundle.loading") }}</p>
    <template v-else-if="manifest">
	  <p><code>{{ manifest.proposal.state }}</code><template v-if="manifest.proposal.candidateCommitSha"> · <code>{{ manifest.proposal.candidateCommitSha }}</code></template></p>
	  <p v-if="manifest.proposal.continuationState === 'BLOCKED'" role="alert">{{ $t(manifest.proposal.continuationCode === "CONTINUATION_AUTHORITY_CHANGED" ? "repositoryBundle.continuationAuthorityChanged" : "repositoryBundle.continuationBlocked") }} <code>{{ manifest.proposal.continuationCode }} · {{ manifest.proposal.continuationNodeRef }}</code></p>
	  <p v-else-if="manifest.proposal.continuationState === 'CANCELLED'" role="status">{{ $t("repositoryBundle.continuationCancelled") }}</p>
      <p>{{ $t("repositoryBundle.complete", { count: manifest.files.length, bytes: manifest.proposal.totalBytes }) }}</p>
      <button type="button" class="button" @click="load">{{ $t("repositoryBundle.retryRead") }}</button>
      <dl>
        <dt>{{ $t("repositoryBundle.repository") }}</dt><dd>{{ manifest.proposal.repositoryRef }}</dd>
        <dt>{{ $t("repositoryBundle.base") }}</dt><dd><code>{{ manifest.proposal.baseCommitSha }}</code></dd>
        <dt>{{ $t("repositoryBundle.branch") }}</dt><dd><code>{{ manifest.proposal.proposalBranch }}</code></dd>
        <dt>SHA256</dt><dd><code>{{ manifest.proposal.manifest.sha256 }}</code></dd>
      </dl>
      <div class="bundle-manifest__files" tabindex="0">
        <table>
          <thead><tr><th>{{ $t("repositoryBundle.path") }}</th><th>{{ $t("repositoryBundle.bytes") }}</th><th>SHA256</th><th>{{ $t("repositoryBundle.mode") }}</th></tr></thead>
          <tbody><tr v-for="file in manifest.files" :key="file.path"><td>{{ file.path }}</td><td>{{ file.bytes }}</td><td><code>{{ file.sha256 }}</code></td><td>{{ file.mode }}</td></tr></tbody>
        </table>
      </div>
    </template>
    <template v-else-if="failed">
      <p role="alert">{{ $t("repositoryBundle.unavailable") }}</p>
      <button type="button" class="button" @click="load">{{ $t("repositoryBundle.retryRead") }}</button>
    </template>
  </section>
</template>

<style scoped>
.bundle-manifest { min-width: 0; }
.bundle-manifest dd { margin: 0 0 .5rem; overflow-wrap: anywhere; }
.bundle-manifest__files { max-height: 24rem; overflow: auto; }
.bundle-manifest table { border-collapse: collapse; width: 100%; text-align: left; font-size: .8rem; }
.bundle-manifest td, .bundle-manifest th { padding: .4rem; vertical-align: top; border-bottom: 1px solid var(--border-color, #d2d7df); }
.bundle-manifest td:first-child { overflow-wrap: anywhere; min-width: 9rem; }
</style>

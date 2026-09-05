<script setup lang="ts">
import {
  ArrowLeft,
  ExternalLink,
  File,
  Folder,
  RefreshCw,
  Search,
  Maximize2,
} from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import type { VfsNode } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { loadVfsPage, vfsEntityRoute } from "./vfs";
import { parseVfsTrail } from "./vfs-location";
import ModalDialog from "@/shared/ui/ModalDialog.vue";

const { locale } = useI18n();
const props = defineProps<{ projectRef?: string }>();
const route = useRoute();
const router = useRouter();
const expanded = ref(false);
const folders = ref(parseVfsTrail(route.query.vfsTrail));
const path = computed(() => folders.value.at(-1)?.path ?? "/projects");
const query = ref(
  typeof route.query.vfsQuery === "string"
    ? route.query.vfsQuery.slice(0, 500)
    : "",
);
const nodes = ref<VfsNode[]>([]);
const selected = ref<VfsNode>();
const entityRoute = computed(() =>
  selected.value ? vfsEntityRoute(selected.value) : undefined,
);
const entityLocation = computed(() => {
  if (!entityRoute.value) return undefined;
  const resolved = router.resolve(entityRoute.value);
  return {
    path: resolved.path,
    query: {
      ...resolved.query,
      vfsReturn: router.resolve({
        path: route.path,
        query: {
          view: "vfs",
          vfsTrail: folders.value.length
            ? JSON.stringify(folders.value)
            : undefined,
          vfsQuery: query.value || undefined,
        },
      }).fullPath,
    },
  };
});
const nextPageToken = ref("");
const total = ref(0);
const loading = ref(false);
const problem = ref<AppProblem>();
let controller: AbortController | undefined;
let timer: ReturnType<typeof setTimeout> | undefined;
let generation = 0;
const consumedCursors = new Set<string>();
async function load(more = false): Promise<void> {
  if (more && (loading.value || !nextPageToken.value)) return;
  controller?.abort();
  const request = new AbortController();
  controller = request;
  const current = ++generation;
  loading.value = true;
  problem.value = undefined;
  try {
    const token = more ? nextPageToken.value : undefined;
    const page = await loadVfsPage({
      path: path.value,
      query: query.value,
      projectRef: props.projectRef,
      pageToken: token,
      signal: request.signal,
    });
    if (current !== generation || request.signal.aborted) return;
    const entries = more ? [...nodes.value, ...page.items] : page.items;
    if (
      new Set(entries.map((node) => node.ref)).size !== entries.length ||
      (page.nextPageToken &&
        (page.nextPageToken === token ||
          (more && consumedCursors.has(page.nextPageToken))))
    )
      throw new Error("Invalid VFS cursor sequence");
    if (!more) consumedCursors.clear();
    if (token) consumedCursors.add(token);
    nodes.value = entries;
    selected.value = entries.find((node) => node.ref === selected.value?.ref);
    total.value = page.total;
    nextPageToken.value = page.nextPageToken;
  } catch (error) {
    if (current === generation && !request.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}
function open(node: VfsNode): void {
  if (!node.directory) {
    selected.value = node;
    return;
  }
  if (query.value.trim())
    folders.value = [{ path: node.path, name: node.name }];
  else folders.value.push({ path: node.path, name: node.name });
  query.value = "";
}
function scroll(event: Event): void {
  const element = event.currentTarget as HTMLElement;
  if (element.scrollTop + element.clientHeight >= element.scrollHeight - 80)
    void load(true);
}
watch(
  [folders, query],
  () => {
    void router.replace({
      query: {
        ...route.query,
        view: "vfs",
        vfsTrail: folders.value.length
          ? JSON.stringify(folders.value)
          : undefined,
        vfsQuery: query.value || undefined,
      },
    });
  },
  { deep: true },
);
watch(
  () => props.projectRef,
  () => {
    folders.value = [];
    query.value = "";
  },
);
watch(
  () => [props.projectRef, path.value, query.value],
  () => {
    controller?.abort();
    generation += 1;
    if (timer) clearTimeout(timer);
    nodes.value = [];
    selected.value = undefined;
    nextPageToken.value = "";
    total.value = 0;
    problem.value = undefined;
    consumedCursors.clear();
    loading.value = true;
    timer = setTimeout(
      () => {
        void load();
      },
      query.value.trim() ? 500 : 0,
    );
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  controller?.abort();
  if (timer) clearTimeout(timer);
  generation += 1;
});
</script>

<template>
  <component
    :is="expanded ? ModalDialog : 'section'"
    :title="expanded ? $t('files.title') : undefined"
    size="full"
    @close="expanded = false"
  >
    <section class="vfs-browser" @keydown.esc="selected = undefined">
      <div class="vfs-toolbar">
        <button
          class="icon-button"
          :disabled="!folders.length || !!query"
          :title="$t('vfs.back')"
          :aria-label="$t('vfs.back')"
          @click="folders.pop()"
        >
          <ArrowLeft :size="18" />
        </button>
        <nav :aria-label="$t('vfs.folders')">
          <button
            class="button button--ghost"
            @click="
              folders = [];
              query = '';
            "
          >
            {{ $t("nav.projects") }}</button
          ><button
            v-for="(folder, index) in folders"
            :key="folder.path"
            class="button button--ghost"
            @click="
              folders = folders.slice(0, index + 1);
              query = '';
            "
          >
            {{ folder.name }}
          </button>
        </nav>
        <button
          class="icon-button"
          :disabled="loading"
          :title="$t('vfs.refresh')"
          :aria-label="$t('vfs.refresh')"
          @click="load()"
        >
          <RefreshCw :size="18" />
        </button>
        <label class="vfs-search"
          ><Search :size="18" /><input
            v-model="query"
            type="search"
            :aria-label="$t('files.search')"
            :placeholder="$t('files.search')"
        /></label>
        <button
          v-if="!expanded"
          class="icon-button"
          :title="$t('catalog.expand')"
          :aria-label="$t('catalog.expand')"
          @click="expanded = true"
        >
          <Maximize2 :size="18" />
        </button>
      </div>
      <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
      <p v-if="loading && !nodes.length" role="status">
        {{ $t("common.loading") }}
      </p>
      <p v-else-if="!nodes.length && !problem">{{ $t("common.empty") }}</p>
      <div class="vfs-content" :class="{ 'vfs-content--selected': selected }">
        <div
          class="vfs-list"
          :class="{ 'vfs-list--expanded': expanded }"
          :aria-busy="loading"
          @scroll="scroll"
        >
          <button
            v-for="node in nodes"
            :key="node.ref"
            class="vfs-row"
            :class="{ 'vfs-row--selected': selected?.ref === node.ref }"
            :aria-pressed="selected?.ref === node.ref"
            @click="selected = selected?.ref === node.ref ? undefined : node"
            @dblclick="open(node)"
            @keydown.enter.prevent="open(node)"
          >
            <component :is="node.directory ? Folder : File" :size="20" />
            <span
              ><strong>{{ node.name }}</strong
              ><small>{{ $t(`vfs.kind.${node.kind}`) }}</small></span
            >
            <span v-if="!node.directory" class="vfs-size"
              >{{ $n(node.sizeBytes) }} {{ $t("vfs.bytes") }}</span
            >
          </button>
          <button
            v-if="nextPageToken"
            class="button"
            :disabled="loading"
            @click="load(true)"
          >
            {{ $t("managed.more") }} ({{ nodes.length }}/{{ total }})
          </button>
        </div>
        <aside v-if="selected" class="vfs-inspector">
          <h2>{{ selected.name }}</h2>
          <p>{{ $t(`vfs.kind.${selected.kind}`) }}</p>
          <button
            v-if="selected.directory"
            class="button"
            @click="open(selected)"
          >
            <Folder :size="18" />{{ $t("common.open") }}
          </button>
          <RouterLink
            v-if="entityRoute"
            class="button button--secondary"
            :to="entityLocation ?? entityRoute"
            ><ExternalLink :size="18" />{{ $t("vfs.entity") }}</RouterLink
          >
          <dl>
            <dt>{{ $t("vfs.path") }}</dt>
            <dd>{{ selected.path }}</dd>
            <template v-if="selected.digest"
              ><dt>{{ $t("vfs.digest") }}</dt>
              <dd>{{ selected.digest }}</dd></template
            ><template v-if="selected.modifiedAt"
              ><dt>{{ $t("vfs.modified") }}</dt>
              <dd>
                {{ new Date(selected.modifiedAt).toLocaleString(locale) }}
              </dd></template
            >
          </dl>
        </aside>
      </div>
    </section>
  </component>
</template>

<style scoped>
.vfs-browser {
  display: grid;
  gap: 16px;
  min-width: 0;
}
.vfs-toolbar {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
.vfs-toolbar nav {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  flex: 1;
  min-width: 0;
}
.vfs-toolbar nav button {
  overflow-wrap: anywhere;
  white-space: normal;
}
.vfs-search {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.vfs-search input {
  min-width: 0;
  width: 100%;
}
.vfs-content {
  display: grid;
  min-width: 0;
  gap: 24px;
}
.vfs-content--selected {
  grid-template-columns: minmax(0, 1fr) minmax(220px, 320px);
}
.vfs-list {
  min-width: 0;
  max-height: 432px;
  overflow: auto;
}
.vfs-list--expanded {
  max-height: calc(100dvh - 230px);
}
.vfs-row {
  display: grid;
  width: 100%;
  grid-template-columns: 20px minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  min-height: 72px;
  padding: 12px;
  text-align: left;
  border: 0;
  border-bottom: 1px solid var(--border);
  background: transparent;
  color: inherit;
}
.vfs-row:hover,
.vfs-row--selected {
  background: var(--accent-soft);
}
.vfs-row span {
  min-width: 0;
  overflow-wrap: anywhere;
}
.vfs-row strong,
.vfs-row small {
  display: block;
}
.vfs-row small {
  color: var(--muted);
  margin-top: 4px;
}
.vfs-inspector {
  min-width: 0;
  overflow-wrap: anywhere;
}
.vfs-inspector h2 {
  font-size: 18px;
}
.vfs-inspector dl {
  display: grid;
  gap: 8px;
}
.vfs-inspector dd {
  margin: 0 0 8px;
}
.vfs-inspector dt {
  color: var(--muted);
}
@media (max-width: 760px) {
  .vfs-content--selected {
    grid-template-columns: minmax(0, 1fr);
  }
  .vfs-search {
    flex: 1 1 100%;
  }
  .vfs-size {
    display: none;
  }
  .vfs-row {
    grid-template-columns: 20px minmax(0, 1fr);
  }
}
</style>

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
import type {
  VfsNode,
  VfsKind,
  SearchVfsData,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { loadVfsPage, vfsEntityRoute } from "./vfs";
import {
  parseVfsTrail,
  vfsKinds as availableKinds,
  vfsLifecycleStates as availableStates,
} from "./vfs-location";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import {
  applyVfsAction,
  prepareVfsAction,
  downloadVfsNode,
  vfsAction,
  vfsActionController,
  type VfsBulkAction,
  type VfsPreparedItem,
  type VfsActionReceipt,
} from "./vfs-actions";

const { locale } = useI18n();
const props = defineProps<{ projectRef?: string }>();
const route = useRoute();
const router = useRouter();
const lifecycleState = ref<
  NonNullable<SearchVfsData["query"]["lifecycleState"]>
>(availableStates.find((state) => state === route.query.vfsState) ?? "ACTIVE");
const kinds = ref<VfsKind[]>(
  availableKinds.filter(
    (kind) =>
      typeof route.query.vfsKinds === "string" &&
      route.query.vfsKinds.split(",").includes(kind),
  ),
);
const checked = ref<VfsNode[]>([]);
const actionBusy = ref(false);
const prepared = ref<VfsPreparedItem[]>([]);
const receipts = ref<VfsActionReceipt[]>([]);
let actionController: AbortController | undefined;
const bulkActions: VfsBulkAction[] = ["REMOVE", "RESTORE", "PURGE"];
function selectable(node: VfsNode): boolean {
  return (
    node.selectable &&
    (checked.value.some((item) => item.ref === node.ref) ||
      checked.value.length < 100)
  );
}
function toggle(node: VfsNode): void {
  if (!selectable(node) || actionBusy.value) return;
  prepared.value = [];
  checked.value = checked.value.some((item) => item.ref === node.ref)
    ? checked.value.filter((item) => item.ref !== node.ref)
    : [...checked.value, node];
}
function allows(action: VfsBulkAction): boolean {
  return (
    checked.value.length > 0 &&
    checked.value.every((node) => vfsAction(node, action))
  );
}
async function prepare(action: VfsBulkAction): Promise<void> {
  if (actionBusy.value || !allows(action)) return;
  actionController?.abort();
  const request = vfsActionController();
  actionController = request;
  actionBusy.value = true;
  problem.value = undefined;
  prepared.value = [];
  receipts.value = [];
  try {
    const result = await prepareVfsAction(
      checked.value,
      action,
      request.signal,
    );
    if (!request.signal.aborted) prepared.value = result;
  } catch (error) {
    if (!request.signal.aborted) problem.value = asProblem(error);
  } finally {
    if (actionController === request) actionBusy.value = false;
  }
}
async function confirm(): Promise<void> {
  const request = actionController;
  if (!request || actionBusy.value || !prepared.value.length) return;
  actionBusy.value = true;
  try {
    request.signal.throwIfAborted();
    const result = await applyVfsAction(prepared.value, request.signal);
    if (request.signal.aborted) return;
    receipts.value = result;
    checked.value = result
      .filter((item) => item.status === "FAILED")
      .map((item) => item.node);
    prepared.value = [];
    await load();
  } catch (error) {
    if (!request.signal.aborted) problem.value = asProblem(error);
  } finally {
    if (actionController === request) actionBusy.value = false;
  }
}
async function download(node: VfsNode): Promise<void> {
  if (actionBusy.value) return;
  const request = vfsActionController();
  actionController?.abort();
  actionController = request;
  actionBusy.value = true;
  try {
    const blob = await downloadVfsNode(node, request.signal);
    if (request.signal.aborted) return;
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = node.name;
    anchor.click();
    URL.revokeObjectURL(url);
  } catch (error) {
    if (!request.signal.aborted) problem.value = asProblem(error);
  } finally {
    if (actionController === request) actionBusy.value = false;
  }
}
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
          vfsState: lifecycleState.value,
          vfsKinds: kinds.value.join(",") || undefined,
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
      lifecycleState: lifecycleState.value,
      kinds: kinds.value,
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
    checked.value = checked.value.filter((item) =>
      entries.some(
        (node) =>
          node.ref === item.ref &&
          node.version === item.version &&
          node.revisionRef === item.revisionRef &&
          node.selectable,
      ),
    );
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
  [folders, query, lifecycleState, kinds, selected],
  () => {
    void router.replace({
      query: {
        ...route.query,
        view: "vfs",
        artifactRef:
          selected.value?.resourceKind === "ARTIFACT"
            ? selected.value.entityRef
            : undefined,
        vfsTrail: folders.value.length
          ? JSON.stringify(folders.value)
          : undefined,
        vfsQuery: query.value || undefined,
        vfsState: lifecycleState.value,
        vfsKinds: kinds.value.join(",") || undefined,
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
  () => [
    props.projectRef,
    path.value,
    query.value,
    lifecycleState.value,
    kinds.value.join(","),
  ],
  () => {
    controller?.abort();
    actionController?.abort();
    actionBusy.value = false;
    checked.value = [];
    prepared.value = [];
    receipts.value = [];
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
  actionController?.abort();
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
        <label class="vfs-filter">
          <span>{{ $t("vfs.lifecycle") }}</span>
          <select v-model="lifecycleState" :disabled="actionBusy">
            <option
              v-for="state in availableStates"
              :key="state"
              :value="state"
            >
              {{ $t(`vfs.lifecycleState.${state}`) }}
            </option>
          </select>
        </label>
        <details class="vfs-kind-filter">
          <summary>
            {{ $t("vfs.filterKinds") }} ({{
              kinds.length || availableKinds.length
            }})
          </summary>
          <fieldset :disabled="actionBusy">
            <legend>{{ $t("vfs.filterKinds") }}</legend>
            <label v-for="kind in availableKinds" :key="kind">
              <input v-model="kinds" type="checkbox" :value="kind" />{{
                $t(`vfs.kind.${kind}`)
              }}
            </label>
            <button type="button" class="button" @click="kinds = []">
              {{ $t("vfs.allKinds") }}
            </button>
          </fieldset>
        </details>
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
      <div class="vfs-bulk" :aria-busy="actionBusy">
        <span>{{ $t("common.selectedCount", { count: checked.length }) }}</span>
        <span v-if="checked.length === 100">{{
          $t("vfs.selectionLimit")
        }}</span>
        <button
          class="button"
          :disabled="!checked.length || actionBusy"
          @click="
            checked = [];
            prepared = [];
          "
        >
          {{ $t("vfs.clearSelection") }}
        </button>
        <button
          v-for="action in bulkActions"
          :key="action"
          class="button"
          :disabled="actionBusy || !allows(action)"
          @click="prepare(action)"
        >
          {{ $t(`vfs.bulkAction.${action}`) }}
        </button>
      </div>
      <section
        v-if="prepared.length"
        class="vfs-confirmation"
        :aria-label="$t('vfs.confirmTitle')"
      >
        <h2>{{ $t("vfs.confirmTitle") }}</h2>
        <p>{{ $t("vfs.confirmDescription") }}</p>
        <ul>
          <li v-for="item in prepared" :key="item.node.ref">
            {{ item.node.name }} · {{ $t(`vfs.command.${item.action}`) }} ·
            {{
              $t("vfs.version", {
                version: item.node.version,
                revision: item.node.revision,
              })
            }}
            <span v-if="item.impact">
              ·
              {{
                $t("vfs.impact", {
                  bindings: item.impact.bindingCount,
                  attachments: item.impact.attachmentCount,
                })
              }}</span
            >
          </li>
        </ul>
        <button
          class="button button--danger"
          :disabled="actionBusy"
          @click="confirm"
        >
          {{ $t("vfs.confirm", { count: prepared.length }) }}
        </button>
        <button class="button" :disabled="actionBusy" @click="prepared = []">
          {{ $t("common.cancel") }}
        </button>
      </section>
      <section v-if="receipts.length" :aria-label="$t('vfs.results')">
        <h2>{{ $t("vfs.results") }}</h2>
        <div v-for="receipt in receipts" :key="receipt.node.ref">
          <p>{{ receipt.node.name }} · {{ $t(`states.${receipt.status}`) }}</p>
          <ProblemNotice v-if="receipt.problem" :problem="receipt.problem" />
        </div>
      </section>
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
          <div v-for="node in nodes" :key="node.ref" class="vfs-entry">
            <input
              type="checkbox"
              :checked="checked.some((item) => item.ref === node.ref)"
              :disabled="actionBusy || !selectable(node)"
              :aria-label="$t('vfs.selectNode', { name: node.name })"
              :title="$t(`vfs.selectionReason.${node.selectionReason}`)"
              @change="toggle(node)"
            />
            <button
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
                ><small>{{ $t(`vfs.kind.${node.kind}`) }}</small>
                <small v-if="!node.selectable">{{
                  $t(`vfs.selectionReason.${node.selectionReason}`)
                }}</small></span
              >
              <span v-if="!node.directory" class="vfs-size"
                >{{ $n(node.sizeBytes) }} {{ $t("vfs.bytes") }}</span
              >
            </button>
          </div>
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
          <p>{{ $t(`vfs.selectionReason.${selected.selectionReason}`) }}</p>
          <p>
            {{
              $t("vfs.version", {
                version: selected.version,
                revision: selected.revision,
              })
            }}
          </p>
          <button
            v-if="selected.nextActions.includes('DOWNLOAD')"
            class="button"
            :disabled="actionBusy"
            @click="download(selected)"
          >
            {{ $t("vfs.download") }}
          </button>
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
.vfs-filter,
.vfs-kind-filter fieldset {
  display: grid;
  gap: 6px;
}
.vfs-kind-filter fieldset {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.vfs-kind-filter label {
  display: flex;
  align-items: center;
  gap: 6px;
}
.vfs-bulk {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.vfs-confirmation {
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.vfs-confirmation li {
  overflow-wrap: anywhere;
}
.vfs-entry {
  display: flex;
  align-items: center;
  gap: 8px;
}
.vfs-entry > input {
  flex: 0 0 auto;
  margin-left: 8px;
}
.vfs-entry > .vfs-row {
  min-width: 0;
  flex: 1;
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

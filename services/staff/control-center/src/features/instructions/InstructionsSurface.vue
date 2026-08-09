<script setup lang="ts">
import {
  Copy,
  FileCheck2,
  GitCompareArrows,
  History,
  Plus,
  RefreshCw,
  RotateCcw,
  Send,
  Unplug,
} from "@lucide/vue";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useInstructionsStore } from "@/features/instructions/store";
import type { Resource } from "@/shared/api/generated/openapi/types.gen";
import { resourceOwnership } from "@/shared/lib/resources";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const store = useInstructionsStore();
const { t } = useI18n();
const editorOpen = ref(false);
const historyOpen = ref(false);
const selected = ref<Resource | null>(null);
const leftVersion = ref(0);
const rightVersion = ref(0);
const form = reactive({ name: "", stableKey: "", locale: "ru", content: "" });
const projection = computed(() => selected.value?.spec.instructionSet);
const isGitOwned = computed(
  () =>
    selected.value !== null &&
    resourceOwnership(selected.value)?.managedBy === "git",
);

function edit(item?: Resource): void {
  selected.value = item ?? null;
  Object.assign(form, {
    name: item?.name ?? "",
    stableKey: item?.spec.instructionSet?.stableKey ?? "",
    locale: item?.spec.instructionSet?.locale ?? "ru",
    content: item?.spec.instructionSet?.content ?? "",
  });
  editorOpen.value = true;
}

async function save(): Promise<void> {
  const value = selected.value;
  const ok = await store.saveDraft(value, {
    name: form.name.trim(),
    stableKey: form.stableKey.trim(),
    locale: form.locale,
    content: form.content,
  });
  if (ok) editorOpen.value = false;
}

async function command(
  item: Resource,
  action:
    | "VALIDATE"
    | "PUBLISH"
    | "ROLLBACK"
    | "DETACH"
    | "COPY"
    | "ARCHIVE"
    | "DELETE",
  targetVersion?: number,
): Promise<void> {
  const ownership = resourceOwnership(item);
  if (
    (action === "DETACH" || action === "COPY") &&
    (!ownership || ownership.managedBy !== "git")
  )
    return;
  let confirmation = t("instructions.confirmAction", {
    action,
    name: item.name,
  });
  if ((action === "DETACH" || action === "COPY") && ownership) {
    confirmation = t("instructions.confirmGitAction", {
      action,
      name: item.name,
      source: ownership.source,
      revision: ownership.revision,
    });
  }
  if (!window.confirm(confirmation)) return;
  await store.executeInstruction(item, action, targetVersion);
}

async function showHistory(item: Resource): Promise<void> {
  selected.value = item;
  await store.loadInstructionHistory(item.id);
  const versions = store.history.data.map((entry) => entry.resource.version);
  leftVersion.value = versions.at(-1) ?? item.version;
  rightVersion.value = versions[0] ?? item.version;
  historyOpen.value = true;
}

async function compare(): Promise<void> {
  if (!selected.value || !leftVersion.value || !rightVersion.value) return;
  await store.compareInstructions(
    selected.value.id,
    leftVersion.value,
    rightVersion.value,
  );
}

onMounted(store.loadInstructions);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('instructions.title')"
      :subtitle="$t('instructions.subtitle')"
    >
      <template #actions
        ><button
          class="button button--secondary"
          type="button"
          @click="store.loadInstructions"
        >
          <RefreshCw :size="15" aria-hidden="true" />{{
            $t("common.refresh")
          }}</button
        ><button class="button button--primary" type="button" @click="edit()">
          <Plus :size="15" aria-hidden="true" />{{ $t("instructions.create") }}
        </button></template
      >
    </PageHeader>
    <ProblemNotice :problem="store.mutationProblem" />
    <AsyncPanel
      :phase="store.instructionSets.phase"
      :problem="store.instructionSets.problem"
      @retry="store.loadInstructions"
    >
      <section class="panel" style="margin-top: 15px">
        <div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ $t("common.name") }}</th>
                <th>{{ $t("instructions.locale") }}</th>
                <th>{{ $t("instructions.versionState") }}</th>
                <th>{{ $t("common.managedBy") }}</th>
                <th>{{ $t("common.source") }}</th>
                <th>{{ $t("common.version", { version: "" }) }}</th>
                <th>
                  <span class="sr-only">{{ $t("common.actions") }}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in store.instructionSets.data" :key="item.id">
                <td class="data-table__name">{{ item.name }}</td>
                <td>{{ item.spec.instructionSet?.locale }}</td>
                <td>
                  <StatusBadge
                    :state="
                      item.spec.instructionSet?.versionState ?? item.state
                    "
                  />
                </td>
                <td>
                  <StatusBadge
                    :state="resourceOwnership(item)?.managedBy ?? 'ui'"
                  />
                </td>
                <td>
                  {{ resourceOwnership(item)?.source ?? $t("common.noValue") }}
                  ·
                  {{
                    resourceOwnership(item)?.revision ?? $t("common.noValue")
                  }}
                </td>
                <td>{{ item.spec.instructionSet?.currentVersion }}</td>
                <td>
                  <div class="data-table__actions">
                    <button
                      v-if="resourceOwnership(item)?.managedBy !== 'git'"
                      class="button button--text"
                      type="button"
                      @click="edit(item)"
                    >
                      {{ $t("common.edit") }}</button
                    ><button
                      v-if="resourceOwnership(item)?.managedBy !== 'git'"
                      class="button button--text"
                      type="button"
                      @click="
                        command(
                          item,
                          'VALIDATE',
                          item.spec.instructionSet?.currentVersion,
                        )
                      "
                    >
                      <FileCheck2 :size="14" aria-hidden="true" />{{
                        $t("instructions.validate")
                      }}</button
                    ><button
                      v-if="
                        resourceOwnership(item)?.managedBy !== 'git' &&
                        item.spec.instructionSet?.validationSucceeded
                      "
                      class="button button--text"
                      type="button"
                      @click="
                        command(
                          item,
                          'PUBLISH',
                          item.spec.instructionSet?.currentVersion,
                        )
                      "
                    >
                      <Send :size="14" aria-hidden="true" />{{
                        $t("instructions.publish")
                      }}</button
                    ><button
                      class="button button--text"
                      type="button"
                      @click="showHistory(item)"
                    >
                      <History :size="14" aria-hidden="true" />{{
                        $t("instructions.history")
                      }}</button
                    ><button
                      v-if="resourceOwnership(item)?.managedBy === 'git'"
                      class="button button--text"
                      type="button"
                      @click="command(item, 'DETACH')"
                    >
                      <Unplug :size="14" aria-hidden="true" />{{
                        $t("common.detach")
                      }}</button
                    ><button
                      v-if="resourceOwnership(item)?.managedBy === 'git'"
                      class="button button--text"
                      type="button"
                      @click="command(item, 'COPY')"
                    >
                      <Copy :size="14" aria-hidden="true" />{{
                        $t("common.copy")
                      }}
                    </button>
                    <button
                      v-if="
                        resourceOwnership(item)?.managedBy !== 'git' &&
                        item.state !== 'ARCHIVED'
                      "
                      class="button button--text"
                      type="button"
                      @click="command(item, 'ARCHIVE')"
                    >
                      {{ $t("common.archive") }}
                    </button>
                    <button
                      v-if="resourceOwnership(item)?.managedBy !== 'git'"
                      class="button button--text"
                      type="button"
                      @click="command(item, 'DELETE')"
                    >
                      {{ $t("common.delete") }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </AsyncPanel>

    <ModalDialog
      :open="editorOpen"
      :title="selected ? $t('instructions.edit') : $t('instructions.create')"
      @close="editorOpen = false"
      ><form class="form-grid" @submit.prevent="save">
        <ProblemNotice :problem="store.mutationProblem" />
        <div
          v-if="isGitOwned"
          class="state-panel state-panel--warning form-field--full"
        >
          {{ $t("instructions.gitOwned") }}
        </div>
        <label class="form-field"
          ><span>{{ $t("common.name") }}</span
          ><input
            v-model="form.name"
            required
            maxlength="160"
            :disabled="isGitOwned" /></label
        ><label class="form-field"
          ><span>{{ $t("instructions.stableKey") }}</span
          ><input
            v-model="form.stableKey"
            required
            maxlength="160"
            :disabled="isGitOwned" /></label
        ><label class="form-field form-field--full"
          ><span>{{ $t("instructions.locale") }}</span
          ><select v-model="form.locale" :disabled="isGitOwned">
            <option value="ru">RU</option>
            <option value="en">EN</option>
          </select></label
        ><label class="form-field form-field--full"
          ><span>{{ $t("instructions.content") }}</span
          ><textarea
            v-model="form.content"
            class="code-editor"
            required
            minlength="1"
            maxlength="131072"
            spellcheck="true"
            :disabled="isGitOwned"
          />
        </label>
        <div
          v-if="projection?.validationProblems.length"
          class="validation-list form-field--full"
        >
          <article
            v-for="problem in projection.validationProblems"
            :key="`${problem.code}-${problem.line}-${problem.column}`"
          >
            <strong>{{ problem.code }}</strong
            ><span
              >{{ problem.field }} · {{ problem.line }}:{{
                problem.column
              }}</span
            >
            <p>{{ problem.message }}</p>
          </article>
        </div>
        <div class="button-row form-field--full">
          <button
            class="button button--primary"
            type="submit"
            :disabled="isGitOwned || store.mutating"
          >
            {{ $t("common.save") }}
          </button>
        </div>
      </form></ModalDialog
    >

    <ModalDialog
      :open="historyOpen"
      :title="$t('instructions.history')"
      @close="historyOpen = false"
      ><div class="section-stack">
        <div class="inline-form">
          <label class="form-field"
            ><span>{{ $t("instructions.leftVersion") }}</span
            ><select v-model.number="leftVersion">
              <option
                v-for="entry in store.history.data"
                :key="`l-${entry.resource.version}`"
                :value="entry.resource.version"
              >
                {{ entry.resource.version }} · {{ entry.action }}
              </option>
            </select></label
          ><label class="form-field"
            ><span>{{ $t("instructions.rightVersion") }}</span
            ><select v-model.number="rightVersion">
              <option
                v-for="entry in store.history.data"
                :key="`r-${entry.resource.version}`"
                :value="entry.resource.version"
              >
                {{ entry.resource.version }} · {{ entry.action }}
              </option>
            </select></label
          ><button
            class="button button--secondary"
            type="button"
            @click="compare"
          >
            <GitCompareArrows :size="15" aria-hidden="true" />{{
              $t("instructions.compare")
            }}
          </button>
        </div>
        <div v-if="store.instructionComparison.data" class="summary-card">
          <strong>{{
            store.instructionComparison.data.contentEqual
              ? $t("instructions.equal")
              : $t("instructions.different")
          }}</strong
          ><span>{{ store.instructionComparison.data.comparisonSha256 }}</span>
        </div>
        <div class="timeline">
          <article
            v-for="entry in store.history.data"
            :key="`${entry.resource.id}-${entry.resource.version}`"
          >
            <div>
              <strong>{{
                $t("common.version", { version: entry.resource.version })
              }}</strong
              ><span>{{ entry.action }} · {{ entry.occurredAt }}</span>
            </div>
            <button
              v-if="entry.resource.version !== selected?.version && !isGitOwned"
              class="button button--text"
              type="button"
              @click="
                selected &&
                command(selected, 'ROLLBACK', entry.resource.version)
              "
            >
              <RotateCcw :size="14" aria-hidden="true" />{{
                $t("instructions.rollback")
              }}
            </button>
          </article>
        </div>
      </div></ModalDialog
    >
  </div>
</template>

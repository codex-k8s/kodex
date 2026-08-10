<script setup lang="ts">
import { GitCompareArrows, RefreshCw } from "@lucide/vue";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import type {
  ConfigurationChangeModel,
  ConfigurationSourceKind,
} from "@/features/configuration/model";
import { useConfigurationStore } from "@/features/configuration/store";
import { formatDateTime } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const { locale } = useI18n();
const store = useConfigurationStore();
const instructionRef = ref("");
const leftVersion = ref(0);
const rightVersion = ref(0);
const versions = computed(() => store.historyVersions.data);
const sourceKinds: ConfigurationSourceKind[] = [
  "ROLE_DEFINITION",
  "AGENT",
  "INSTRUCTION_SET",
  "PROVIDER_POOL",
];

async function selectInstruction(): Promise<void> {
  if (!instructionRef.value) return;
  await store.loadHistory(instructionRef.value);
  leftVersion.value = versions.value.at(-1) ?? 0;
  rightVersion.value = versions.value[0] ?? 0;
}

const compare = () =>
  instructionRef.value && leftVersion.value && rightVersion.value
    ? store.compare(instructionRef.value, leftVersion.value, rightVersion.value)
    : Promise.resolve();

const showSource = (item: ConfigurationChangeModel) =>
  sourceKinds.includes(item.resourceKind as ConfigurationSourceKind)
    ? store.loadSource(
        item.resourceRef,
        item.resourceKind as ConfigurationSourceKind,
      )
    : Promise.resolve();

onMounted(store.load);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('configuration.title')"
      :subtitle="$t('configuration.ownerSubtitle')"
    >
      <template #actions>
        <button
          class="button button--secondary"
          type="button"
          @click="store.load"
        >
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button>
      </template>
    </PageHeader>
    <div class="section-stack" style="margin-top: 15px">
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("configuration.diff") }}</h2>
        </header>
        <div class="panel__body section-stack">
          <div class="inline-form">
            <label class="form-field">
              <span>{{ $t("configuration.instruction") }}</span>
              <select v-model="instructionRef" @change="selectInstruction">
                <option value="">{{ $t("common.select") }}</option>
                <option
                  v-for="item in store.instructions.data"
                  :key="item.ref"
                  :value="item.ref"
                >
                  {{ item.displayName }}
                </option>
              </select>
            </label>
            <label class="form-field">
              <span>{{ $t("configuration.left") }}</span>
              <select v-model.number="leftVersion">
                <option
                  v-for="version in versions"
                  :key="`left-${version}`"
                  :value="version"
                >
                  {{ version }}
                </option>
              </select>
            </label>
            <label class="form-field">
              <span>{{ $t("configuration.right") }}</span>
              <select v-model.number="rightVersion">
                <option
                  v-for="version in versions"
                  :key="`right-${version}`"
                  :value="version"
                >
                  {{ version }}
                </option>
              </select>
            </label>
            <button
              class="button button--primary"
              type="button"
              @click="compare"
            >
              <GitCompareArrows :size="15" aria-hidden="true" />{{
                $t("configuration.compare")
              }}
            </button>
          </div>
          <div v-if="store.diff.data" class="diff-list">
            <article
              v-for="(change, index) in store.diff.data.changes"
              :key="`${change.path}-${index}`"
            >
              <StatusBadge :state="change.kind" />
              <code>{{ change.path }}</code>
              <span v-if="change.display === 'REDACTED'">{{
                $t("configuration.redacted")
              }}</span>
              <template v-else
                ><del>{{ change.before }}</del
                ><ins>{{ change.after }}</ins></template
              >
            </article>
          </div>
        </div>
      </section>
      <section v-if="store.source.data" class="callout">
        <div>
          <strong>{{ store.source.data.displayName }}</strong>
          <span>
            {{ store.source.data.managedBy }} · {{ store.source.data.source }} ·
            {{ $t("common.revision") }} {{ store.source.data.sourceRevision }} ·
            drift={{ store.source.data.drift }}
            <template v-if="store.source.data.sourceSha256">
              · {{ $t("common.sourceDigest") }}
              {{ store.source.data.sourceSha256 }}
            </template>
          </span>
        </div>
        <StatusBadge :state="store.source.data.drift" />
      </section>
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("configuration.changes") }}</h2>
        </header>
        <AsyncPanel
          :phase="store.changes.phase"
          :problem="store.changes.problem"
          @retry="store.load"
        >
          <div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("audit.action") }}</th>
                  <th>{{ $t("audit.resource") }}</th>
                  <th>{{ $t("audit.outcome") }}</th>
                  <th>{{ $t("audit.policy") }}</th>
                  <th>{{ $t("audit.occurredAt") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in store.changes.data" :key="item.ref">
                  <td class="data-table__name">
                    <button
                      v-if="
                        sourceKinds.includes(
                          item.resourceKind as ConfigurationSourceKind,
                        )
                      "
                      class="button button--text"
                      type="button"
                      :aria-label="`${$t('common.details')}: ${item.resourceKind}`"
                      @click="showSource(item)"
                    >
                      <GitCompareArrows :size="15" aria-hidden="true" />{{
                        item.action
                      }}
                    </button>
                    <template v-else>{{ item.action }}</template>
                  </td>
                  <td>{{ item.resourceKind }} · v{{ item.resourceVersion }}</td>
                  <td><StatusBadge :state="item.outcome" /></td>
                  <td>v{{ item.policyRevision }}</td>
                  <td>{{ formatDateTime(item.occurredAt, locale) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </AsyncPanel>
      </section>
    </div>
  </div>
</template>

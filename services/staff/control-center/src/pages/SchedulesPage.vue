<script setup lang="ts">
import { Plus, RefreshCw } from "@lucide/vue";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useOwnerControlStore } from "@/features/owner-control/store";
import type { OwnerScheduleView } from "@/shared/api/generated/openapi/types.gen";
import { formatDateTime } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const store = useOwnerControlStore();
const { locale } = useI18n();
const editorOpen = ref(false);
const selected = ref<OwnerScheduleView | null>(null);
const promptKind = ref<"INLINE" | "SELECTOR">("INLINE");
const advanced = ref(false);
const form = reactive({
  name: "",
  agentStableKey: "",
  instructionSetStableKey: "",
  providerPoolStableKey: "",
  presetKey: "",
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  inlineMarkdown: "",
  artifactSelector: "",
  cron: "",
  intervalSeconds: 0,
  maximumAttempts: 0,
  maximumExecutionSeconds: 0,
});
const selectors = computed(() => store.scheduleSelectors.data?.selectors ?? []);
const agents = computed(() =>
  selectors.value.filter(
    (item) => item.kind === "AGENT" && item.state === "ACTIVE",
  ),
);
const instructions = computed(() =>
  selectors.value.filter(
    (item) => item.kind === "INSTRUCTION_SET" && item.state === "ACTIVE",
  ),
);
const pools = computed(() =>
  selectors.value.filter(
    (item) => item.kind === "PROVIDER_POOL" && item.state === "ACTIVE",
  ),
);
const defaults = computed(() => store.scheduleSelectors.data?.defaults);

function edit(value?: OwnerScheduleView): void {
  selected.value = value ?? null;
  promptKind.value = value?.prompt.kind ?? "INLINE";
  advanced.value = Boolean(value?.advancedOverrides.length);
  Object.assign(form, {
    name: value?.displayName ?? "",
    agentStableKey: value?.agentSelection.selector ?? "",
    instructionSetStableKey: value?.instructionSelection.selector ?? "",
    providerPoolStableKey: value?.providerPoolSelection.selector ?? "",
    presetKey:
      value?.presetKey ?? store.scheduleSelectors.data?.presets[0]?.key ?? "",
    timezone:
      value?.timezone ||
      Intl.DateTimeFormat().resolvedOptions().timeZone ||
      "UTC",
    inlineMarkdown: value?.prompt.inlineMarkdown ?? "",
    artifactSelector: value?.prompt.artifactSelector ?? "",
    cron: value?.cron ?? "",
    intervalSeconds: value?.intervalSeconds ?? 0,
    maximumAttempts:
      value?.maximumAttempts ?? defaults.value?.maximumAttempts ?? 0,
    maximumExecutionSeconds:
      value?.maximumExecutionSeconds ??
      defaults.value?.maximumExecutionSeconds ??
      0,
  });
  editorOpen.value = true;
}

async function save(): Promise<void> {
  const prompt =
    promptKind.value === "INLINE"
      ? { inlineMarkdown: form.inlineMarkdown }
      : { artifactSelector: form.artifactSelector };
  const advancedOverrides = advanced.value
    ? {
        ...(form.cron ? { cron: form.cron } : {}),
        ...(form.intervalSeconds > 0
          ? { intervalSeconds: form.intervalSeconds }
          : {}),
        ...(form.maximumAttempts > 0
          ? { maximumAttempts: form.maximumAttempts }
          : {}),
        ...(form.maximumExecutionSeconds > 0
          ? { maximumExecutionSeconds: form.maximumExecutionSeconds }
          : {}),
      }
    : undefined;
  const body = {
    name: form.name.trim(),
    agentStableKey: form.agentStableKey,
    instructionSetStableKey: form.instructionSetStableKey,
    providerPoolStableKey: form.providerPoolStableKey,
    intent: {
      timezone: form.timezone,
      presetKey: form.presetKey,
      prompt,
      ...(advancedOverrides ? { advancedOverrides } : {}),
    },
  };
  const ok = selected.value
    ? await store.saveSchedule(selected.value, body)
    : await store.addSchedule(body);
  if (ok) editorOpen.value = false;
}

onMounted(store.loadSchedules);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('schedules.title')"
      :subtitle="$t('schedules.ownerSubtitle')"
      ><template #actions
        ><button
          class="button button--secondary"
          type="button"
          @click="store.loadSchedules"
        >
          <RefreshCw :size="15" aria-hidden="true" />{{
            $t("common.refresh")
          }}</button
        ><button
          class="button button--primary"
          type="button"
          :disabled="!store.scheduleSelectors.data"
          @click="edit()"
        >
          <Plus :size="15" aria-hidden="true" />{{ $t("schedules.create") }}
        </button></template
      ></PageHeader
    >
    <ProblemNotice :problem="store.mutationProblem" />
    <section v-if="defaults" class="callout" style="margin-top: 15px">
      <div>
        <strong>{{ $t("schedules.serverDefaults") }}</strong
        ><span
          >{{ defaults.calendar }} · {{ defaults.overlapPolicy }} ·
          {{ defaults.misfirePolicy }} · {{ defaults.maximumAttempts }}
          {{ $t("schedules.attempts") }}</span
        >
      </div>
      <StatusBadge state="READY" />
    </section>
    <AsyncPanel
      :phase="store.schedules.phase"
      :problem="store.schedules.problem"
      @retry="store.loadSchedules"
    >
      <section class="panel" style="margin-top: 15px">
        <div class="card-grid">
          <article
            v-for="item in store.schedules.data"
            :key="item.scheduleRef"
            class="resource-card"
          >
            <div class="resource-card__header">
              <div>
                <strong>{{ item.displayName }}</strong
                ><small>{{ item.timezone }} · {{ item.presetKey }}</small>
              </div>
              <StatusBadge :state="item.state" />
            </div>
            <dl class="detail-list">
              <div>
                <dt>{{ $t("schedules.agent") }}</dt>
                <dd>{{ item.agentSelection.displayName }}</dd>
              </div>
              <div>
                <dt>{{ $t("schedules.prompt") }}</dt>
                <dd>{{ item.prompt.displayName }}</dd>
              </div>
              <div>
                <dt>{{ $t("schedules.effective") }}</dt>
                <dd>
                  {{ item.cron || `${item.intervalSeconds}s` }} ·
                  {{ item.overlapPolicy }} · {{ item.maximumAttempts }}
                </dd>
              </div>
              <div>
                <dt>{{ $t("schedules.nextRun") }}</dt>
                <dd>{{ formatDateTime(item.nextRunAt, locale) }}</dd>
              </div>
            </dl>
            <button
              class="button button--secondary"
              type="button"
              @click="edit(item)"
            >
              {{ $t("common.edit") }}
            </button>
          </article>
        </div>
      </section>
    </AsyncPanel>

    <ModalDialog
      :open="editorOpen"
      :title="$t('schedules.editor')"
      @close="editorOpen = false"
      ><form class="form-grid" @submit.prevent="save">
        <label class="form-field form-field--full"
          ><span>{{ $t("common.name") }}</span
          ><input v-model="form.name" required maxlength="120" /></label
        ><label class="form-field"
          ><span>{{ $t("schedules.agent") }}</span
          ><select v-model="form.agentStableKey" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in agents"
              :key="item.ref"
              :value="item.stableKey"
            >
              {{ item.displayName }}
            </option>
          </select></label
        ><label class="form-field"
          ><span>{{ $t("schedules.instructions") }}</span
          ><select v-model="form.instructionSetStableKey" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in instructions"
              :key="item.ref"
              :value="item.stableKey"
            >
              {{ item.displayName }}
            </option>
          </select></label
        ><label class="form-field"
          ><span>{{ $t("schedules.pool") }}</span
          ><select v-model="form.providerPoolStableKey" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in pools"
              :key="item.ref"
              :value="item.stableKey"
            >
              {{ item.displayName }}
            </option>
          </select></label
        ><label class="form-field"
          ><span>{{ $t("schedules.preset") }}</span
          ><select v-model="form.presetKey" required>
            <option
              v-for="item in store.scheduleSelectors.data?.presets ?? []"
              :key="item.key"
              :value="item.key"
            >
              {{ item.displayName }} · {{ item.description }}
            </option>
          </select></label
        ><label class="form-field form-field--full"
          ><span>{{ $t("schedules.timezone") }}</span
          ><input v-model="form.timezone" required maxlength="64"
        /></label>
        <fieldset class="segmented form-field--full">
          <legend>{{ $t("schedules.promptInput") }}</legend>
          <label
            ><input
              v-model="promptKind"
              type="radio"
              value="INLINE"
            />INLINE</label
          ><label
            ><input v-model="promptKind" type="radio" value="SELECTOR" />{{
              $t("schedules.artifact")
            }}</label
          >
        </fieldset>
        <label
          v-if="promptKind === 'INLINE'"
          class="form-field form-field--full"
          ><span>{{ $t("schedules.inlineMarkdown") }}</span
          ><textarea
            v-model="form.inlineMarkdown"
            required
            maxlength="100000"
          /></label
        ><label v-else class="form-field form-field--full"
          ><span>{{ $t("schedules.artifact") }}</span
          ><select v-model="form.artifactSelector" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in store.artifacts.data.filter(
                (artifact) => artifact.spec.artifact?.scanStatus === 'CLEAN',
              )"
              :key="item.id"
              :value="item.id"
            >
              {{ item.name }} · {{ item.spec.artifact?.mediaType }}
            </option>
          </select></label
        ><label class="check-field form-field--full"
          ><input v-model="advanced" type="checkbox" />{{
            $t("common.advanced")
          }}</label
        ><template v-if="advanced"
          ><label class="form-field"
            ><span>{{ $t("schedules.cron") }}</span
            ><input v-model="form.cron" maxlength="120" /></label
          ><label class="form-field"
            ><span>{{ $t("schedules.interval") }}</span
            ><input
              v-model.number="form.intervalSeconds"
              type="number"
              min="60"
              max="31536000" /></label
          ><label class="form-field"
            ><span>{{ $t("schedules.maximumAttempts") }}</span
            ><input
              v-model.number="form.maximumAttempts"
              type="number"
              min="1"
              max="100" /></label
          ><label class="form-field"
            ><span>{{ $t("schedules.maximumExecution") }}</span
            ><input
              v-model.number="form.maximumExecutionSeconds"
              type="number"
              min="30"
              max="86400" /></label
        ></template>
        <div class="button-row form-field--full">
          <button
            class="button button--primary"
            type="submit"
            :disabled="store.mutating"
          >
            {{ $t("common.save") }}
          </button>
        </div>
      </form></ModalDialog
    >
  </div>
</template>

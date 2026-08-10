<script setup lang="ts">
import { History, Pause, Play, Plus, RefreshCw, Trash2 } from "@lucide/vue";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useSchedulesStore } from "@/features/schedules/store";
import type {
  ScheduleOccurrenceModel,
  ScheduleView,
} from "@/features/schedules/model";
import { formatDateTime } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const store = useSchedulesStore();
const { locale, t } = useI18n();
const editorOpen = ref(false);
const selected = ref<ScheduleView | null>(null);
const promptKind = ref<"INLINE" | "SELECTOR">("INLINE");
const advanced = ref(false);
const historyOpen = ref(false);
const historySchedule = ref<ScheduleView | null>(null);
const recoveryReason = ref("owner_recovery");
const form = reactive({
  name: "",
  agentStableKey: "",
  instructionSetStableKey: "",
  providerPoolStableKey: "",
  presetKey: "",
  timezone: "",
  inlineMarkdown: "",
  artifactSelector: "",
  cron: "",
  intervalSeconds: 0,
  maximumAttempts: 0,
  calendar: "GREGORIAN" as "GREGORIAN" | "BUSINESS",
  overlapPolicy: "FORBID" as "FORBID" | "SKIP" | "QUEUE",
  misfirePolicy: "SKIP" as "SKIP" | "RUN_ONCE" | "CATCH_UP" | "WITHIN_GRACE",
  misfireGraceSeconds: 0,
  deliveryPolicy: "AT_LEAST_ONCE" as "AT_LEAST_ONCE" | "EXACTLY_ONCE_EFFECT",
  initialBackoffSeconds: 0,
  maximumBackoffSeconds: 0,
  deadLetterAfterSeconds: 0,
  sessionPolicy: "NEW" as "NEW" | "PERSISTENT" | "ROLLING",
  notificationPolicy: "AUDIT_ONLY" as
    | "ALWAYS"
    | "ON_ACTION"
    | "ON_FAILURE"
    | "ON_ACTION_OR_FAILURE"
    | "AUDIT_ONLY",
  coalesce: false,
  roomStableKey: "",
  maximumExecutionSeconds: 0,
});
const selectors = computed(() => store.scheduleSelectors.data?.selectors ?? []);
const agents = computed(() =>
  selectors.value.filter((item) => item.kind === "AGENT"),
);
const instructions = computed(() =>
  selectors.value.filter((item) => item.kind === "INSTRUCTION_SET"),
);
const pools = computed(() =>
  selectors.value.filter((item) => item.kind === "PROVIDER_POOL"),
);
const defaults = computed(() => store.scheduleSelectors.data?.defaults);

function edit(value?: ScheduleView): void {
  const serverDefaults = defaults.value;
  if (!value && !serverDefaults) return;
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
    timezone: value?.timezone ?? "",
    inlineMarkdown: value?.prompt.inlineMarkdown ?? "",
    artifactSelector: value?.prompt.artifactSelector ?? "",
    cron: value?.cron ?? "",
    intervalSeconds: value?.intervalSeconds ?? 0,
    maximumAttempts: value?.maximumAttempts ?? serverDefaults?.maximumAttempts,
    calendar: value?.calendar ?? serverDefaults?.calendar,
    overlapPolicy: value?.overlapPolicy ?? serverDefaults?.overlapPolicy,
    misfirePolicy: value?.misfirePolicy ?? serverDefaults?.misfirePolicy,
    misfireGraceSeconds: value?.misfireGraceSeconds ?? 0,
    deliveryPolicy: value?.deliveryPolicy ?? serverDefaults?.deliveryPolicy,
    initialBackoffSeconds:
      value?.initialBackoffSeconds ?? serverDefaults?.initialBackoffSeconds,
    maximumBackoffSeconds:
      value?.maximumBackoffSeconds ?? serverDefaults?.maximumBackoffSeconds,
    deadLetterAfterSeconds:
      value?.deadLetterAfterSeconds ?? serverDefaults?.deadLetterAfterSeconds,
    sessionPolicy: value?.sessionPolicy ?? serverDefaults?.sessionPolicy,
    notificationPolicy:
      value?.notificationPolicy ?? serverDefaults?.notificationPolicy,
    coalesce: value?.coalesce ?? serverDefaults?.coalesce,
    roomStableKey: value?.roomSelection.selector ?? "",
    maximumExecutionSeconds:
      value?.maximumExecutionSeconds ?? serverDefaults?.maximumExecutionSeconds,
  });
  editorOpen.value = true;
}

async function save(): Promise<void> {
  const ok = await store.saveScheduleDraft(selected.value, {
    name: form.name.trim(),
    agentStableKey: form.agentStableKey,
    instructionSetStableKey: form.instructionSetStableKey,
    providerPoolStableKey: form.providerPoolStableKey,
    presetKey: form.presetKey,
    timezone: form.timezone,
    promptKind: promptKind.value,
    inlineMarkdown: form.inlineMarkdown,
    artifactSelector: form.artifactSelector,
    roomStableKey: form.roomStableKey,
    advanced: advanced.value,
    cron: form.cron,
    intervalSeconds: form.intervalSeconds,
    maximumAttempts: form.maximumAttempts,
    calendar: form.calendar,
    overlapPolicy: form.overlapPolicy,
    misfirePolicy: form.misfirePolicy,
    misfireGraceSeconds: form.misfireGraceSeconds,
    deliveryPolicy: form.deliveryPolicy,
    initialBackoffSeconds: form.initialBackoffSeconds,
    maximumBackoffSeconds: form.maximumBackoffSeconds,
    deadLetterAfterSeconds: form.deadLetterAfterSeconds,
    sessionPolicy: form.sessionPolicy,
    notificationPolicy: form.notificationPolicy,
    coalesce: form.coalesce,
    maximumExecutionSeconds: form.maximumExecutionSeconds,
  });
  if (ok) editorOpen.value = false;
}

async function openHistory(value: ScheduleView): Promise<void> {
  historySchedule.value = value;
  historyOpen.value = true;
  await store.loadScheduleOccurrences(value.scheduleRef);
}

async function runNow(value: ScheduleView): Promise<void> {
  if (window.confirm(t("schedules.confirmRun", { name: value.displayName })))
    await store.runSchedule(value);
}

async function changeState(
  value: ScheduleView,
  target: "ACTIVE" | "PAUSED",
): Promise<void> {
  if (
    window.confirm(
      t("schedules.confirmState", { state: target, name: value.displayName }),
    )
  )
    await store.transitionSchedule(value, target);
}

async function remove(value: ScheduleView): Promise<void> {
  if (window.confirm(t("schedules.confirmDelete", { name: value.displayName })))
    await store.removeSchedule(value);
}

async function recover(
  occurrence: ScheduleOccurrenceModel,
  action: "REPAIR" | "CANCEL" | "SKIP",
): Promise<void> {
  if (!historySchedule.value || !occurrence.recoveryEvidenceSha256) return;
  await store.resolveScheduleOccurrence(
    historySchedule.value,
    occurrence,
    action,
    recoveryReason.value.trim(),
  );
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
                  {{ item.calendar }} · {{ item.overlapPolicy }} ·
                  {{ item.misfirePolicy }} ({{ item.misfireGraceSeconds }}s) ·
                  {{ item.deliveryPolicy }} · {{ item.maximumAttempts }}
                </dd>
              </div>
              <div>
                <dt>
                  {{ $t("schedules.initialBackoff") }} /
                  {{ $t("schedules.deadLetter") }}
                </dt>
                <dd>
                  {{ item.initialBackoffSeconds }}s →
                  {{ item.maximumBackoffSeconds }}s ·
                  {{ item.deadLetterAfterSeconds }}s
                </dd>
              </div>
              <div>
                <dt>
                  {{ $t("schedules.sessionPolicy") }} /
                  {{ $t("schedules.notification") }} /
                  {{ $t("schedules.room") }}
                </dt>
                <dd>
                  {{ item.sessionPolicy }} · {{ item.notificationPolicy }} ·
                  {{ item.roomSelection.displayName }} · coalesce={{
                    item.coalesce
                  }}
                  · max={{ item.maximumExecutionSeconds }}s
                </dd>
              </div>
              <div>
                <dt>{{ $t("schedules.nextRun") }}</dt>
                <dd>{{ formatDateTime(item.nextRunAt, locale) }}</dd>
              </div>
            </dl>
            <div class="button-row">
              <button
                v-if="item.nextActions.includes('UPDATE')"
                class="button button--secondary"
                type="button"
                @click="edit(item)"
              >
                {{ $t("common.edit") }}
              </button>
              <button
                v-if="item.nextActions.includes('RUN_NOW')"
                class="button button--text"
                type="button"
                @click="runNow(item)"
              >
                <Play :size="14" aria-hidden="true" />{{
                  $t("schedules.runNow")
                }}
              </button>
              <button
                v-if="item.nextActions.includes('PAUSE')"
                class="button button--text"
                type="button"
                @click="changeState(item, 'PAUSED')"
              >
                <Pause :size="14" aria-hidden="true" />{{
                  $t("schedules.pause")
                }}
              </button>
              <button
                v-if="item.nextActions.includes('RESUME')"
                class="button button--text"
                type="button"
                @click="changeState(item, 'ACTIVE')"
              >
                <Play :size="14" aria-hidden="true" />{{
                  $t("schedules.resume")
                }}
              </button>
              <button
                v-if="item.nextActions.includes('VIEW_OCCURRENCES')"
                class="button button--text"
                type="button"
                @click="openHistory(item)"
              >
                <History :size="14" aria-hidden="true" />{{
                  $t("schedules.occurrences")
                }}
              </button>
              <button
                v-if="item.nextActions.includes('DELETE')"
                class="button button--text"
                type="button"
                @click="remove(item)"
              >
                <Trash2 :size="14" aria-hidden="true" />{{
                  $t("common.delete")
                }}
              </button>
            </div>
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
          ><input v-model="form.name" required maxlength="160" /></label
        ><label class="form-field"
          ><span>{{ $t("schedules.agent") }}</span
          ><select v-model="form.agentStableKey" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in agents"
              :key="item.ref"
              :value="item.stableKey"
            >
              {{ item.displayName }} · {{ item.state }}
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
              {{ item.displayName }} · {{ item.state }}
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
              {{ item.displayName }} · {{ item.state }}
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
        <label class="form-field form-field--full"
          ><span>{{ $t("people.room") }}</span
          ><select v-model="form.roomStableKey">
            <option value="">{{ $t("common.noValue") }}</option>
            <option
              v-for="item in store.rooms.data"
              :key="item.id"
              :value="item.stableKey"
            >
              {{ item.name }}
            </option>
          </select></label
        >
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
            maxlength="131072"
          /></label
        ><label v-else class="form-field form-field--full"
          ><span>{{ $t("schedules.artifact") }}</span
          ><select v-model="form.artifactSelector" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in store.artifacts.data.filter(
                (artifact) => artifact.scanStatus === 'CLEAN',
              )"
              :key="item.id"
              :value="item.id"
            >
              {{ item.name }} · {{ item.mediaType }}
            </option>
          </select></label
        ><label class="check-field form-field--full"
          ><input v-model="advanced" type="checkbox" />{{
            $t("common.advanced")
          }}</label
        ><template v-if="advanced"
          ><label class="form-field"
            ><span>{{ $t("schedules.cron") }}</span
            ><input
              v-model="form.cron"
              maxlength="128"
              :disabled="form.intervalSeconds > 0" /></label
          ><label class="form-field"
            ><span>{{ $t("schedules.interval") }}</span
            ><input
              v-model.number="form.intervalSeconds"
              type="number"
              min="60"
              max="31536000"
              :disabled="Boolean(form.cron)" /></label
          ><label class="form-field"
            ><span>{{ $t("schedules.maximumAttempts") }}</span
            ><input
              v-model.number="form.maximumAttempts"
              type="number"
              min="1"
              max="100" /></label
          ><label class="form-field"
            ><span>{{ $t("schedules.calendar") }}</span
            ><select v-model="form.calendar">
              <option value="GREGORIAN">GREGORIAN</option>
              <option value="BUSINESS">BUSINESS</option>
            </select></label
          ><label class="form-field"
            ><span>{{ $t("schedules.overlap") }}</span
            ><select v-model="form.overlapPolicy">
              <option value="FORBID">FORBID</option>
              <option value="SKIP">SKIP</option>
              <option value="QUEUE">QUEUE</option>
            </select></label
          ><label class="form-field"
            ><span>{{ $t("schedules.misfire") }}</span
            ><select v-model="form.misfirePolicy">
              <option value="SKIP">SKIP</option>
              <option value="RUN_ONCE">RUN_ONCE</option>
              <option value="CATCH_UP">CATCH_UP</option>
              <option value="WITHIN_GRACE">WITHIN_GRACE</option>
            </select></label
          ><label class="form-field"
            ><span>{{ $t("schedules.misfireGrace") }}</span
            ><input
              v-model.number="form.misfireGraceSeconds"
              type="number"
              min="0"
              max="86400" /></label
          ><label class="form-field"
            ><span>{{ $t("schedules.delivery") }}</span
            ><select v-model="form.deliveryPolicy">
              <option value="AT_LEAST_ONCE">AT_LEAST_ONCE</option>
              <option value="EXACTLY_ONCE_EFFECT">EXACTLY_ONCE_EFFECT</option>
            </select></label
          ><label class="form-field"
            ><span>{{ $t("schedules.initialBackoff") }}</span
            ><input
              v-model.number="form.initialBackoffSeconds"
              type="number"
              min="1"
              max="86400" /></label
          ><label class="form-field"
            ><span>{{ $t("schedules.maximumBackoff") }}</span
            ><input
              v-model.number="form.maximumBackoffSeconds"
              type="number"
              min="1"
              max="86400" /></label
          ><label class="form-field"
            ><span>{{ $t("schedules.deadLetter") }}</span
            ><input
              v-model.number="form.deadLetterAfterSeconds"
              type="number"
              min="1"
              max="2592000" /></label
          ><label class="form-field"
            ><span>{{ $t("schedules.session") }}</span
            ><select v-model="form.sessionPolicy">
              <option value="NEW">NEW</option>
              <option value="PERSISTENT">PERSISTENT</option>
              <option value="ROLLING">ROLLING</option>
            </select></label
          ><label class="form-field"
            ><span>{{ $t("schedules.notification") }}</span
            ><select v-model="form.notificationPolicy">
              <option value="ALWAYS">ALWAYS</option>
              <option value="ON_ACTION">ON_ACTION</option>
              <option value="ON_FAILURE">ON_FAILURE</option>
              <option value="ON_ACTION_OR_FAILURE">ON_ACTION_OR_FAILURE</option>
              <option value="AUDIT_ONLY">AUDIT_ONLY</option>
            </select></label
          ><label class="check-field"
            ><input v-model="form.coalesce" type="checkbox" />{{
              $t("schedules.coalesce")
            }}</label
          ><label class="form-field"
            ><span>{{ $t("schedules.maximumExecution") }}</span
            ><input
              v-model.number="form.maximumExecutionSeconds"
              type="number"
              min="60"
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
    <ModalDialog
      :open="historyOpen"
      :title="$t('schedules.occurrences')"
      @close="historyOpen = false"
    >
      <div class="section-stack">
        <label class="form-field">
          <span>{{ $t("integrations.reasonCode") }}</span>
          <input
            v-model="recoveryReason"
            required
            maxlength="64"
            pattern="[a-z][a-z0-9_]{2,63}"
          />
        </label>
        <AsyncPanel
          :phase="store.scheduleOccurrences.phase"
          :problem="store.scheduleOccurrences.problem"
          @retry="
            historySchedule &&
            store.loadScheduleOccurrences(historySchedule.scheduleRef)
          "
        >
          <div class="timeline">
            <article
              v-for="occurrence in store.scheduleOccurrences.data"
              :key="occurrence.occurrenceId"
            >
              <div>
                <strong>{{
                  formatDateTime(occurrence.scheduledFor, locale)
                }}</strong>
                <span
                  >{{ occurrence.state }} · attempt
                  {{ occurrence.attempt }}</span
                >
              </div>
              <div v-if="occurrence.recoveryActions.length" class="button-row">
                <button
                  v-if="occurrence.recoveryActions.includes('REPAIR')"
                  class="button button--text"
                  type="button"
                  @click="recover(occurrence, 'REPAIR')"
                >
                  REPAIR
                </button>
                <button
                  v-if="occurrence.recoveryActions.includes('SKIP')"
                  class="button button--text"
                  type="button"
                  @click="recover(occurrence, 'SKIP')"
                >
                  SKIP
                </button>
                <button
                  v-if="occurrence.recoveryActions.includes('CANCEL')"
                  class="button button--text"
                  type="button"
                  @click="recover(occurrence, 'CANCEL')"
                >
                  CANCEL
                </button>
              </div>
            </article>
          </div>
        </AsyncPanel>
      </div>
    </ModalDialog>
  </div>
</template>

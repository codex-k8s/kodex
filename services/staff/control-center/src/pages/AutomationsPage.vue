<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRoute } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
const list = computed(() =>
  Object.values(platform.schedules).filter(
    (item) => item.projectRef === projectRef.value,
  ),
);
const agents = computed(() =>
  Object.values(platform.agents).filter(
    (item) =>
      item.projectRef === projectRef.value && item.enabled && !item.system,
  ),
);
const workflows = computed(() =>
  Object.values(platform.workflows).filter(
    (item) =>
      item.projectRef === projectRef.value && item.state === "PUBLISHED",
  ),
);
const dialog = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const form = reactive({
  name: "",
  targetType: "AGENT" as "AGENT" | "WORKFLOW",
  targetRef: "",
  preset: "DAILY",
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  task: "",
  sessionPolicy: "NEW_EACH_RUN" as "NEW_EACH_RUN" | "CONTINUE_ONE",
  notificationPolicy: "CONTROL_CENTER_ONLY" as
    | "CONTROL_CENTER_ONLY"
    | "CONTROL_CENTER_AND_OPTIONAL_CHANNELS",
});
const targets = computed(() =>
  form.targetType === "AGENT" ? agents.value : workflows.value,
);

async function submit(): Promise<void> {
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.saveSchedule(projectRef.value, {
      name: form.name,
      targetType: form.targetType,
      targetRef: form.targetRef,
      preset: form.preset,
      timezone: form.timezone,
      input: { task: form.task },
      sessionPolicy: form.sessionPolicy,
      notificationPolicy: form.notificationPolicy,
    });
    dialog.value = false;
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function command(
  ref: string,
  action: "ENABLE" | "PAUSE" | "ARCHIVE",
): Promise<void> {
  const schedule = platform.schedules[ref];
  if (!schedule) return;
  try {
    await platform.changeSchedule(schedule, action);
  } catch (error) {
    problem.value = asProblem(error);
  }
}

onMounted(
  () =>
    void Promise.all([
      platform.loadSchedules(projectRef.value),
      platform.loadAgents(projectRef.value),
      platform.loadWorkflows(projectRef.value),
    ]),
);
</script>

<template>
  <PageFrame
    :title="$t('automations.title')"
    :subtitle="$t('automations.subtitle')"
  >
    <template #actions
      ><button
        class="button button--primary"
        type="button"
        :disabled="agents.length + workflows.length === 0"
        @click="dialog = true"
      >
        {{ $t("automations.new") }}
      </button></template
    >
    <ProblemNotice v-if="problem && !dialog" :problem="problem" compact />
    <AsyncState
      :loading="platform.loading.schedules"
      :problem="platform.problems.schedules"
      :empty="list.length === 0"
      :empty-title="$t('automations.emptyTitle')"
      :empty-text="$t('automations.emptyText')"
      @retry="platform.loadSchedules(projectRef)"
    >
      <template #empty-action
        ><button
          class="button button--primary"
          type="button"
          :disabled="agents.length + workflows.length === 0"
          @click="dialog = true"
        >
          {{ $t("automations.new") }}
        </button></template
      >
      <div class="entity-list">
        <article
          v-for="schedule in list"
          :key="schedule.ref"
          class="entity-row"
        >
          <div>
            <h3>{{ schedule.name }}</h3>
            <p>
              {{ schedule.target.displayName }} · {{ schedule.preset }} ·
              {{ schedule.timezone }}
            </p>
          </div>
          <StatusBadge :state="schedule.state" />
          <div class="entity-row__actions">
            <button
              v-if="schedule.nextActions.includes('ENABLE')"
              class="button"
              type="button"
              @click="command(schedule.ref, 'ENABLE')"
            >
              {{ $t("common.enable") }}
            </button>
            <button
              v-if="schedule.nextActions.includes('DISABLE')"
              class="button"
              type="button"
              @click="command(schedule.ref, 'PAUSE')"
            >
              {{ $t("automations.pause") }}
            </button>
            <button
              v-if="schedule.nextActions.includes('ARCHIVE')"
              class="button button--danger"
              type="button"
              @click="command(schedule.ref, 'ARCHIVE')"
            >
              {{ $t("common.archive") }}
            </button>
          </div>
        </article>
      </div>
    </AsyncState>
    <ModalDialog
      v-if="dialog"
      :title="$t('automations.new')"
      :busy="busy"
      @close="dialog = false"
    >
      <form id="schedule-form" class="form-grid" @submit.prevent="submit">
        <label class="field field--wide"
          ><span>{{ $t("common.name") }}</span
          ><input v-model.trim="form.name" required maxlength="160" autofocus
        /></label>
        <label class="field"
          ><span>{{ $t("runs.targetType") }}</span
          ><select v-model="form.targetType" @change="form.targetRef = ''">
            <option value="AGENT">{{ $t("runs.agent") }}</option>
            <option value="WORKFLOW">{{ $t("runs.workflow") }}</option>
          </select></label
        >
        <label class="field"
          ><span>{{ $t("common.target") }}</span
          ><select v-model="form.targetRef" required>
            <option value="" disabled>
              {{ $t("automations.chooseTarget") }}
            </option>
            <option
              v-for="target in targets"
              :key="target.ref"
              :value="target.ref"
            >
              {{ target.name }}
            </option>
          </select></label
        >
        <label class="field"
          ><span>{{ $t("automations.preset") }}</span
          ><select v-model="form.preset">
            <option value="HOURLY">{{ $t("automations.hourly") }}</option>
            <option value="DAILY">{{ $t("automations.daily") }}</option>
            <option value="WEEKDAYS">{{ $t("automations.weekdays") }}</option>
            <option value="WEEKLY">{{ $t("automations.weekly") }}</option>
          </select></label
        >
        <label class="field"
          ><span>{{ $t("automations.timezone") }}</span
          ><input v-model.trim="form.timezone" required maxlength="64"
        /></label>
        <label class="field field--wide"
          ><span>{{ $t("runs.task") }}</span
          ><textarea v-model.trim="form.task" required maxlength="8000" />
        </label>
        <label class="field"
          ><span>{{ $t("automations.sessionPolicy") }}</span
          ><select v-model="form.sessionPolicy">
            <option value="NEW_EACH_RUN">
              {{ $t("automations.newSession") }}
            </option>
            <option value="CONTINUE_ONE">
              {{ $t("automations.continueSession") }}
            </option>
          </select></label
        >
        <label class="field"
          ><span>{{ $t("automations.notifications") }}</span
          ><select v-model="form.notificationPolicy">
            <option value="CONTROL_CENTER_ONLY">
              {{ $t("automations.controlCenterOnly") }}
            </option>
            <option value="CONTROL_CENTER_AND_OPTIONAL_CHANNELS">
              {{ $t("automations.optionalChannels") }}
            </option>
          </select></label
        >
        <ProblemNotice
          v-if="problem"
          class="field--wide"
          :problem="problem"
          compact
        />
      </form>
      <template #actions
        ><button
          class="button"
          type="button"
          :disabled="busy"
          @click="dialog = false"
        >
          {{ $t("common.cancel") }}</button
        ><button
          class="button button--primary"
          form="schedule-form"
          type="submit"
          :disabled="busy"
        >
          {{ $t("common.create") }}
        </button></template
      >
    </ModalDialog>
  </PageFrame>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, useId } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { usePersonalEmailConnections } from "../personal-email-connections";
const { t } = useI18n(),
  prefix = useId(),
  model = usePersonalEmailConnections();
const {
  items,
  query,
  nextPageToken,
  name,
  nameValid,
  sender,
  busy,
  loading,
  requiresRefresh,
  problem,
  pending,
  createdRef,
} = model;
onMounted(() => {
  window.addEventListener("pagehide", model.suspend);
  void model.load();
});
onBeforeUnmount(() => window.removeEventListener("pagehide", model.suspend));
</script>

<template>
  <section class="card stack" :aria-labelledby="`${prefix}-title`">
    <header class="stack">
      <h2 :id="`${prefix}-title`">{{ t("personalEmail.title") }}</h2>
      <p>{{ t("personalEmail.description") }}</p>
    </header>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="requiresRefresh" role="status">
      {{ t("personalEmail.refreshRequired") }}
    </p>
    <div v-if="pending" class="stack" role="alert">
      <p>{{ t("personalEmail.pending") }}</p>
      <button
        class="button"
        type="button"
        :disabled="busy || loading"
        @click="model.recover()"
      >
        {{ t("personalEmail.recover") }}
      </button>
    </div>
    <div v-if="createdRef" role="status" class="stack">
      <p>{{ t("personalEmail.created") }}</p>
      <RouterLink
        class="button"
        :to="{
          name: 'integrations',
          query: { section: 'personal', connectionRef: createdRef },
        }"
        >{{ t("personalEmail.open") }}</RouterLink
      >
    </div>
    <form class="stack" @submit.prevent="model.load()">
      <label class="field" :for="`${prefix}-search`"
        ><span>{{ t("personalEmail.search") }}</span
        ><input
          :id="`${prefix}-search`"
          v-model="query"
          type="search"
          maxlength="200"
          :disabled="busy"
      /></label>
      <div class="actions">
        <button class="button" type="submit" :disabled="busy || loading">
          {{ t("personalEmail.find") }}</button
        ><button
          class="button"
          type="button"
          :disabled="busy || loading"
          @click="model.load()"
        >
          {{ t("personalEmail.refresh") }}
        </button>
      </div>
    </form>
    <p v-if="loading" role="status">{{ t("personalEmail.loading") }}</p>
    <p v-else-if="!problem && !items.length">{{ t("personalEmail.empty") }}</p>
    <ul class="stack">
      <li
        v-for="item in items"
        :key="item.ownership.connectionRef"
        class="stack"
      >
        <strong>{{ item.name }}</strong
        ><span>{{
          t(
            item.ownership.policy === "INITIATOR_REQUIRED"
              ? "personalEmail.personal"
              : "personalEmail.service",
          )
        }}</span>
        <span
          >{{ t(`personalEmail.states.${item.ownership.state}`) }} —
          {{
            t(
              item.available
                ? "personalEmail.available"
                : "personalEmail.notReady",
            )
          }}</span
        >
        <RouterLink
          class="button"
          :to="{
            name: 'integrations',
            query: {
              section: 'personal',
              connectionRef: item.ownership.connectionRef,
            },
          }"
          >{{ t("personalEmail.open") }}</RouterLink
        >
      </li>
    </ul>
    <button
      v-if="nextPageToken"
      class="button"
      type="button"
      :disabled="busy || loading"
      @click="model.load(true)"
    >
      {{ t("personalEmail.next") }}
    </button>
    <form class="stack" @submit.prevent="model.create()">
      <label class="field" :for="`${prefix}-name`"
        ><span>{{ t("personalEmail.name") }}</span
        ><input
          :id="`${prefix}-name`"
          v-model="name"
          required
          maxlength="320"
          :aria-invalid="Boolean(name) && !nameValid"
          :disabled="busy || loading"
      /></label>
      <label class="field" :for="`${prefix}-sender`"
        ><span>{{ t("personalEmail.sender") }}</span
        ><input
          :id="`${prefix}-sender`"
          v-model="sender"
          type="email"
          autocomplete="email"
          required
          maxlength="320"
          :disabled="busy || loading"
      /></label>
      <button
        class="button button--primary"
        type="submit"
        :disabled="
          busy || loading || requiresRefresh || !nameValid || !sender.trim()
        "
      >
        {{ t("personalEmail.create") }}
      </button>
      <p class="muted">{{ t("personalEmail.noRights") }}</p>
    </form>
  </section>
</template>

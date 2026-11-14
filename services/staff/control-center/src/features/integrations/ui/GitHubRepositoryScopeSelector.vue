<script setup lang="ts">
import { toRef, useId } from "vue";
import { useI18n } from "vue-i18n";
import type { PersonalIntegrationConnection } from "@/shared/api/generated/openapi/types.gen";
import { useGitHubRepositoryScope } from "../github-repository-scope";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
const props = defineProps<{ connection: PersonalIntegrationConnection }>();
const emit = defineEmits<{ changed: [] }>();
const prefix = useId();
const { t } = useI18n();
const {
  busy,
  problem,
  page,
  selected,
  kind,
  saved,
  discover,
  toggle,
  save,
  canDiscover,
} = useGitHubRepositoryScope(toRef(props, "connection"), () => emit("changed"));
</script>
<template>
  <section
    class="github-scope"
    :aria-labelledby="`${prefix}-title`"
    :aria-busy="busy"
  >
    <h4 :id="`${prefix}-title`">
      {{ t("personalConnections.githubScope.title") }}
    </h4>
    <p>{{ t("personalConnections.githubScope.description") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" />
    <button
      class="button"
      type="button"
      :disabled="!canDiscover"
      @click="discover(1)"
    >
      {{ t("personalConnections.githubScope.discover") }}
    </button>
    <p v-if="busy" role="status">
      {{ t("personalConnections.githubScope.loading") }}
    </p>
    <p v-if="page">
      {{
        t("personalConnections.githubScope.identity", {
          login: page.login,
          page: page.page,
        })
      }}
    </p>
    <fieldset :disabled="busy">
      <legend>{{ t("personalConnections.githubScope.scope") }}</legend>
      <label
        ><input
          v-model="kind"
          type="radio"
          :name="`${prefix}-kind`"
          value="ALL_ACCESSIBLE"
        />{{ t("personalConnections.githubScope.all") }}</label
      >
      <label
        ><input
          v-model="kind"
          type="radio"
          :name="`${prefix}-kind`"
          value="SELECTED"
        />{{ t("personalConnections.githubScope.selected") }}</label
      >
      <p v-if="kind === 'SELECTED'">
        {{
          t("personalConnections.githubScope.count", { count: selected.length })
        }}
      </p>
      <ul v-if="page">
        <li v-for="repo in page.items" :key="repo.id">
          <label>
            <input
              type="checkbox"
              :checked="selected.some((item) => item.id === repo.id)"
              :disabled="
                kind !== 'SELECTED' ||
                (selected.length >= 100 &&
                  !selected.some((item) => item.id === repo.id))
              "
              @change="toggle(repo)"
            />{{ repo.fullName }} ·
            {{
              t(
                `personalConnections.githubScope.${repo.ownerKind === "Organization" ? "organization" : "personal"}`,
              )
            }}<template v-if="repo.private">
              · {{ t("personalConnections.githubScope.private") }}</template
            ><template v-if="repo.archived">
              · {{ t("personalConnections.githubScope.archived") }}</template
            >
          </label>
        </li>
      </ul>
      <p v-if="page && !page.items.length">
        {{ t("personalConnections.githubScope.empty") }}
      </p>
      <button
        v-if="page?.nextPage"
        class="button"
        type="button"
        :disabled="!canDiscover"
        @click="discover(page.nextPage)"
      >
        {{ t("personalConnections.githubScope.next") }}
      </button>
      <button
        class="button"
        type="button"
        :disabled="kind === 'SELECTED' && !selected.length"
        @click="save"
      >
        {{ t("personalConnections.githubScope.save") }}
      </button>
    </fieldset>
    <p v-if="saved" role="status">
      {{ t("personalConnections.githubScope.saved") }}
    </p>
    <p class="muted">{{ t("personalConnections.githubScope.limits") }}</p>
  </section>
</template>

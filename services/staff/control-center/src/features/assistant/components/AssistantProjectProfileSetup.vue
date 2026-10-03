<script setup lang="ts">
defineProps<{ busy: boolean }>();
const name = defineModel<string>("name", { required: true });
const purpose = defineModel<string>("purpose", { required: true });
const instructions = defineModel<string>("instructions", { required: true });
defineEmits<{ create: [] }>();
</script>

<template>
  <form class="assistant-project-profile" @submit.prevent="$emit('create')">
    <h3>{{ $t("assistant.projectProfile.createTitle") }}</h3>
    <p>{{ $t("assistant.projectProfile.createHelp") }}</p>
    <label
      >{{ $t("common.name")
      }}<input
        v-model="name"
        name="project-assistant-name"
        maxlength="160"
        required
        :disabled="busy"
    /></label>
    <label
      >{{ $t("common.purpose")
      }}<textarea
        v-model="purpose"
        name="project-assistant-purpose"
        rows="3"
        maxlength="2000"
        required
        :disabled="busy"
      />
    </label>
    <label
      >{{ $t("assistant.settings.instructions")
      }}<textarea
        v-model="instructions"
        name="project-assistant-instructions"
        rows="4"
        maxlength="32768"
        required
        :disabled="busy"
      />
    </label>
    <button
      class="button button--primary"
      type="submit"
      :disabled="
        busy || !name.trim() || !purpose.trim() || !instructions.trim()
      "
    >
      {{ $t("assistant.projectProfile.create") }}
    </button>
  </form>
</template>

<style scoped>
.assistant-project-profile {
  display: grid;
  gap: 12px;
  padding: 20px;
  width: min(100%, 680px);
  margin: 0 auto;
  overflow: auto;
}
.assistant-project-profile h3,
.assistant-project-profile p {
  margin: 0;
}
.assistant-project-profile label {
  display: grid;
  gap: 6px;
}
.assistant-project-profile .button {
  justify-self: start;
}
</style>

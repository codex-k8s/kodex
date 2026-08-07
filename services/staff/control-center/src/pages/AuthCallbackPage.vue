<script setup lang="ts">
import { LoaderCircle, RotateCcw } from "@lucide/vue";
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";

import { useSessionStore } from "@/features/session/store";

const router = useRouter();
const session = useSessionStore();
const failed = ref(false);

async function complete(): Promise<void> {
  failed.value = false;
  try {
    await session.completeLogin();
    await router.replace("/");
  } catch {
    failed.value = true;
  }
}

onMounted(complete);
</script>

<template>
  <main class="auth-gate">
    <section class="auth-gate__panel">
      <div class="brand-mark" aria-hidden="true">M</div>
      <template v-if="!failed">
        <LoaderCircle class="spin" :size="30" aria-hidden="true" />
        <h1>{{ $t("auth.callback") }}</h1>
      </template>
      <template v-else>
        <h1>{{ $t("auth.callbackError") }}</h1>
        <button
          class="button button--primary"
          type="button"
          @click="session.beginLogin"
        >
          <RotateCcw :size="16" aria-hidden="true" />{{ $t("auth.retryLogin") }}
        </button>
      </template>
    </section>
  </main>
</template>

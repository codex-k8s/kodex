<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import { useSessionStore } from "@/features/session/store";
import type { AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const session = useSessionStore();
const platform = usePlatformStore();
const router = useRouter();
const problem = ref<AppProblem>();

onMounted(async () => {
  try {
    await session.completeLogin();
    await platform.loadBootstrap();
    await router.replace(
      platform.bootstrap?.onboardingComplete ? "/" : "/onboarding",
    );
  } catch {
    problem.value = session.problem;
  }
});
</script>

<template>
  <main class="auth-gate">
    <section class="auth-card">
      <div class="brand-mark" aria-hidden="true">M</div>
      <h1>{{ $t("auth.callback") }}</h1>
      <p v-if="!problem" role="status">{{ $t("common.loading") }}</p>
      <ProblemNotice v-else :problem="problem" />
    </section>
  </main>
</template>

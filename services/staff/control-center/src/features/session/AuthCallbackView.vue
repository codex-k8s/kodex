<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import { useSessionStore } from "@/features/session/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

import { callbackReturnPath } from "./callback";

const session = useSessionStore();
const platform = usePlatformStore();
const router = useRouter();
const problem = ref<AppProblem>();

async function restartLogin(): Promise<void> {
  problem.value = undefined;
  try {
    await session.beginLogin();
  } catch (error) {
    problem.value = asProblem(error);
  }
}

onMounted(async () => {
  try {
    const completion = await session.completeLogin();
    if (completion.kind === "runtime-secret") {
      await router.replace(callbackReturnPath(completion));
      return;
    }
    await platform.loadBootstrap();
    await router.replace(
      callbackReturnPath(
        completion,
        platform.bootstrap?.onboardingComplete ?? false,
      ),
    );
  } catch (error) {
    problem.value = session.problem ?? asProblem(error);
  }
});
</script>

<template>
  <main class="auth-gate">
    <section class="auth-card">
      <div class="brand-mark" aria-hidden="true">
        <img src="/logo.png" alt="" />
      </div>
      <h1>{{ $t("auth.callback") }}</h1>
      <p v-if="!problem" role="status">{{ $t("common.loading") }}</p>
      <ProblemNotice v-else :problem="problem" @retry="restartLogin" />
    </section>
  </main>
</template>

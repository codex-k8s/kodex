<script setup lang="ts">
import { provide, ref } from "vue";
import VoiceTextarea from "../../src/shared/ui/VoiceTextarea.vue";
import CodeEditor from "../../src/shared/ui/CodeEditor.vue";
import CodeDiff from "../../src/shared/ui/CodeDiff.vue";
import AsyncEntityPicker from "../../src/shared/ui/AsyncEntityPicker.vue";
import ConfigurationFields from "../../src/features/managed-configurations/ConfigurationFields.vue";
import type {
  AsyncEntityOptionPage,
  AsyncEntityLoader,
  AsyncEntityPickerItem,
} from "../../src/shared/ui/async-entity-picker";
import { voiceContextKey } from "../../src/shared/ui/voice-input";
const available = ref(true);
const disabled = ref(false);
const failing = ref(false);
const delayed = ref(false);
const text = ref("Начало конец");
const code = ref("Начало конец");
const sensitive = ref("Тест");
const calls = ref(0);
const selected = ref<string | null | readonly string[]>([]);
const inlineVisible = ref(false);
const inlineDisabled = ref(true);
const inlineSelected = ref<string | null | readonly string[]>([]);
const inlineCalls = ref(0);
const managedVisible = ref(false);
const managedDisabled = ref(false);
const managedContent = ref(
  JSON.stringify({
    name: "STT",
    description: "Начало конец",
    stt: {
      parameters: {
        languages: ["ru"],
        keywords: ["Kodex"],
        prompt: "Контекст",
      },
    },
  }),
);
const loadInline: AsyncEntityLoader<AsyncEntityPickerItem> = async (
  request,
) => {
  inlineCalls.value++;
  const page = await loadPicker(request.query);
  return {
    items: page.items.map((item) => ({
      id: item.ref,
      label: item.title,
      description: item.description ?? item.disabledReason,
      disabled: item.disabled,
    })),
  };
};
function loadPicker(query: string): Promise<AsyncEntityOptionPage> {
  return Promise.resolve({
    items: [
      { ref: "first", title: "Первое окружение", description: "Ревизия 1" },
      { ref: "second", title: "Второе окружение", description: "Ревизия 2" },
      {
        ref: "disabled",
        title: "Закрытое окружение",
        disabled: true,
        disabledReason: "Нет разрешения",
      },
    ].filter((item) =>
      item.title.toLocaleLowerCase().includes(query.toLocaleLowerCase()),
    ),
  });
}
provide(voiceContextKey, {
  available,
  async transcribe(audio, signal) {
    if (!audio.size) throw new Error("Empty synthetic capture");
    calls.value += 1;
    if (delayed.value)
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(resolve, 1000);
        signal.addEventListener(
          "abort",
          () => {
            clearTimeout(timer);
            reject(new DOMException("Cancelled", "AbortError"));
          },
          { once: true },
        );
      });
    if (signal.aborted || failing.value)
      throw new Error("Synthetic transcription failed");
    return "диктовка ";
  },
});
</script>
<template>
  <main class="voice-fixture">
    <label><input v-model="available" type="checkbox" />Доступность</label>
    <label><input v-model="disabled" type="checkbox" />Блокировка</label>
    <label><input v-model="failing" type="checkbox" />Ошибка</label>
    <label><input v-model="delayed" type="checkbox" />Задержка</label>
    <output data-testid="calls">{{ calls }}</output>
    <section data-testid="picker">
      <AsyncEntityPicker
        v-model="selected"
        multiple
        :load-page="loadPicker"
        trigger-label="Окружения"
        placeholder="Выберите окружения"
        search-placeholder="Поиск окружений"
      />
      <output data-testid="selection">{{ JSON.stringify(selected) }}</output>
    </section>
    <button type="button" @click="inlineVisible = !inlineVisible">
      {{ inlineVisible ? "Скрыть список" : "Показать список" }}
    </button>
    <section v-if="inlineVisible" data-testid="inline-picker">
      <label
        ><input v-model="inlineDisabled" type="checkbox" />Заблокировать
        список</label
      >
      <AsyncEntityPicker
        v-model="inlineSelected"
        :load-items="loadInline"
        :disabled="inlineDisabled"
        multiple
        :labels="{
          label: 'Выбор окружений',
          searchPlaceholder: 'Поиск окружений',
          loading: 'Загрузка',
          loadingMore: 'Загрузка',
          empty: 'Пусто',
          error: 'Ошибка',
          retry: 'Повторить',
        }"
      />
      <output data-testid="inline-selection">{{
        JSON.stringify(inlineSelected)
      }}</output>
      <output data-testid="inline-calls">{{ inlineCalls }}</output>
    </section>
    <section data-testid="textarea">
      <VoiceTextarea v-model="text" aria-label="Обычный текст" rows="6" />
    </section>
    <section data-testid="code">
      <CodeEditor v-model="code" label="Код" language="yaml" />
    </section>
    <section data-testid="diff">
      <CodeDiff
        original="Сохранённый текст"
        :modified="code"
        label="Изменения"
      />
    </section>
    <section data-testid="secret">
      <VoiceTextarea
        v-model="sensitive"
        sensitive
        aria-label="Секрет"
      /><CodeEditor v-model="sensitive" label="Чувствительный JSON" sensitive />
    </section>
    <section data-testid="readonly">
      <VoiceTextarea model-value="Только чтение" readonly /><CodeEditor
        model-value="{}"
        label="Readonly"
        readonly
      />
    </section>
    <fieldset :disabled="disabled" data-testid="fieldset">
      <VoiceTextarea v-model="sensitive" aria-label="Внутри fieldset" />
    </fieldset>
    <button type="button" @click="managedVisible = !managedVisible">
      {{ managedVisible ? "Скрыть конфигурацию" : "Показать конфигурацию" }}
    </button>
    <section v-if="managedVisible" data-testid="managed-fields">
      <label
        ><input v-model="managedDisabled" type="checkbox" />Блокировка
        конфигурации</label
      >
      <ConfigurationFields
        v-model="managedContent"
        kind="SYSTEM_STT"
        name="STT"
        format="JSON"
        :disabled="managedDisabled"
      />
    </section>
  </main>
</template>
<style scoped>
.voice-fixture {
  margin: 24px auto;
  padding: 16px;
  max-width: 960px;
  display: grid;
  gap: 16px;
}
.voice-fixture section {
  min-width: 0;
}
</style>

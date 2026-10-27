import { computed, ref } from "vue";

// ImpactEditabilityPage содержит поля плана, необходимые для проверки срока.
type ImpactEditabilityPage = { plan: { state: string; expiresAt: string } };

// ImpactEditabilityInputs задаёт только общие условия редактирования плана.
interface ImpactEditabilityInputs {
  busy: () => boolean | undefined;
  loading: () => boolean;
  problem: () => unknown;
  page: () => ImpactEditabilityPage | undefined;
}

// createImpactEditability обновляет часы и проверяет PREPARED до срока действия.
// Выбор, полномочия, операции и вызов stop при unmount принадлежат компоненту.
export function createImpactEditability(inputs: ImpactEditabilityInputs) {
  const now = ref(Date.now());
  const clock = setInterval(() => {
    now.value = Date.now();
  }, 1000);
  const editable = computed(
    () =>
      !inputs.busy() &&
      !inputs.loading() &&
      !inputs.problem() &&
      inputs.page()?.plan.state === "PREPARED" &&
      Date.parse((inputs.page() as ImpactEditabilityPage).plan.expiresAt) >
        now.value,
  );
  return { now, editable, stop: () => clearInterval(clock) };
}

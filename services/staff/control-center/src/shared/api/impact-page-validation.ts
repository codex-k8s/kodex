// ImpactPageEnvelope описывает только общие границы страницы и её плана.
interface ImpactPageEnvelope<Plan extends { state: string; total: number }> {
  plan: Plan;
  total: number;
  items: readonly { ref: string }[];
  nextPageToken?: string;
}

// hasInvalidImpactPageEnvelope проверяет идентичность плана, cursor, размер и
// уникальность ref. Авторизация, проверка плана и каждого элемента остаются
// у конкретного API boundary; функция не превращает отказ в пустую страницу.
export function hasInvalidImpactPageEnvelope<
  Plan extends { state: string; total: number },
>(
  page: ImpactPageEnvelope<Plan>,
  expected: Plan,
  identity: (plan: Plan) => string,
  cursor?: string,
): boolean {
  return (
    identity(page.plan) !== identity(expected) ||
    (cursor && page.plan.state !== expected.state) ||
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    page.total > page.plan.total ||
    (cursor && page.nextPageToken === cursor) ||
    new Set(page.items.map((item) => item.ref)).size !== page.items.length
  );
}

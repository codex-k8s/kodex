export interface ImpactPageSlice {
  readonly plan: { readonly state: string };
  readonly items: readonly { readonly ref: string }[];
  readonly total: number;
  readonly nextPageToken?: string;
}

// Объединяет уже проверенные страницы impact-плана в прежнем порядке. Сначала
// запоминает использованный cursor, затем отклоняет цикл, drift и повтор ref.
// Возвращает новую страницу с metadata последнего ответа; выбор элементов и
// проверка владельца остаются у вызывающего сценария и его API boundary.
export function appendImpactPage<Page extends ImpactPageSlice>(
  previous: Page,
  next: Page,
  cursors: Set<string>,
): Page {
  if (previous.nextPageToken) cursors.add(previous.nextPageToken);
  if (
    (next.nextPageToken && cursors.has(next.nextPageToken)) ||
    next.total !== previous.total ||
    next.plan.state !== previous.plan.state ||
    previous.items.length + next.items.length > next.total ||
    next.items.some((item) =>
      previous.items.some((old) => old.ref === item.ref),
    )
  )
    throw new Error("Publication impact pagination changed");
  return { ...next, items: [...previous.items, ...next.items] };
}

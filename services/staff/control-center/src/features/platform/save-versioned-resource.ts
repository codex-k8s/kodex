import { mutate, type MutationHeaders } from "@/shared/api/mutation";

interface GeneratedMutationResponse<T> {
  data?: T;
  error?: unknown;
  response?: Response;
}

/** Выбирает OCC-обновление либо создание ровно один раз; типизированный запрос,
 * отображение заголовков и обновление состояния остаются у вызывающего кода. */
export async function saveVersionedResource<
  Current extends { version: number },
  Result,
>(
  current: Current | undefined,
  update: (
    existing: Current,
    headers: MutationHeaders,
  ) => Promise<GeneratedMutationResponse<Result>>,
  create: (
    headers: MutationHeaders,
  ) => Promise<GeneratedMutationResponse<Result>>,
): Promise<NonNullable<Result>> {
  const readback = current
    ? await mutate((headers) => update(current, headers), current.version)
    : await mutate(create);
  return readback.data;
}

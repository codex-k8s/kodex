const failure = "Runtime provider catalog has no eligible exact account";
const accountPattern = /^pacc_[A-Za-z0-9_-]{3,91}$/;

export function workspaceProviderAccountRef(value = "") {
  if (typeof value !== "string" || (value && !accountPattern.test(value)))
    throw new Error("Runtime provider account reference is invalid");
  return value;
}

export function workspaceModelQuery(model, accountRef = "") {
  accountRef = workspaceProviderAccountRef(accountRef);
  const query = new URLSearchParams({
    query: model,
    providerDefinitionKey: "openai-codex",
    pageSize: "100",
  });
  if (accountRef) query.set("providerAccountRef", accountRef);
  return query;
}

export function workspaceProviderAccountCandidates(items, accountRef = "") {
  accountRef = workspaceProviderAccountRef(accountRef);
  const eligible = (Array.isArray(items) ? items : [])
    .filter(
      (item) =>
        item?.state === "AUTHORIZED" &&
        item.enabled === true &&
        item.ready === true &&
        accountPattern.test(item.ref ?? ""),
    )
    .map((item) => item.ref)
    .filter((ref, index, refs) => refs.indexOf(ref) === index)
    .sort();
  if (accountRef) return eligible.includes(accountRef) ? [accountRef] : [];
  return eligible;
}

export function selectWorkspaceProviderAccount(items, model, accountRef = "") {
  accountRef = workspaceProviderAccountRef(accountRef);
  const selected = items?.find(
    (item) => item.id === model && item.available === true,
  );
  const refs = selected?.eligibleProviderAccountRefs;
  if (!Array.isArray(refs)) throw new Error(failure);
  const candidate = accountRef || refs[0];
  if (
    typeof candidate !== "string" ||
    !accountPattern.test(candidate) ||
    !refs.includes(candidate)
  )
    throw new Error(failure);
  return candidate;
}

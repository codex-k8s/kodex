import type {
  RuntimeEnvironmentInput,
  RuntimeSecretDescriptor,
} from "@/shared/api/generated/openapi/types.gen";

const variableName = /^[A-Z_][A-Z0-9_]{0,126}$/;
const sha256 = /^[a-f0-9]{64}$/;

export interface EnvironmentFormProblem {
  field: string;
  message: string;
}

export function validateEnvironmentInput(
  input: RuntimeEnvironmentInput,
): EnvironmentFormProblem[] {
  const problems: EnvironmentFormProblem[] = [];
  if (!input.name.trim())
    problems.push({ field: "name", message: "runtime.errors.nameRequired" });
  const names = new Set<string>();
  for (const [index, item] of input.values.entries()) {
    if (!variableName.test(item.name))
      problems.push({
        field: `values.${String(index)}.name`,
        message: "runtime.errors.variableName",
      });
    if (names.has(item.name))
      problems.push({
        field: `values.${String(index)}.name`,
        message: "runtime.errors.duplicateVariable",
      });
    names.add(item.name);
  }
  for (const [index, item] of input.secretDescriptors.entries()) {
    validateSecret(item, index, names, problems);
    names.add(item.name);
  }
  return problems;
}

function validateSecret(
  item: RuntimeSecretDescriptor,
  index: number,
  names: Set<string>,
  problems: EnvironmentFormProblem[],
): void {
  if (!variableName.test(item.name))
    problems.push({
      field: `secretDescriptors.${String(index)}.name`,
      message: "runtime.errors.variableName",
    });
  if (names.has(item.name))
    problems.push({
      field: `secretDescriptors.${String(index)}.name`,
      message: "runtime.errors.duplicateVariable",
    });
  for (const field of [
    "secretName",
    "secretKey",
    "secretUid",
    "secretResourceVersion",
  ] as const) {
    if (!item[field].trim())
      problems.push({
        field: `secretDescriptors.${String(index)}.${field}`,
        message: "runtime.errors.secretDescriptorRequired",
      });
  }
  if (!sha256.test(item.contentSha256))
    problems.push({
      field: `secretDescriptors.${String(index)}.contentSha256`,
      message: "runtime.errors.sha256",
    });
}

export function emptySecretDescriptor(): RuntimeSecretDescriptor {
  return {
    name: "",
    secretName: "",
    secretKey: "",
    secretUid: "",
    secretResourceVersion: "",
    contentSha256: "",
  };
}

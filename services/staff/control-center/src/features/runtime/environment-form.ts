import type {
  RuntimeEnvironmentInput,
  RuntimeSecretBinding,
  RuntimeSecretDescriptor,
} from "@/shared/api/generated/openapi/types.gen";

const variableName = /^[A-Z_][A-Z0-9_]{0,126}$/;
const toolCommand = /^[A-Za-z0-9][A-Za-z0-9._+-]{0,159}$/;
const reservedVariablePrefixes = [
  "KODEX_",
  "CODEX_",
  "OPENAI_",
  "OTEL_",
  "AWS_",
  "AZURE_",
  "GOOGLE_",
  "KUBERNETES_",
];
const reservedVariableNames = new Set([
  "HOME",
  "PATH",
  "PWD",
  "SHELL",
  "USER",
  "LOGNAME",
  "TMPDIR",
  "HTTP_PROXY",
  "HTTPS_PROXY",
  "NO_PROXY",
  "SSL_CERT_FILE",
  "SSL_CERT_DIR",
]);

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
  if (!input.imageArtifactRef.trim())
    problems.push({
      field: "imageArtifactRef",
      message: "runtime.errors.imageRequired",
    });
  const names = new Set<string>();
  for (const [index, item] of input.values.entries()) {
    if (!variableName.test(item.name))
      problems.push({
        field: `values.${String(index)}.name`,
        message: "runtime.errors.variableName",
      });
    else if (isReservedVariableName(item.name))
      problems.push({
        field: `values.${String(index)}.name`,
        message: "runtime.errors.reservedVariableName",
      });
    if (names.has(item.name))
      problems.push({
        field: `values.${String(index)}.name`,
        message: "runtime.errors.duplicateVariable",
      });
    names.add(item.name);
  }
  for (const [index, item] of input.secretBindings.entries()) {
    validateSecret(item, index, names, problems);
    names.add(item.name);
  }
  const toolCommands = new Set<string>();
  for (const [index, item] of input.tools.entries()) {
    if (!item.name.trim() || item.name.trim() !== item.name)
      problems.push({
        field: `tools.${String(index)}.name`,
        message: "runtime.errors.toolNameRequired",
      });
    if (!toolCommand.test(item.command))
      problems.push({
        field: `tools.${String(index)}.command`,
        message: "runtime.errors.toolCommand",
      });
    if (
      !item.description.trim() ||
      item.description.trim() !== item.description
    )
      problems.push({
        field: `tools.${String(index)}.description`,
        message: "runtime.errors.toolDescriptionRequired",
      });
    if (toolCommands.has(item.command))
      problems.push({
        field: `tools.${String(index)}.command`,
        message: "runtime.errors.duplicateTool",
      });
    toolCommands.add(item.command);
  }
  return problems;
}

function validateSecret(
  item: RuntimeSecretBinding,
  index: number,
  names: Set<string>,
  problems: EnvironmentFormProblem[],
): void {
  if (!variableName.test(item.name))
    problems.push({
      field: `secretBindings.${String(index)}.name`,
      message: "runtime.errors.variableName",
    });
  else if (isReservedVariableName(item.name))
    problems.push({
      field: `secretBindings.${String(index)}.name`,
      message: "runtime.errors.reservedVariableName",
    });
  if (names.has(item.name))
    problems.push({
      field: `secretBindings.${String(index)}.name`,
      message: "runtime.errors.duplicateVariable",
    });
  if (!item.secretRef.trim())
    problems.push({
      field: `secretBindings.${String(index)}.secretRef`,
      message: "runtime.errors.secretBindingRequired",
    });
}

function isReservedVariableName(name: string): boolean {
  return (
    reservedVariableNames.has(name) ||
    reservedVariablePrefixes.some((prefix) => name.startsWith(prefix))
  );
}

export function emptySecretBinding(): RuntimeSecretBinding {
  return {
    name: "",
    secretRef: "",
  };
}

export function editableSecretBindings(
  descriptors: readonly RuntimeSecretDescriptor[],
): RuntimeSecretBinding[] {
  return descriptors.map(({ name, secretRef }) => ({ name, secretRef }));
}

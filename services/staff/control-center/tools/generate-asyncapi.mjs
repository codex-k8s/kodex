import { Parser } from "@asyncapi/parser";
import { load as loadYaml } from "js-yaml";
import {
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const controlCenterRoot = resolve(
  fileURLToPath(new URL("..", import.meta.url)),
);
const repositoryRoot = resolve(controlCenterRoot, "../../..");
const contractPath = resolve(
  repositoryRoot,
  "contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml",
);
const goOutput = resolve(
  repositoryRoot,
  "services/external/control-api-gateway/internal/transport/websocket/generated",
);
const typescriptOutput = resolve(
  controlCenterRoot,
  "src/shared/api/generated/asyncapi",
);
const generatedBoundary = resolve(repositoryRoot, "services");
const aliasSchemas = new Set(["OpaqueRef", "Timestamp"]);

function fail(message) {
  throw new Error(message);
}

function assertGeneratedPath(path) {
  if (!path.startsWith(`${generatedBoundary}/`)) {
    fail(`Generated output is outside the services boundary: ${path}`);
  }
}

function packageVersion(packageName) {
  const packagePath = resolve(
    controlCenterRoot,
    "node_modules",
    ...packageName.split("/"),
    "package.json",
  );
  return JSON.parse(readFileSync(packagePath, "utf8")).version;
}

function checkToolchain() {
  const packageManifest = JSON.parse(
    readFileSync(resolve(controlCenterRoot, "package.json"), "utf8"),
  );
  for (const packageName of ["@asyncapi/parser", "js-yaml"]) {
    const expected =
      packageManifest.dependencies?.[packageName] ??
      packageManifest.devDependencies?.[packageName];
    const actual = packageVersion(packageName);
    if (!expected || expected !== actual) {
      fail(
        `Unexpected ${packageName} version: expected ${expected ?? "missing"}, got ${actual}`,
      );
    }
  }
  process.stdout.write("AsyncAPI toolchain check passed\n");
}

function localSchemaRef(ref) {
  const prefix = "#/components/schemas/";
  if (typeof ref !== "string" || !ref.startsWith(prefix)) {
    fail(`Only local component schema refs are supported: ${String(ref)}`);
  }
  const name = ref.slice(prefix.length);
  if (!/^[A-Z][A-Za-z0-9]*$/.test(name)) {
    fail(`Unsafe schema ref: ${ref}`);
  }
  return name;
}

function pascalCase(value) {
  const result = value
    .split(/[^A-Za-z0-9]+/u)
    .filter(Boolean)
    .map((part) => `${part[0].toUpperCase()}${part.slice(1).toLowerCase()}`)
    .join("");
  return /^\d/u.test(result) ? `Value${result}` : result;
}

function fieldName(value) {
  if (!/^[A-Za-z][A-Za-z0-9]*$/.test(value)) {
    fail(`Unsafe property name: ${value}`);
  }
  return `${value[0].toUpperCase()}${value.slice(1)}`;
}

function goFileName(value) {
  return `${value.replace(/([a-z0-9])([A-Z])/gu, "$1_$2").toLowerCase()}.go`;
}

function scalarGoType(schema) {
  if (schema.$ref) {
    const name = localSchemaRef(schema.$ref);
    return aliasSchemas.has(name) ? "string" : name;
  }
  if (schema.type === "string") return "string";
  if (schema.type === "integer") {
    return schema.format === "int64" ? "int64" : "int";
  }
  if (schema.type === "number") return "float64";
  if (schema.type === "boolean") return "bool";
  if (schema.type === "object" && schema.additionalProperties === true) {
    return "map[string]any";
  }
  fail(`Unsupported Go schema shape: ${JSON.stringify(schema)}`);
}

function goType(schema, required) {
  if (schema.type === "array") {
    if (!schema.items) fail("Array items are required");
    return `[]${scalarGoType(schema.items)}`;
  }
  const value = scalarGoType(schema);
  if (
    !required &&
    schema.$ref &&
    !aliasSchemas.has(localSchemaRef(schema.$ref))
  ) {
    return `*${value}`;
  }
  if (!required && !schema.const && schema.type !== "boolean") {
    return `*${value}`;
  }
  return value;
}

function scalarTypescriptType(schema) {
  if (schema.$ref) {
    const name = localSchemaRef(schema.$ref);
    return aliasSchemas.has(name) ? "string" : name;
  }
  if (schema.const !== undefined) return JSON.stringify(schema.const);
  if (schema.type === "string") return "string";
  if (schema.type === "integer" || schema.type === "number") return "number";
  if (schema.type === "boolean") return "boolean";
  if (schema.type === "object" && schema.additionalProperties === true) {
    return "Record<string, unknown>";
  }
  fail(`Unsupported TypeScript schema shape: ${JSON.stringify(schema)}`);
}

function typescriptType(schema) {
  if (schema.type === "array") {
    if (!schema.items) fail("Array items are required");
    return `${scalarTypescriptType(schema.items)}[]`;
  }
  return scalarTypescriptType(schema);
}

function schemaRefs(schema) {
  const refs = [];
  for (const property of Object.values(schema.properties ?? {})) {
    const candidate = property.$ref ?? property.items?.$ref;
    if (!candidate) continue;
    const name = localSchemaRef(candidate);
    if (!aliasSchemas.has(name) && !refs.includes(name)) refs.push(name);
  }
  return refs;
}

function validateNamedSchema(name, schema) {
  if (!/^[A-Z][A-Za-z0-9]*$/.test(name)) fail(`Unsafe schema name: ${name}`);
  if (!schema || typeof schema !== "object") fail(`Schema ${name} is invalid`);
  if (schema.enum) {
    if (schema.type !== "string" || !Array.isArray(schema.enum)) {
      fail(`Enum ${name} must be a string enum`);
    }
    for (const value of schema.enum) {
      if (typeof value !== "string" || !/^[A-Z][A-Z0-9_]*$/.test(value)) {
        fail(`Enum ${name} has an unsafe value`);
      }
    }
    return;
  }
  if (aliasSchemas.has(name)) {
    if (schema.type !== "string") fail(`Alias ${name} must be a string`);
    return;
  }
  if (schema.type !== "object" || schema.additionalProperties !== false) {
    fail(`Schema ${name} must be a closed object`);
  }
  if (!schema.properties || typeof schema.properties !== "object") {
    fail(`Schema ${name} must define properties`);
  }
  const required = new Set(schema.required ?? []);
  for (const propertyName of required) {
    if (!(propertyName in schema.properties)) {
      fail(`Schema ${name} requires an unknown property ${propertyName}`);
    }
  }
  for (const [propertyName, property] of Object.entries(schema.properties)) {
    fieldName(propertyName);
    typescriptType(property);
  }
}

function generateGoSchema(name, schema) {
  const header =
    "// Code generated by generate-asyncapi.mjs. DO NOT EDIT.\n\npackage generated\n\n";
  if (schema.enum) {
    const constants = schema.enum
      .map(
        (value) =>
          `\t${name}${pascalCase(value)} ${name} = ${JSON.stringify(value)}`,
      )
      .join("\n");
    return `${header}type ${name} string\n\nconst (\n${constants}\n)\n`;
  }
  const required = new Set(schema.required ?? []);
  const fields = Object.entries(schema.properties)
    .map(([propertyName, property]) => {
      const optional = required.has(propertyName) ? "" : ",omitempty";
      return `\t${fieldName(propertyName)} ${goType(property, required.has(propertyName))} \`json:${JSON.stringify(`${propertyName}${optional}`)}\``;
    })
    .join("\n");
  return `${header}type ${name} struct {\n${fields}\n}\n`;
}

function generateTypescriptSchema(name, schema) {
  if (schema.enum) {
    const union = schema.enum.map((value) => JSON.stringify(value)).join(" | ");
    return `// Code generated by generate-asyncapi.mjs. DO NOT EDIT.\n\nexport type ${name} = ${union};\n`;
  }
  const refs = schemaRefs(schema).filter((ref) => ref !== name);
  const imports = refs
    .map((ref) => `import type { ${ref} } from "./${ref}";`)
    .join("\n");
  const required = new Set(schema.required ?? []);
  const fields = Object.entries(schema.properties)
    .map(([propertyName, property]) => {
      const optional = required.has(propertyName) ? "" : "?";
      return `  ${propertyName}${optional}: ${typescriptType(property)};`;
    })
    .join("\n");
  const prefix = imports ? `${imports}\n\n` : "";
  return `// Code generated by generate-asyncapi.mjs. DO NOT EDIT.\n\n${prefix}export interface ${name} {\n${fields}\n}\n`;
}

function resolveOperationStream(contract, operationName, operation) {
  if (operation.action !== "send") return undefined;
  const channelRef = operation.channel?.$ref;
  const channelPrefix = "#/channels/";
  if (typeof channelRef !== "string" || !channelRef.startsWith(channelPrefix)) {
    fail(`Operation ${operationName} has an unsupported channel ref`);
  }
  const channelName = channelRef.slice(channelPrefix.length);
  const channel = contract.channels?.[channelName];
  if (!channel)
    fail(`Operation ${operationName} references an unknown channel`);
  const payloads = operation.messages.map((message) => {
    const messagePrefix = `#/channels/${channelName}/messages/`;
    if (
      typeof message.$ref !== "string" ||
      !message.$ref.startsWith(messagePrefix)
    ) {
      fail(`Operation ${operationName} has an unsupported message ref`);
    }
    const channelMessageName = message.$ref.slice(messagePrefix.length);
    const componentRef = channel.messages?.[channelMessageName]?.$ref;
    const componentPrefix = "#/components/messages/";
    if (
      typeof componentRef !== "string" ||
      !componentRef.startsWith(componentPrefix)
    ) {
      fail(
        `Channel message ${channelMessageName} has an unsupported component ref`,
      );
    }
    const componentName = componentRef.slice(componentPrefix.length);
    const payloadRef =
      contract.components?.messages?.[componentName]?.payload?.$ref;
    return localSchemaRef(payloadRef);
  });
  return { name: fieldName(channelName), payloads };
}

function generateGoStream(stream) {
  const marker = `is${stream.name}`;
  const implementations = stream.payloads
    .map((payload) => `func (${payload}) ${marker}() {}`)
    .join("\n");
  return `// Code generated by generate-asyncapi.mjs. DO NOT EDIT.\n\npackage generated\n\ntype ${stream.name} interface {\n\t${marker}()\n}\n\n${implementations}\n`;
}

function generateTypescriptStream(stream) {
  const imports = stream.payloads
    .map((payload) => `import type { ${payload} } from "./${payload}";`)
    .join("\n");
  const union = stream.payloads.join(" | ");
  return `// Code generated by generate-asyncapi.mjs. DO NOT EDIT.\n\n${imports}\n\nexport type ${stream.name} = ${union};\n`;
}

async function validateContract(source) {
  const result = await new Parser().parse(source);
  for (const diagnostic of result.diagnostics) {
    const location = diagnostic.path?.length
      ? diagnostic.path.join(".")
      : "document";
    process.stderr.write(
      `AsyncAPI diagnostic ${diagnostic.severity} at ${location}: ${diagnostic.message}\n`,
    );
  }
  if (
    !result.document ||
    result.diagnostics.some(({ severity }) => severity === 0)
  ) {
    fail("AsyncAPI validation failed");
  }
}

async function main() {
  checkToolchain();
  if (process.argv.includes("--check-toolchain")) return;

  const source = readFileSync(contractPath, "utf8");
  await validateContract(source);
  if (process.argv.includes("--validate-only")) {
    process.stdout.write("AsyncAPI validation passed\n");
    return;
  }

  const contract = loadYaml(source, { json: true });
  const schemas = contract?.components?.schemas;
  if (!schemas || typeof schemas !== "object")
    fail("AsyncAPI schemas are missing");
  for (const [name, schema] of Object.entries(schemas)) {
    validateNamedSchema(name, schema);
  }
  const streams = Object.entries(contract.operations ?? {})
    .map(([name, operation]) =>
      resolveOperationStream(contract, name, operation),
    )
    .filter(Boolean);

  for (const output of [goOutput, typescriptOutput]) {
    assertGeneratedPath(output);
    rmSync(output, { force: true, recursive: true });
    mkdirSync(output, { recursive: true });
  }

  for (const [name, schema] of Object.entries(schemas)) {
    if (aliasSchemas.has(name)) continue;
    writeFileSync(
      resolve(goOutput, goFileName(name)),
      generateGoSchema(name, schema),
      { encoding: "utf8", mode: 0o644 },
    );
    writeFileSync(
      resolve(typescriptOutput, `${name}.ts`),
      generateTypescriptSchema(name, schema),
      { encoding: "utf8", mode: 0o644 },
    );
  }
  for (const stream of streams) {
    writeFileSync(
      resolve(goOutput, goFileName(stream.name)),
      generateGoStream(stream),
      { encoding: "utf8", mode: 0o644 },
    );
    writeFileSync(
      resolve(typescriptOutput, `${stream.name}.ts`),
      generateTypescriptStream(stream),
      { encoding: "utf8", mode: 0o644 },
    );
  }

  const goFiles = readdirSync(goOutput).filter((name) => name.endsWith(".go"));
  const typescriptFiles = readdirSync(typescriptOutput).filter((name) =>
    name.endsWith(".ts"),
  );
  if (goFiles.length !== typescriptFiles.length || goFiles.length < 1) {
    fail("AsyncAPI generation produced an incomplete model set");
  }
  process.stdout.write(
    `AsyncAPI codegen passed: ${goFiles.length} Go and ${typescriptFiles.length} TypeScript models\n`,
  );
}

await main();

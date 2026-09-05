// Сгенерировано tools/generate-integration-schema.mjs. Не редактировать.
var __getOwnPropNames = Object.getOwnPropertyNames;
var __commonJS = (cb, mod) => function __require() {
  try {
    return mod || (0, cb[__getOwnPropNames(cb)[0]])((mod = { exports: {} }).exports, mod), mod.exports;
  } catch (e) {
    throw mod = 0, e;
  }
};

// node_modules/ajv/dist/runtime/ucs2length.js
var require_ucs2length = __commonJS({
  "node_modules/ajv/dist/runtime/ucs2length.js"(exports) {
    "use strict";
    Object.defineProperty(exports, "__esModule", { value: true });
    function ucs2length(str) {
      const len = str.length;
      let length = 0;
      let pos = 0;
      let value;
      while (pos < len) {
        length++;
        value = str.charCodeAt(pos++);
        if (value >= 55296 && value <= 56319 && pos < len) {
          value = str.charCodeAt(pos);
          if ((value & 64512) === 56320)
            pos++;
        }
      }
      return length;
    }
    exports.default = ucs2length;
    ucs2length.code = 'require("ajv/dist/runtime/ucs2length").default';
  }
});

// node_modules/fast-deep-equal/index.js
var require_fast_deep_equal = __commonJS({
  "node_modules/fast-deep-equal/index.js"(exports, module) {
    "use strict";
    module.exports = function equal(a, b) {
      if (a === b) return true;
      if (a && b && typeof a == "object" && typeof b == "object") {
        if (a.constructor !== b.constructor) return false;
        var length, i, keys;
        if (Array.isArray(a)) {
          length = a.length;
          if (length != b.length) return false;
          for (i = length; i-- !== 0; )
            if (!equal(a[i], b[i])) return false;
          return true;
        }
        if (a.constructor === RegExp) return a.source === b.source && a.flags === b.flags;
        if (a.valueOf !== Object.prototype.valueOf) return a.valueOf() === b.valueOf();
        if (a.toString !== Object.prototype.toString) return a.toString() === b.toString();
        keys = Object.keys(a);
        length = keys.length;
        if (length !== Object.keys(b).length) return false;
        for (i = length; i-- !== 0; )
          if (!Object.prototype.hasOwnProperty.call(b, keys[i])) return false;
        for (i = length; i-- !== 0; ) {
          var key = keys[i];
          if (!equal(a[key], b[key])) return false;
        }
        return true;
      }
      return a !== a && b !== b;
    };
  }
});

// node_modules/ajv/dist/runtime/equal.js
var require_equal = __commonJS({
  "node_modules/ajv/dist/runtime/equal.js"(exports) {
    "use strict";
    Object.defineProperty(exports, "__esModule", { value: true });
    var equal = require_fast_deep_equal();
    equal.code = 'require("ajv/dist/runtime/equal").default';
    exports.default = equal;
  }
});

// integration-package.js
var validate = validate20;
var integration_package_default = validate20;
var schema31 = { "$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "https://kodex.dev/contracts/integrations/v1/integration-package.schema.json", "title": "Kodex Integration Package v1", "type": "object", "additionalProperties": false, "required": ["apiVersion", "kind", "metadata", "spec"], "properties": { "apiVersion": { "const": "integrations.kodex.io/v1" }, "kind": { "const": "IntegrationPackage" }, "metadata": { "type": "object", "additionalProperties": false, "required": ["key", "version", "origin"], "properties": { "key": { "$ref": "#/$defs/key" }, "version": { "type": "string", "pattern": "^[1-9][0-9]*\\.[0-9]+\\.[0-9]+$", "maxLength": 32 }, "origin": { "const": "SHIPPED" } } }, "spec": { "type": "object", "additionalProperties": false, "required": ["name", "description", "category", "adapter", "adapterOwner", "executionRoute", "readiness", "configurationFields", "networkDestinations", "healthCheck", "capabilities"], "properties": { "name": { "type": "string", "minLength": 1, "maxLength": 120 }, "description": { "type": "string", "minLength": 1, "maxLength": 500 }, "category": { "$ref": "#/$defs/key" }, "adapter": { "enum": ["SYNTHETIC_HTTP", "GITHUB", "GITLAB", "JIRA", "CONFLUENCE", "EMAIL_HTTPS", "MATTERMOST_INTERACTION"] }, "adapterOwner": { "enum": ["integration-gateway", "interaction-gateway"] }, "executionRoute": { "enum": ["MANAGED_MCP", "INTERACTION"] }, "readiness": { "enum": ["READY", "NOT_READY"] }, "credential": { "$ref": "#/$defs/credential" }, "configurationFields": { "type": "array", "maxItems": 24, "items": { "$ref": "#/$defs/field", "type": "object", "properties": { "allowEmpty": { "const": false } } } }, "networkDestinations": { "type": "array", "minItems": 1, "maxItems": 16, "items": { "$ref": "#/$defs/networkDestination" } }, "healthCheck": { "$ref": "#/$defs/healthCheck" }, "capabilities": { "type": "array", "minItems": 1, "maxItems": 48, "items": { "$ref": "#/$defs/capability" } } } } }, "$defs": { "key": { "type": "string", "pattern": "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$", "maxLength": 120 }, "field": { "type": "object", "additionalProperties": false, "required": ["key", "type", "required"], "properties": { "key": { "$ref": "#/$defs/key" }, "type": { "enum": ["STRING", "INTEGER", "BOOLEAN"] }, "format": { "enum": ["PLAIN", "HTTPS_ORIGIN", "HTTPS_URL", "EMAIL", "HOST", "IDENTIFIER"] }, "required": { "type": "boolean" }, "maximumLength": { "type": "integer", "minimum": 1, "maximum": 65536 }, "minimum": { "type": "integer", "minimum": 0 }, "maximum": { "type": "integer", "minimum": 0 }, "allowedValues": { "type": "array", "minItems": 1, "maxItems": 32, "uniqueItems": true, "items": { "type": "string", "minLength": 1, "maxLength": 120 } } } }, "credential": { "type": "object", "additionalProperties": false, "required": ["secretKey", "kind"], "properties": { "secretKey": { "$ref": "#/$defs/key" }, "kind": { "enum": ["TOKEN", "PASSWORD"] } } }, "networkDestination": { "type": "object", "additionalProperties": false, "required": ["key", "source", "port", "tls"], "properties": { "key": { "$ref": "#/$defs/key" }, "source": { "enum": ["STATIC", "CONFIGURATION"] }, "hostname": { "type": "string", "pattern": "^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$", "maxLength": 253 }, "configurationField": { "$ref": "#/$defs/key" }, "port": { "type": "integer", "minimum": 1, "maximum": 65535 }, "tls": { "enum": ["REQUIRED", "NONE"] } }, "allOf": [{ "if": { "properties": { "source": { "const": "STATIC" } } }, "then": { "required": ["hostname"], "not": { "required": ["configurationField"] } } }, { "if": { "properties": { "source": { "const": "CONFIGURATION" } } }, "then": { "required": ["configurationField"], "not": { "required": ["hostname"] } } }] }, "healthCheck": { "type": "object", "additionalProperties": false, "required": ["operation", "timeoutSeconds", "maxAttempts"], "properties": { "operation": { "$ref": "#/$defs/key" }, "timeoutSeconds": { "type": "integer", "minimum": 1, "maximum": 60 }, "maxAttempts": { "type": "integer", "minimum": 1, "maximum": 3 } } }, "resourceScope": { "type": "object", "additionalProperties": false, "required": ["kind", "connectionFields"], "properties": { "kind": { "enum": ["SYNTHETIC_JOURNAL", "GITHUB_REPOSITORY", "GITLAB_PROJECT", "JIRA_PROJECT", "CONFLUENCE_SPACE", "EMAIL_SENDER", "MATTERMOST_CHANNEL"] }, "connectionFields": { "type": "array", "minItems": 1, "maxItems": 8, "uniqueItems": true, "items": { "$ref": "#/$defs/key" } } } }, "execution": { "type": "object", "additionalProperties": false, "required": ["idempotency", "timeoutSeconds", "maxAttempts", "retryBackoffMilliseconds"], "properties": { "idempotency": { "enum": ["READ_ONLY", "EFFECT_KEY", "PROVIDER_NATIVE"] }, "timeoutSeconds": { "type": "integer", "minimum": 1, "maximum": 120 }, "maxAttempts": { "type": "integer", "minimum": 1, "maximum": 4 }, "retryBackoffMilliseconds": { "type": "integer", "minimum": 50, "maximum": 5e3 } } }, "capability": { "type": "object", "additionalProperties": false, "required": ["key", "name", "description", "operation", "risk", "approvalPolicy", "resourceScope", "inputFields", "outputFields", "execution"], "properties": { "key": { "$ref": "#/$defs/key" }, "name": { "type": "string", "minLength": 1, "maxLength": 120 }, "description": { "type": "string", "minLength": 1, "maxLength": 500 }, "operation": { "$ref": "#/$defs/key" }, "risk": { "enum": ["READ", "WRITE", "SENSITIVE", "DESTRUCTIVE"] }, "approvalPolicy": { "enum": ["NONE", "HUMAN_EACH_EFFECT"] }, "resourceScope": { "$ref": "#/$defs/resourceScope" }, "inputFields": { "type": "array", "maxItems": 24, "items": { "$ref": "#/$defs/field" } }, "outputFields": { "type": "array", "minItems": 1, "maxItems": 24, "items": { "$ref": "#/$defs/field" } }, "execution": { "$ref": "#/$defs/execution" } } } } };
var func1 = require_ucs2length().default;
var func3 = Object.prototype.hasOwnProperty;
var pattern4 = new RegExp("^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$", "u");
var pattern5 = new RegExp("^[1-9][0-9]*\\.[0-9]+\\.[0-9]+$", "u");
var schema34 = { "type": "object", "additionalProperties": false, "required": ["secretKey", "kind"], "properties": { "secretKey": { "$ref": "#/$defs/key" }, "kind": { "enum": ["TOKEN", "PASSWORD"] } } };
function validate21(data, { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {}) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate21.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = void 0;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = void 0;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.secretKey === void 0) {
      const err0 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "secretKey" }, message: "must have required property 'secretKey'" };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    if (data.kind === void 0) {
      const err1 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "kind" }, message: "must have required property 'kind'" };
      if (vErrors === null) {
        vErrors = [err1];
      } else {
        vErrors.push(err1);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!(key0 === "secretKey" || key0 === "kind")) {
        const err2 = { instancePath, schemaPath: "#/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key0 }, message: "must NOT have additional properties" };
        if (vErrors === null) {
          vErrors = [err2];
        } else {
          vErrors.push(err2);
        }
        errors++;
      }
    }
    if (data.secretKey !== void 0) {
      let data0 = data.secretKey;
      if (typeof data0 === "string") {
        if (func1(data0) > 120) {
          const err3 = { instancePath: instancePath + "/secretKey", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err3];
          } else {
            vErrors.push(err3);
          }
          errors++;
        }
        if (!pattern4.test(data0)) {
          const err4 = { instancePath: instancePath + "/secretKey", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$"' };
          if (vErrors === null) {
            vErrors = [err4];
          } else {
            vErrors.push(err4);
          }
          errors++;
        }
      } else {
        const err5 = { instancePath: instancePath + "/secretKey", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err5];
        } else {
          vErrors.push(err5);
        }
        errors++;
      }
    }
    if (data.kind !== void 0) {
      let data1 = data.kind;
      if (!(data1 === "TOKEN" || data1 === "PASSWORD")) {
        const err6 = { instancePath: instancePath + "/kind", schemaPath: "#/properties/kind/enum", keyword: "enum", params: { allowedValues: schema34.properties.kind.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err6];
        } else {
          vErrors.push(err6);
        }
        errors++;
      }
    }
  } else {
    const err7 = { instancePath, schemaPath: "#/type", keyword: "type", params: { type: "object" }, message: "must be object" };
    if (vErrors === null) {
      vErrors = [err7];
    } else {
      vErrors.push(err7);
    }
    errors++;
  }
  validate21.errors = vErrors;
  return errors === 0;
}
validate21.evaluated = { "props": true, "dynamicProps": false, "dynamicItems": false };
var schema36 = { "type": "object", "additionalProperties": false, "required": ["key", "type", "required"], "properties": { "key": { "$ref": "#/$defs/key" }, "type": { "enum": ["STRING", "INTEGER", "BOOLEAN"] }, "format": { "enum": ["PLAIN", "HTTPS_ORIGIN", "HTTPS_URL", "EMAIL", "HOST", "IDENTIFIER"] }, "required": { "type": "boolean" }, "maximumLength": { "type": "integer", "minimum": 1, "maximum": 65536 }, "minimum": { "type": "integer", "minimum": 0 }, "maximum": { "type": "integer", "minimum": 0 }, "allowedValues": { "type": "array", "minItems": 1, "maxItems": 32, "uniqueItems": true, "items": { "type": "string", "minLength": 1, "maxLength": 120 } } } };
function validate23(data, { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {}) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate23.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = void 0;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = void 0;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.key === void 0) {
      const err0 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "key" }, message: "must have required property 'key'" };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    if (data.type === void 0) {
      const err1 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "type" }, message: "must have required property 'type'" };
      if (vErrors === null) {
        vErrors = [err1];
      } else {
        vErrors.push(err1);
      }
      errors++;
    }
    if (data.required === void 0) {
      const err2 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "required" }, message: "must have required property 'required'" };
      if (vErrors === null) {
        vErrors = [err2];
      } else {
        vErrors.push(err2);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!(key0 === "key" || key0 === "type" || key0 === "format" || key0 === "required" || key0 === "maximumLength" || key0 === "minimum" || key0 === "maximum" || key0 === "allowedValues")) {
        const err3 = { instancePath, schemaPath: "#/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key0 }, message: "must NOT have additional properties" };
        if (vErrors === null) {
          vErrors = [err3];
        } else {
          vErrors.push(err3);
        }
        errors++;
      }
    }
    if (data.key !== void 0) {
      let data0 = data.key;
      if (typeof data0 === "string") {
        if (func1(data0) > 120) {
          const err4 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err4];
          } else {
            vErrors.push(err4);
          }
          errors++;
        }
        if (!pattern4.test(data0)) {
          const err5 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$"' };
          if (vErrors === null) {
            vErrors = [err5];
          } else {
            vErrors.push(err5);
          }
          errors++;
        }
      } else {
        const err6 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err6];
        } else {
          vErrors.push(err6);
        }
        errors++;
      }
    }
    if (data.type !== void 0) {
      let data1 = data.type;
      if (!(data1 === "STRING" || data1 === "INTEGER" || data1 === "BOOLEAN")) {
        const err7 = { instancePath: instancePath + "/type", schemaPath: "#/properties/type/enum", keyword: "enum", params: { allowedValues: schema36.properties.type.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err7];
        } else {
          vErrors.push(err7);
        }
        errors++;
      }
    }
    if (data.format !== void 0) {
      let data2 = data.format;
      if (!(data2 === "PLAIN" || data2 === "HTTPS_ORIGIN" || data2 === "HTTPS_URL" || data2 === "EMAIL" || data2 === "HOST" || data2 === "IDENTIFIER")) {
        const err8 = { instancePath: instancePath + "/format", schemaPath: "#/properties/format/enum", keyword: "enum", params: { allowedValues: schema36.properties.format.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err8];
        } else {
          vErrors.push(err8);
        }
        errors++;
      }
    }
    if (data.required !== void 0) {
      if (typeof data.required !== "boolean") {
        const err9 = { instancePath: instancePath + "/required", schemaPath: "#/properties/required/type", keyword: "type", params: { type: "boolean" }, message: "must be boolean" };
        if (vErrors === null) {
          vErrors = [err9];
        } else {
          vErrors.push(err9);
        }
        errors++;
      }
    }
    if (data.maximumLength !== void 0) {
      let data4 = data.maximumLength;
      if (!(typeof data4 == "number" && (!(data4 % 1) && !isNaN(data4)) && isFinite(data4))) {
        const err10 = { instancePath: instancePath + "/maximumLength", schemaPath: "#/properties/maximumLength/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
        if (vErrors === null) {
          vErrors = [err10];
        } else {
          vErrors.push(err10);
        }
        errors++;
      }
      if (typeof data4 == "number" && isFinite(data4)) {
        if (data4 > 65536 || isNaN(data4)) {
          const err11 = { instancePath: instancePath + "/maximumLength", schemaPath: "#/properties/maximumLength/maximum", keyword: "maximum", params: { comparison: "<=", limit: 65536 }, message: "must be <= 65536" };
          if (vErrors === null) {
            vErrors = [err11];
          } else {
            vErrors.push(err11);
          }
          errors++;
        }
        if (data4 < 1 || isNaN(data4)) {
          const err12 = { instancePath: instancePath + "/maximumLength", schemaPath: "#/properties/maximumLength/minimum", keyword: "minimum", params: { comparison: ">=", limit: 1 }, message: "must be >= 1" };
          if (vErrors === null) {
            vErrors = [err12];
          } else {
            vErrors.push(err12);
          }
          errors++;
        }
      }
    }
    if (data.minimum !== void 0) {
      let data5 = data.minimum;
      if (!(typeof data5 == "number" && (!(data5 % 1) && !isNaN(data5)) && isFinite(data5))) {
        const err13 = { instancePath: instancePath + "/minimum", schemaPath: "#/properties/minimum/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
        if (vErrors === null) {
          vErrors = [err13];
        } else {
          vErrors.push(err13);
        }
        errors++;
      }
      if (typeof data5 == "number" && isFinite(data5)) {
        if (data5 < 0 || isNaN(data5)) {
          const err14 = { instancePath: instancePath + "/minimum", schemaPath: "#/properties/minimum/minimum", keyword: "minimum", params: { comparison: ">=", limit: 0 }, message: "must be >= 0" };
          if (vErrors === null) {
            vErrors = [err14];
          } else {
            vErrors.push(err14);
          }
          errors++;
        }
      }
    }
    if (data.maximum !== void 0) {
      let data6 = data.maximum;
      if (!(typeof data6 == "number" && (!(data6 % 1) && !isNaN(data6)) && isFinite(data6))) {
        const err15 = { instancePath: instancePath + "/maximum", schemaPath: "#/properties/maximum/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
        if (vErrors === null) {
          vErrors = [err15];
        } else {
          vErrors.push(err15);
        }
        errors++;
      }
      if (typeof data6 == "number" && isFinite(data6)) {
        if (data6 < 0 || isNaN(data6)) {
          const err16 = { instancePath: instancePath + "/maximum", schemaPath: "#/properties/maximum/minimum", keyword: "minimum", params: { comparison: ">=", limit: 0 }, message: "must be >= 0" };
          if (vErrors === null) {
            vErrors = [err16];
          } else {
            vErrors.push(err16);
          }
          errors++;
        }
      }
    }
    if (data.allowedValues !== void 0) {
      let data7 = data.allowedValues;
      if (Array.isArray(data7)) {
        if (data7.length > 32) {
          const err17 = { instancePath: instancePath + "/allowedValues", schemaPath: "#/properties/allowedValues/maxItems", keyword: "maxItems", params: { limit: 32 }, message: "must NOT have more than 32 items" };
          if (vErrors === null) {
            vErrors = [err17];
          } else {
            vErrors.push(err17);
          }
          errors++;
        }
        if (data7.length < 1) {
          const err18 = { instancePath: instancePath + "/allowedValues", schemaPath: "#/properties/allowedValues/minItems", keyword: "minItems", params: { limit: 1 }, message: "must NOT have fewer than 1 items" };
          if (vErrors === null) {
            vErrors = [err18];
          } else {
            vErrors.push(err18);
          }
          errors++;
        }
        const len0 = data7.length;
        for (let i0 = 0; i0 < len0; i0++) {
          let data8 = data7[i0];
          if (typeof data8 === "string") {
            if (func1(data8) > 120) {
              const err19 = { instancePath: instancePath + "/allowedValues/" + i0, schemaPath: "#/properties/allowedValues/items/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
              if (vErrors === null) {
                vErrors = [err19];
              } else {
                vErrors.push(err19);
              }
              errors++;
            }
            if (func1(data8) < 1) {
              const err20 = { instancePath: instancePath + "/allowedValues/" + i0, schemaPath: "#/properties/allowedValues/items/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
              if (vErrors === null) {
                vErrors = [err20];
              } else {
                vErrors.push(err20);
              }
              errors++;
            }
          } else {
            const err21 = { instancePath: instancePath + "/allowedValues/" + i0, schemaPath: "#/properties/allowedValues/items/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err21];
            } else {
              vErrors.push(err21);
            }
            errors++;
          }
        }
        let i1 = data7.length;
        let j0;
        if (i1 > 1) {
          const indices0 = {};
          for (; i1--; ) {
            let item0 = data7[i1];
            if (typeof item0 !== "string") {
              continue;
            }
            if (typeof indices0[item0] == "number") {
              j0 = indices0[item0];
              const err22 = { instancePath: instancePath + "/allowedValues", schemaPath: "#/properties/allowedValues/uniqueItems", keyword: "uniqueItems", params: { i: i1, j: j0 }, message: "must NOT have duplicate items (items ## " + j0 + " and " + i1 + " are identical)" };
              if (vErrors === null) {
                vErrors = [err22];
              } else {
                vErrors.push(err22);
              }
              errors++;
              break;
            }
            indices0[item0] = i1;
          }
        }
      } else {
        const err23 = { instancePath: instancePath + "/allowedValues", schemaPath: "#/properties/allowedValues/type", keyword: "type", params: { type: "array" }, message: "must be array" };
        if (vErrors === null) {
          vErrors = [err23];
        } else {
          vErrors.push(err23);
        }
        errors++;
      }
    }
  } else {
    const err24 = { instancePath, schemaPath: "#/type", keyword: "type", params: { type: "object" }, message: "must be object" };
    if (vErrors === null) {
      vErrors = [err24];
    } else {
      vErrors.push(err24);
    }
    errors++;
  }
  validate23.errors = vErrors;
  return errors === 0;
}
validate23.evaluated = { "props": true, "dynamicProps": false, "dynamicItems": false };
var schema38 = { "type": "object", "additionalProperties": false, "required": ["key", "source", "port", "tls"], "properties": { "key": { "$ref": "#/$defs/key" }, "source": { "enum": ["STATIC", "CONFIGURATION"] }, "hostname": { "type": "string", "pattern": "^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$", "maxLength": 253 }, "configurationField": { "$ref": "#/$defs/key" }, "port": { "type": "integer", "minimum": 1, "maximum": 65535 }, "tls": { "enum": ["REQUIRED", "NONE"] } }, "allOf": [{ "if": { "properties": { "source": { "const": "STATIC" } } }, "then": { "required": ["hostname"], "not": { "required": ["configurationField"] } } }, { "if": { "properties": { "source": { "const": "CONFIGURATION" } } }, "then": { "required": ["configurationField"], "not": { "required": ["hostname"] } } }] };
var pattern10 = new RegExp("^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$", "u");
function validate25(data, { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {}) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate25.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = void 0;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = void 0;
  }
  const _errs2 = errors;
  let valid1 = true;
  const _errs3 = errors;
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.source !== void 0) {
      if ("STATIC" !== data.source) {
        const err0 = {};
        if (vErrors === null) {
          vErrors = [err0];
        } else {
          vErrors.push(err0);
        }
        errors++;
      }
    }
  }
  var _valid0 = _errs3 === errors;
  errors = _errs2;
  if (vErrors !== null) {
    if (_errs2) {
      vErrors.length = _errs2;
    } else {
      vErrors = null;
    }
  }
  if (_valid0) {
    const _errs5 = errors;
    const _errs6 = errors;
    const _errs7 = errors;
    if (data && typeof data == "object" && !Array.isArray(data)) {
      let missing0;
      if (data.configurationField === void 0 && (missing0 = "configurationField")) {
        const err1 = {};
        if (vErrors === null) {
          vErrors = [err1];
        } else {
          vErrors.push(err1);
        }
        errors++;
      }
    }
    var valid3 = _errs7 === errors;
    if (valid3) {
      const err2 = { instancePath, schemaPath: "#/allOf/0/then/not", keyword: "not", params: {}, message: "must NOT be valid" };
      if (vErrors === null) {
        vErrors = [err2];
      } else {
        vErrors.push(err2);
      }
      errors++;
    } else {
      errors = _errs6;
      if (vErrors !== null) {
        if (_errs6) {
          vErrors.length = _errs6;
        } else {
          vErrors = null;
        }
      }
    }
    if (data && typeof data == "object" && !Array.isArray(data)) {
      if (data.hostname === void 0) {
        const err3 = { instancePath, schemaPath: "#/allOf/0/then/required", keyword: "required", params: { missingProperty: "hostname" }, message: "must have required property 'hostname'" };
        if (vErrors === null) {
          vErrors = [err3];
        } else {
          vErrors.push(err3);
        }
        errors++;
      }
    }
    var _valid0 = _errs5 === errors;
    valid1 = _valid0;
  }
  if (!valid1) {
    const err4 = { instancePath, schemaPath: "#/allOf/0/if", keyword: "if", params: { failingKeyword: "then" }, message: 'must match "then" schema' };
    if (vErrors === null) {
      vErrors = [err4];
    } else {
      vErrors.push(err4);
    }
    errors++;
  }
  const _errs9 = errors;
  let valid4 = true;
  const _errs10 = errors;
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.source !== void 0) {
      if ("CONFIGURATION" !== data.source) {
        const err5 = {};
        if (vErrors === null) {
          vErrors = [err5];
        } else {
          vErrors.push(err5);
        }
        errors++;
      }
    }
  }
  var _valid1 = _errs10 === errors;
  errors = _errs9;
  if (vErrors !== null) {
    if (_errs9) {
      vErrors.length = _errs9;
    } else {
      vErrors = null;
    }
  }
  if (_valid1) {
    const _errs12 = errors;
    const _errs13 = errors;
    const _errs14 = errors;
    if (data && typeof data == "object" && !Array.isArray(data)) {
      let missing1;
      if (data.hostname === void 0 && (missing1 = "hostname")) {
        const err6 = {};
        if (vErrors === null) {
          vErrors = [err6];
        } else {
          vErrors.push(err6);
        }
        errors++;
      }
    }
    var valid6 = _errs14 === errors;
    if (valid6) {
      const err7 = { instancePath, schemaPath: "#/allOf/1/then/not", keyword: "not", params: {}, message: "must NOT be valid" };
      if (vErrors === null) {
        vErrors = [err7];
      } else {
        vErrors.push(err7);
      }
      errors++;
    } else {
      errors = _errs13;
      if (vErrors !== null) {
        if (_errs13) {
          vErrors.length = _errs13;
        } else {
          vErrors = null;
        }
      }
    }
    if (data && typeof data == "object" && !Array.isArray(data)) {
      if (data.configurationField === void 0) {
        const err8 = { instancePath, schemaPath: "#/allOf/1/then/required", keyword: "required", params: { missingProperty: "configurationField" }, message: "must have required property 'configurationField'" };
        if (vErrors === null) {
          vErrors = [err8];
        } else {
          vErrors.push(err8);
        }
        errors++;
      }
    }
    var _valid1 = _errs12 === errors;
    valid4 = _valid1;
  }
  if (!valid4) {
    const err9 = { instancePath, schemaPath: "#/allOf/1/if", keyword: "if", params: { failingKeyword: "then" }, message: 'must match "then" schema' };
    if (vErrors === null) {
      vErrors = [err9];
    } else {
      vErrors.push(err9);
    }
    errors++;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.key === void 0) {
      const err10 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "key" }, message: "must have required property 'key'" };
      if (vErrors === null) {
        vErrors = [err10];
      } else {
        vErrors.push(err10);
      }
      errors++;
    }
    if (data.source === void 0) {
      const err11 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "source" }, message: "must have required property 'source'" };
      if (vErrors === null) {
        vErrors = [err11];
      } else {
        vErrors.push(err11);
      }
      errors++;
    }
    if (data.port === void 0) {
      const err12 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "port" }, message: "must have required property 'port'" };
      if (vErrors === null) {
        vErrors = [err12];
      } else {
        vErrors.push(err12);
      }
      errors++;
    }
    if (data.tls === void 0) {
      const err13 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "tls" }, message: "must have required property 'tls'" };
      if (vErrors === null) {
        vErrors = [err13];
      } else {
        vErrors.push(err13);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!(key0 === "key" || key0 === "source" || key0 === "hostname" || key0 === "configurationField" || key0 === "port" || key0 === "tls")) {
        const err14 = { instancePath, schemaPath: "#/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key0 }, message: "must NOT have additional properties" };
        if (vErrors === null) {
          vErrors = [err14];
        } else {
          vErrors.push(err14);
        }
        errors++;
      }
    }
    if (data.key !== void 0) {
      let data2 = data.key;
      if (typeof data2 === "string") {
        if (func1(data2) > 120) {
          const err15 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err15];
          } else {
            vErrors.push(err15);
          }
          errors++;
        }
        if (!pattern4.test(data2)) {
          const err16 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$"' };
          if (vErrors === null) {
            vErrors = [err16];
          } else {
            vErrors.push(err16);
          }
          errors++;
        }
      } else {
        const err17 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err17];
        } else {
          vErrors.push(err17);
        }
        errors++;
      }
    }
    if (data.source !== void 0) {
      let data3 = data.source;
      if (!(data3 === "STATIC" || data3 === "CONFIGURATION")) {
        const err18 = { instancePath: instancePath + "/source", schemaPath: "#/properties/source/enum", keyword: "enum", params: { allowedValues: schema38.properties.source.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err18];
        } else {
          vErrors.push(err18);
        }
        errors++;
      }
    }
    if (data.hostname !== void 0) {
      let data4 = data.hostname;
      if (typeof data4 === "string") {
        if (func1(data4) > 253) {
          const err19 = { instancePath: instancePath + "/hostname", schemaPath: "#/properties/hostname/maxLength", keyword: "maxLength", params: { limit: 253 }, message: "must NOT have more than 253 characters" };
          if (vErrors === null) {
            vErrors = [err19];
          } else {
            vErrors.push(err19);
          }
          errors++;
        }
        if (!pattern10.test(data4)) {
          const err20 = { instancePath: instancePath + "/hostname", schemaPath: "#/properties/hostname/pattern", keyword: "pattern", params: { pattern: "^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$" }, message: 'must match pattern "^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$"' };
          if (vErrors === null) {
            vErrors = [err20];
          } else {
            vErrors.push(err20);
          }
          errors++;
        }
      } else {
        const err21 = { instancePath: instancePath + "/hostname", schemaPath: "#/properties/hostname/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err21];
        } else {
          vErrors.push(err21);
        }
        errors++;
      }
    }
    if (data.configurationField !== void 0) {
      let data5 = data.configurationField;
      if (typeof data5 === "string") {
        if (func1(data5) > 120) {
          const err22 = { instancePath: instancePath + "/configurationField", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err22];
          } else {
            vErrors.push(err22);
          }
          errors++;
        }
        if (!pattern4.test(data5)) {
          const err23 = { instancePath: instancePath + "/configurationField", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$"' };
          if (vErrors === null) {
            vErrors = [err23];
          } else {
            vErrors.push(err23);
          }
          errors++;
        }
      } else {
        const err24 = { instancePath: instancePath + "/configurationField", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err24];
        } else {
          vErrors.push(err24);
        }
        errors++;
      }
    }
    if (data.port !== void 0) {
      let data6 = data.port;
      if (!(typeof data6 == "number" && (!(data6 % 1) && !isNaN(data6)) && isFinite(data6))) {
        const err25 = { instancePath: instancePath + "/port", schemaPath: "#/properties/port/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
        if (vErrors === null) {
          vErrors = [err25];
        } else {
          vErrors.push(err25);
        }
        errors++;
      }
      if (typeof data6 == "number" && isFinite(data6)) {
        if (data6 > 65535 || isNaN(data6)) {
          const err26 = { instancePath: instancePath + "/port", schemaPath: "#/properties/port/maximum", keyword: "maximum", params: { comparison: "<=", limit: 65535 }, message: "must be <= 65535" };
          if (vErrors === null) {
            vErrors = [err26];
          } else {
            vErrors.push(err26);
          }
          errors++;
        }
        if (data6 < 1 || isNaN(data6)) {
          const err27 = { instancePath: instancePath + "/port", schemaPath: "#/properties/port/minimum", keyword: "minimum", params: { comparison: ">=", limit: 1 }, message: "must be >= 1" };
          if (vErrors === null) {
            vErrors = [err27];
          } else {
            vErrors.push(err27);
          }
          errors++;
        }
      }
    }
    if (data.tls !== void 0) {
      let data7 = data.tls;
      if (!(data7 === "REQUIRED" || data7 === "NONE")) {
        const err28 = { instancePath: instancePath + "/tls", schemaPath: "#/properties/tls/enum", keyword: "enum", params: { allowedValues: schema38.properties.tls.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err28];
        } else {
          vErrors.push(err28);
        }
        errors++;
      }
    }
  } else {
    const err29 = { instancePath, schemaPath: "#/type", keyword: "type", params: { type: "object" }, message: "must be object" };
    if (vErrors === null) {
      vErrors = [err29];
    } else {
      vErrors.push(err29);
    }
    errors++;
  }
  validate25.errors = vErrors;
  return errors === 0;
}
validate25.evaluated = { "props": true, "dynamicProps": false, "dynamicItems": false };
function validate27(data, { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {}) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate27.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = void 0;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = void 0;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.operation === void 0) {
      const err0 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "operation" }, message: "must have required property 'operation'" };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    if (data.timeoutSeconds === void 0) {
      const err1 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "timeoutSeconds" }, message: "must have required property 'timeoutSeconds'" };
      if (vErrors === null) {
        vErrors = [err1];
      } else {
        vErrors.push(err1);
      }
      errors++;
    }
    if (data.maxAttempts === void 0) {
      const err2 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "maxAttempts" }, message: "must have required property 'maxAttempts'" };
      if (vErrors === null) {
        vErrors = [err2];
      } else {
        vErrors.push(err2);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!(key0 === "operation" || key0 === "timeoutSeconds" || key0 === "maxAttempts")) {
        const err3 = { instancePath, schemaPath: "#/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key0 }, message: "must NOT have additional properties" };
        if (vErrors === null) {
          vErrors = [err3];
        } else {
          vErrors.push(err3);
        }
        errors++;
      }
    }
    if (data.operation !== void 0) {
      let data0 = data.operation;
      if (typeof data0 === "string") {
        if (func1(data0) > 120) {
          const err4 = { instancePath: instancePath + "/operation", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err4];
          } else {
            vErrors.push(err4);
          }
          errors++;
        }
        if (!pattern4.test(data0)) {
          const err5 = { instancePath: instancePath + "/operation", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$"' };
          if (vErrors === null) {
            vErrors = [err5];
          } else {
            vErrors.push(err5);
          }
          errors++;
        }
      } else {
        const err6 = { instancePath: instancePath + "/operation", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err6];
        } else {
          vErrors.push(err6);
        }
        errors++;
      }
    }
    if (data.timeoutSeconds !== void 0) {
      let data1 = data.timeoutSeconds;
      if (!(typeof data1 == "number" && (!(data1 % 1) && !isNaN(data1)) && isFinite(data1))) {
        const err7 = { instancePath: instancePath + "/timeoutSeconds", schemaPath: "#/properties/timeoutSeconds/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
        if (vErrors === null) {
          vErrors = [err7];
        } else {
          vErrors.push(err7);
        }
        errors++;
      }
      if (typeof data1 == "number" && isFinite(data1)) {
        if (data1 > 60 || isNaN(data1)) {
          const err8 = { instancePath: instancePath + "/timeoutSeconds", schemaPath: "#/properties/timeoutSeconds/maximum", keyword: "maximum", params: { comparison: "<=", limit: 60 }, message: "must be <= 60" };
          if (vErrors === null) {
            vErrors = [err8];
          } else {
            vErrors.push(err8);
          }
          errors++;
        }
        if (data1 < 1 || isNaN(data1)) {
          const err9 = { instancePath: instancePath + "/timeoutSeconds", schemaPath: "#/properties/timeoutSeconds/minimum", keyword: "minimum", params: { comparison: ">=", limit: 1 }, message: "must be >= 1" };
          if (vErrors === null) {
            vErrors = [err9];
          } else {
            vErrors.push(err9);
          }
          errors++;
        }
      }
    }
    if (data.maxAttempts !== void 0) {
      let data2 = data.maxAttempts;
      if (!(typeof data2 == "number" && (!(data2 % 1) && !isNaN(data2)) && isFinite(data2))) {
        const err10 = { instancePath: instancePath + "/maxAttempts", schemaPath: "#/properties/maxAttempts/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
        if (vErrors === null) {
          vErrors = [err10];
        } else {
          vErrors.push(err10);
        }
        errors++;
      }
      if (typeof data2 == "number" && isFinite(data2)) {
        if (data2 > 3 || isNaN(data2)) {
          const err11 = { instancePath: instancePath + "/maxAttempts", schemaPath: "#/properties/maxAttempts/maximum", keyword: "maximum", params: { comparison: "<=", limit: 3 }, message: "must be <= 3" };
          if (vErrors === null) {
            vErrors = [err11];
          } else {
            vErrors.push(err11);
          }
          errors++;
        }
        if (data2 < 1 || isNaN(data2)) {
          const err12 = { instancePath: instancePath + "/maxAttempts", schemaPath: "#/properties/maxAttempts/minimum", keyword: "minimum", params: { comparison: ">=", limit: 1 }, message: "must be >= 1" };
          if (vErrors === null) {
            vErrors = [err12];
          } else {
            vErrors.push(err12);
          }
          errors++;
        }
      }
    }
  } else {
    const err13 = { instancePath, schemaPath: "#/type", keyword: "type", params: { type: "object" }, message: "must be object" };
    if (vErrors === null) {
      vErrors = [err13];
    } else {
      vErrors.push(err13);
    }
    errors++;
  }
  validate27.errors = vErrors;
  return errors === 0;
}
validate27.evaluated = { "props": true, "dynamicProps": false, "dynamicItems": false };
var schema43 = { "type": "object", "additionalProperties": false, "required": ["key", "name", "description", "operation", "risk", "approvalPolicy", "resourceScope", "inputFields", "outputFields", "execution"], "properties": { "key": { "$ref": "#/$defs/key" }, "name": { "type": "string", "minLength": 1, "maxLength": 120 }, "description": { "type": "string", "minLength": 1, "maxLength": 500 }, "operation": { "$ref": "#/$defs/key" }, "risk": { "enum": ["READ", "WRITE", "SENSITIVE", "DESTRUCTIVE"] }, "approvalPolicy": { "enum": ["NONE", "HUMAN_EACH_EFFECT"] }, "resourceScope": { "$ref": "#/$defs/resourceScope" }, "inputFields": { "type": "array", "maxItems": 24, "items": { "$ref": "#/$defs/field" } }, "outputFields": { "type": "array", "minItems": 1, "maxItems": 24, "items": { "$ref": "#/$defs/field" } }, "execution": { "$ref": "#/$defs/execution" } } };
var schema48 = { "type": "object", "additionalProperties": false, "required": ["idempotency", "timeoutSeconds", "maxAttempts", "retryBackoffMilliseconds"], "properties": { "idempotency": { "enum": ["READ_ONLY", "EFFECT_KEY", "PROVIDER_NATIVE"] }, "timeoutSeconds": { "type": "integer", "minimum": 1, "maximum": 120 }, "maxAttempts": { "type": "integer", "minimum": 1, "maximum": 4 }, "retryBackoffMilliseconds": { "type": "integer", "minimum": 50, "maximum": 5e3 } } };
var schema46 = { "type": "object", "additionalProperties": false, "required": ["kind", "connectionFields"], "properties": { "kind": { "enum": ["SYNTHETIC_JOURNAL", "GITHUB_REPOSITORY", "GITLAB_PROJECT", "JIRA_PROJECT", "CONFLUENCE_SPACE", "EMAIL_SENDER", "MATTERMOST_CHANNEL"] }, "connectionFields": { "type": "array", "minItems": 1, "maxItems": 8, "uniqueItems": true, "items": { "$ref": "#/$defs/key" } } } };
var func0 = require_equal().default;
function validate30(data, { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {}) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate30.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = void 0;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = void 0;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.kind === void 0) {
      const err0 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "kind" }, message: "must have required property 'kind'" };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    if (data.connectionFields === void 0) {
      const err1 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "connectionFields" }, message: "must have required property 'connectionFields'" };
      if (vErrors === null) {
        vErrors = [err1];
      } else {
        vErrors.push(err1);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!(key0 === "kind" || key0 === "connectionFields")) {
        const err2 = { instancePath, schemaPath: "#/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key0 }, message: "must NOT have additional properties" };
        if (vErrors === null) {
          vErrors = [err2];
        } else {
          vErrors.push(err2);
        }
        errors++;
      }
    }
    if (data.kind !== void 0) {
      let data0 = data.kind;
      if (!(data0 === "SYNTHETIC_JOURNAL" || data0 === "GITHUB_REPOSITORY" || data0 === "GITLAB_PROJECT" || data0 === "JIRA_PROJECT" || data0 === "CONFLUENCE_SPACE" || data0 === "EMAIL_SENDER" || data0 === "MATTERMOST_CHANNEL")) {
        const err3 = { instancePath: instancePath + "/kind", schemaPath: "#/properties/kind/enum", keyword: "enum", params: { allowedValues: schema46.properties.kind.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err3];
        } else {
          vErrors.push(err3);
        }
        errors++;
      }
    }
    if (data.connectionFields !== void 0) {
      let data1 = data.connectionFields;
      if (Array.isArray(data1)) {
        if (data1.length > 8) {
          const err4 = { instancePath: instancePath + "/connectionFields", schemaPath: "#/properties/connectionFields/maxItems", keyword: "maxItems", params: { limit: 8 }, message: "must NOT have more than 8 items" };
          if (vErrors === null) {
            vErrors = [err4];
          } else {
            vErrors.push(err4);
          }
          errors++;
        }
        if (data1.length < 1) {
          const err5 = { instancePath: instancePath + "/connectionFields", schemaPath: "#/properties/connectionFields/minItems", keyword: "minItems", params: { limit: 1 }, message: "must NOT have fewer than 1 items" };
          if (vErrors === null) {
            vErrors = [err5];
          } else {
            vErrors.push(err5);
          }
          errors++;
        }
        const len0 = data1.length;
        for (let i0 = 0; i0 < len0; i0++) {
          let data2 = data1[i0];
          if (typeof data2 === "string") {
            if (func1(data2) > 120) {
              const err6 = { instancePath: instancePath + "/connectionFields/" + i0, schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
              if (vErrors === null) {
                vErrors = [err6];
              } else {
                vErrors.push(err6);
              }
              errors++;
            }
            if (!pattern4.test(data2)) {
              const err7 = { instancePath: instancePath + "/connectionFields/" + i0, schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$"' };
              if (vErrors === null) {
                vErrors = [err7];
              } else {
                vErrors.push(err7);
              }
              errors++;
            }
          } else {
            const err8 = { instancePath: instancePath + "/connectionFields/" + i0, schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err8];
            } else {
              vErrors.push(err8);
            }
            errors++;
          }
        }
        let i1 = data1.length;
        let j0;
        if (i1 > 1) {
          outer0: for (; i1--; ) {
            for (j0 = i1; j0--; ) {
              if (func0(data1[i1], data1[j0])) {
                const err9 = { instancePath: instancePath + "/connectionFields", schemaPath: "#/properties/connectionFields/uniqueItems", keyword: "uniqueItems", params: { i: i1, j: j0 }, message: "must NOT have duplicate items (items ## " + j0 + " and " + i1 + " are identical)" };
                if (vErrors === null) {
                  vErrors = [err9];
                } else {
                  vErrors.push(err9);
                }
                errors++;
                break outer0;
              }
            }
          }
        }
      } else {
        const err10 = { instancePath: instancePath + "/connectionFields", schemaPath: "#/properties/connectionFields/type", keyword: "type", params: { type: "array" }, message: "must be array" };
        if (vErrors === null) {
          vErrors = [err10];
        } else {
          vErrors.push(err10);
        }
        errors++;
      }
    }
  } else {
    const err11 = { instancePath, schemaPath: "#/type", keyword: "type", params: { type: "object" }, message: "must be object" };
    if (vErrors === null) {
      vErrors = [err11];
    } else {
      vErrors.push(err11);
    }
    errors++;
  }
  validate30.errors = vErrors;
  return errors === 0;
}
validate30.evaluated = { "props": true, "dynamicProps": false, "dynamicItems": false };
function validate29(data, { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {}) {
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate29.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = void 0;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = void 0;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.key === void 0) {
      const err0 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "key" }, message: "must have required property 'key'" };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    if (data.name === void 0) {
      const err1 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "name" }, message: "must have required property 'name'" };
      if (vErrors === null) {
        vErrors = [err1];
      } else {
        vErrors.push(err1);
      }
      errors++;
    }
    if (data.description === void 0) {
      const err2 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "description" }, message: "must have required property 'description'" };
      if (vErrors === null) {
        vErrors = [err2];
      } else {
        vErrors.push(err2);
      }
      errors++;
    }
    if (data.operation === void 0) {
      const err3 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "operation" }, message: "must have required property 'operation'" };
      if (vErrors === null) {
        vErrors = [err3];
      } else {
        vErrors.push(err3);
      }
      errors++;
    }
    if (data.risk === void 0) {
      const err4 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "risk" }, message: "must have required property 'risk'" };
      if (vErrors === null) {
        vErrors = [err4];
      } else {
        vErrors.push(err4);
      }
      errors++;
    }
    if (data.approvalPolicy === void 0) {
      const err5 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "approvalPolicy" }, message: "must have required property 'approvalPolicy'" };
      if (vErrors === null) {
        vErrors = [err5];
      } else {
        vErrors.push(err5);
      }
      errors++;
    }
    if (data.resourceScope === void 0) {
      const err6 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "resourceScope" }, message: "must have required property 'resourceScope'" };
      if (vErrors === null) {
        vErrors = [err6];
      } else {
        vErrors.push(err6);
      }
      errors++;
    }
    if (data.inputFields === void 0) {
      const err7 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "inputFields" }, message: "must have required property 'inputFields'" };
      if (vErrors === null) {
        vErrors = [err7];
      } else {
        vErrors.push(err7);
      }
      errors++;
    }
    if (data.outputFields === void 0) {
      const err8 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "outputFields" }, message: "must have required property 'outputFields'" };
      if (vErrors === null) {
        vErrors = [err8];
      } else {
        vErrors.push(err8);
      }
      errors++;
    }
    if (data.execution === void 0) {
      const err9 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "execution" }, message: "must have required property 'execution'" };
      if (vErrors === null) {
        vErrors = [err9];
      } else {
        vErrors.push(err9);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!func3.call(schema43.properties, key0)) {
        const err10 = { instancePath, schemaPath: "#/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key0 }, message: "must NOT have additional properties" };
        if (vErrors === null) {
          vErrors = [err10];
        } else {
          vErrors.push(err10);
        }
        errors++;
      }
    }
    if (data.key !== void 0) {
      let data0 = data.key;
      if (typeof data0 === "string") {
        if (func1(data0) > 120) {
          const err11 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err11];
          } else {
            vErrors.push(err11);
          }
          errors++;
        }
        if (!pattern4.test(data0)) {
          const err12 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$"' };
          if (vErrors === null) {
            vErrors = [err12];
          } else {
            vErrors.push(err12);
          }
          errors++;
        }
      } else {
        const err13 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err13];
        } else {
          vErrors.push(err13);
        }
        errors++;
      }
    }
    if (data.name !== void 0) {
      let data1 = data.name;
      if (typeof data1 === "string") {
        if (func1(data1) > 120) {
          const err14 = { instancePath: instancePath + "/name", schemaPath: "#/properties/name/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err14];
          } else {
            vErrors.push(err14);
          }
          errors++;
        }
        if (func1(data1) < 1) {
          const err15 = { instancePath: instancePath + "/name", schemaPath: "#/properties/name/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
          if (vErrors === null) {
            vErrors = [err15];
          } else {
            vErrors.push(err15);
          }
          errors++;
        }
      } else {
        const err16 = { instancePath: instancePath + "/name", schemaPath: "#/properties/name/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err16];
        } else {
          vErrors.push(err16);
        }
        errors++;
      }
    }
    if (data.description !== void 0) {
      let data2 = data.description;
      if (typeof data2 === "string") {
        if (func1(data2) > 500) {
          const err17 = { instancePath: instancePath + "/description", schemaPath: "#/properties/description/maxLength", keyword: "maxLength", params: { limit: 500 }, message: "must NOT have more than 500 characters" };
          if (vErrors === null) {
            vErrors = [err17];
          } else {
            vErrors.push(err17);
          }
          errors++;
        }
        if (func1(data2) < 1) {
          const err18 = { instancePath: instancePath + "/description", schemaPath: "#/properties/description/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
          if (vErrors === null) {
            vErrors = [err18];
          } else {
            vErrors.push(err18);
          }
          errors++;
        }
      } else {
        const err19 = { instancePath: instancePath + "/description", schemaPath: "#/properties/description/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err19];
        } else {
          vErrors.push(err19);
        }
        errors++;
      }
    }
    if (data.operation !== void 0) {
      let data3 = data.operation;
      if (typeof data3 === "string") {
        if (func1(data3) > 120) {
          const err20 = { instancePath: instancePath + "/operation", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err20];
          } else {
            vErrors.push(err20);
          }
          errors++;
        }
        if (!pattern4.test(data3)) {
          const err21 = { instancePath: instancePath + "/operation", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$"' };
          if (vErrors === null) {
            vErrors = [err21];
          } else {
            vErrors.push(err21);
          }
          errors++;
        }
      } else {
        const err22 = { instancePath: instancePath + "/operation", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err22];
        } else {
          vErrors.push(err22);
        }
        errors++;
      }
    }
    if (data.risk !== void 0) {
      let data4 = data.risk;
      if (!(data4 === "READ" || data4 === "WRITE" || data4 === "SENSITIVE" || data4 === "DESTRUCTIVE")) {
        const err23 = { instancePath: instancePath + "/risk", schemaPath: "#/properties/risk/enum", keyword: "enum", params: { allowedValues: schema43.properties.risk.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err23];
        } else {
          vErrors.push(err23);
        }
        errors++;
      }
    }
    if (data.approvalPolicy !== void 0) {
      let data5 = data.approvalPolicy;
      if (!(data5 === "NONE" || data5 === "HUMAN_EACH_EFFECT")) {
        const err24 = { instancePath: instancePath + "/approvalPolicy", schemaPath: "#/properties/approvalPolicy/enum", keyword: "enum", params: { allowedValues: schema43.properties.approvalPolicy.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err24];
        } else {
          vErrors.push(err24);
        }
        errors++;
      }
    }
    if (data.resourceScope !== void 0) {
      if (!validate30(data.resourceScope, { instancePath: instancePath + "/resourceScope", parentData: data, parentDataProperty: "resourceScope", rootData, dynamicAnchors })) {
        vErrors = vErrors === null ? validate30.errors : vErrors.concat(validate30.errors);
        errors = vErrors.length;
      }
    }
    if (data.inputFields !== void 0) {
      let data7 = data.inputFields;
      if (Array.isArray(data7)) {
        if (data7.length > 24) {
          const err25 = { instancePath: instancePath + "/inputFields", schemaPath: "#/properties/inputFields/maxItems", keyword: "maxItems", params: { limit: 24 }, message: "must NOT have more than 24 items" };
          if (vErrors === null) {
            vErrors = [err25];
          } else {
            vErrors.push(err25);
          }
          errors++;
        }
        const len0 = data7.length;
        for (let i0 = 0; i0 < len0; i0++) {
          if (!validate23(data7[i0], { instancePath: instancePath + "/inputFields/" + i0, parentData: data7, parentDataProperty: i0, rootData, dynamicAnchors })) {
            vErrors = vErrors === null ? validate23.errors : vErrors.concat(validate23.errors);
            errors = vErrors.length;
          }
        }
      } else {
        const err26 = { instancePath: instancePath + "/inputFields", schemaPath: "#/properties/inputFields/type", keyword: "type", params: { type: "array" }, message: "must be array" };
        if (vErrors === null) {
          vErrors = [err26];
        } else {
          vErrors.push(err26);
        }
        errors++;
      }
    }
    if (data.outputFields !== void 0) {
      let data9 = data.outputFields;
      if (Array.isArray(data9)) {
        if (data9.length > 24) {
          const err27 = { instancePath: instancePath + "/outputFields", schemaPath: "#/properties/outputFields/maxItems", keyword: "maxItems", params: { limit: 24 }, message: "must NOT have more than 24 items" };
          if (vErrors === null) {
            vErrors = [err27];
          } else {
            vErrors.push(err27);
          }
          errors++;
        }
        if (data9.length < 1) {
          const err28 = { instancePath: instancePath + "/outputFields", schemaPath: "#/properties/outputFields/minItems", keyword: "minItems", params: { limit: 1 }, message: "must NOT have fewer than 1 items" };
          if (vErrors === null) {
            vErrors = [err28];
          } else {
            vErrors.push(err28);
          }
          errors++;
        }
        const len1 = data9.length;
        for (let i1 = 0; i1 < len1; i1++) {
          if (!validate23(data9[i1], { instancePath: instancePath + "/outputFields/" + i1, parentData: data9, parentDataProperty: i1, rootData, dynamicAnchors })) {
            vErrors = vErrors === null ? validate23.errors : vErrors.concat(validate23.errors);
            errors = vErrors.length;
          }
        }
      } else {
        const err29 = { instancePath: instancePath + "/outputFields", schemaPath: "#/properties/outputFields/type", keyword: "type", params: { type: "array" }, message: "must be array" };
        if (vErrors === null) {
          vErrors = [err29];
        } else {
          vErrors.push(err29);
        }
        errors++;
      }
    }
    if (data.execution !== void 0) {
      let data11 = data.execution;
      if (data11 && typeof data11 == "object" && !Array.isArray(data11)) {
        if (data11.idempotency === void 0) {
          const err30 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/required", keyword: "required", params: { missingProperty: "idempotency" }, message: "must have required property 'idempotency'" };
          if (vErrors === null) {
            vErrors = [err30];
          } else {
            vErrors.push(err30);
          }
          errors++;
        }
        if (data11.timeoutSeconds === void 0) {
          const err31 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/required", keyword: "required", params: { missingProperty: "timeoutSeconds" }, message: "must have required property 'timeoutSeconds'" };
          if (vErrors === null) {
            vErrors = [err31];
          } else {
            vErrors.push(err31);
          }
          errors++;
        }
        if (data11.maxAttempts === void 0) {
          const err32 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/required", keyword: "required", params: { missingProperty: "maxAttempts" }, message: "must have required property 'maxAttempts'" };
          if (vErrors === null) {
            vErrors = [err32];
          } else {
            vErrors.push(err32);
          }
          errors++;
        }
        if (data11.retryBackoffMilliseconds === void 0) {
          const err33 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/required", keyword: "required", params: { missingProperty: "retryBackoffMilliseconds" }, message: "must have required property 'retryBackoffMilliseconds'" };
          if (vErrors === null) {
            vErrors = [err33];
          } else {
            vErrors.push(err33);
          }
          errors++;
        }
        for (const key1 in data11) {
          if (!(key1 === "idempotency" || key1 === "timeoutSeconds" || key1 === "maxAttempts" || key1 === "retryBackoffMilliseconds")) {
            const err34 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key1 }, message: "must NOT have additional properties" };
            if (vErrors === null) {
              vErrors = [err34];
            } else {
              vErrors.push(err34);
            }
            errors++;
          }
        }
        if (data11.idempotency !== void 0) {
          let data12 = data11.idempotency;
          if (!(data12 === "READ_ONLY" || data12 === "EFFECT_KEY" || data12 === "PROVIDER_NATIVE")) {
            const err35 = { instancePath: instancePath + "/execution/idempotency", schemaPath: "#/$defs/execution/properties/idempotency/enum", keyword: "enum", params: { allowedValues: schema48.properties.idempotency.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err35];
            } else {
              vErrors.push(err35);
            }
            errors++;
          }
        }
        if (data11.timeoutSeconds !== void 0) {
          let data13 = data11.timeoutSeconds;
          if (!(typeof data13 == "number" && (!(data13 % 1) && !isNaN(data13)) && isFinite(data13))) {
            const err36 = { instancePath: instancePath + "/execution/timeoutSeconds", schemaPath: "#/$defs/execution/properties/timeoutSeconds/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
            if (vErrors === null) {
              vErrors = [err36];
            } else {
              vErrors.push(err36);
            }
            errors++;
          }
          if (typeof data13 == "number" && isFinite(data13)) {
            if (data13 > 120 || isNaN(data13)) {
              const err37 = { instancePath: instancePath + "/execution/timeoutSeconds", schemaPath: "#/$defs/execution/properties/timeoutSeconds/maximum", keyword: "maximum", params: { comparison: "<=", limit: 120 }, message: "must be <= 120" };
              if (vErrors === null) {
                vErrors = [err37];
              } else {
                vErrors.push(err37);
              }
              errors++;
            }
            if (data13 < 1 || isNaN(data13)) {
              const err38 = { instancePath: instancePath + "/execution/timeoutSeconds", schemaPath: "#/$defs/execution/properties/timeoutSeconds/minimum", keyword: "minimum", params: { comparison: ">=", limit: 1 }, message: "must be >= 1" };
              if (vErrors === null) {
                vErrors = [err38];
              } else {
                vErrors.push(err38);
              }
              errors++;
            }
          }
        }
        if (data11.maxAttempts !== void 0) {
          let data14 = data11.maxAttempts;
          if (!(typeof data14 == "number" && (!(data14 % 1) && !isNaN(data14)) && isFinite(data14))) {
            const err39 = { instancePath: instancePath + "/execution/maxAttempts", schemaPath: "#/$defs/execution/properties/maxAttempts/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
            if (vErrors === null) {
              vErrors = [err39];
            } else {
              vErrors.push(err39);
            }
            errors++;
          }
          if (typeof data14 == "number" && isFinite(data14)) {
            if (data14 > 4 || isNaN(data14)) {
              const err40 = { instancePath: instancePath + "/execution/maxAttempts", schemaPath: "#/$defs/execution/properties/maxAttempts/maximum", keyword: "maximum", params: { comparison: "<=", limit: 4 }, message: "must be <= 4" };
              if (vErrors === null) {
                vErrors = [err40];
              } else {
                vErrors.push(err40);
              }
              errors++;
            }
            if (data14 < 1 || isNaN(data14)) {
              const err41 = { instancePath: instancePath + "/execution/maxAttempts", schemaPath: "#/$defs/execution/properties/maxAttempts/minimum", keyword: "minimum", params: { comparison: ">=", limit: 1 }, message: "must be >= 1" };
              if (vErrors === null) {
                vErrors = [err41];
              } else {
                vErrors.push(err41);
              }
              errors++;
            }
          }
        }
        if (data11.retryBackoffMilliseconds !== void 0) {
          let data15 = data11.retryBackoffMilliseconds;
          if (!(typeof data15 == "number" && (!(data15 % 1) && !isNaN(data15)) && isFinite(data15))) {
            const err42 = { instancePath: instancePath + "/execution/retryBackoffMilliseconds", schemaPath: "#/$defs/execution/properties/retryBackoffMilliseconds/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
            if (vErrors === null) {
              vErrors = [err42];
            } else {
              vErrors.push(err42);
            }
            errors++;
          }
          if (typeof data15 == "number" && isFinite(data15)) {
            if (data15 > 5e3 || isNaN(data15)) {
              const err43 = { instancePath: instancePath + "/execution/retryBackoffMilliseconds", schemaPath: "#/$defs/execution/properties/retryBackoffMilliseconds/maximum", keyword: "maximum", params: { comparison: "<=", limit: 5e3 }, message: "must be <= 5000" };
              if (vErrors === null) {
                vErrors = [err43];
              } else {
                vErrors.push(err43);
              }
              errors++;
            }
            if (data15 < 50 || isNaN(data15)) {
              const err44 = { instancePath: instancePath + "/execution/retryBackoffMilliseconds", schemaPath: "#/$defs/execution/properties/retryBackoffMilliseconds/minimum", keyword: "minimum", params: { comparison: ">=", limit: 50 }, message: "must be >= 50" };
              if (vErrors === null) {
                vErrors = [err44];
              } else {
                vErrors.push(err44);
              }
              errors++;
            }
          }
        }
      } else {
        const err45 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/type", keyword: "type", params: { type: "object" }, message: "must be object" };
        if (vErrors === null) {
          vErrors = [err45];
        } else {
          vErrors.push(err45);
        }
        errors++;
      }
    }
  } else {
    const err46 = { instancePath, schemaPath: "#/type", keyword: "type", params: { type: "object" }, message: "must be object" };
    if (vErrors === null) {
      vErrors = [err46];
    } else {
      vErrors.push(err46);
    }
    errors++;
  }
  validate29.errors = vErrors;
  return errors === 0;
}
validate29.evaluated = { "props": true, "dynamicProps": false, "dynamicItems": false };
function validate20(data, { instancePath = "", parentData, parentDataProperty, rootData = data, dynamicAnchors = {} } = {}) {
  ;
  let vErrors = null;
  let errors = 0;
  const evaluated0 = validate20.evaluated;
  if (evaluated0.dynamicProps) {
    evaluated0.props = void 0;
  }
  if (evaluated0.dynamicItems) {
    evaluated0.items = void 0;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.apiVersion === void 0) {
      const err0 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "apiVersion" }, message: "must have required property 'apiVersion'" };
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
    if (data.kind === void 0) {
      const err1 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "kind" }, message: "must have required property 'kind'" };
      if (vErrors === null) {
        vErrors = [err1];
      } else {
        vErrors.push(err1);
      }
      errors++;
    }
    if (data.metadata === void 0) {
      const err2 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "metadata" }, message: "must have required property 'metadata'" };
      if (vErrors === null) {
        vErrors = [err2];
      } else {
        vErrors.push(err2);
      }
      errors++;
    }
    if (data.spec === void 0) {
      const err3 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "spec" }, message: "must have required property 'spec'" };
      if (vErrors === null) {
        vErrors = [err3];
      } else {
        vErrors.push(err3);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!(key0 === "apiVersion" || key0 === "kind" || key0 === "metadata" || key0 === "spec")) {
        const err4 = { instancePath, schemaPath: "#/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key0 }, message: "must NOT have additional properties" };
        if (vErrors === null) {
          vErrors = [err4];
        } else {
          vErrors.push(err4);
        }
        errors++;
      }
    }
    if (data.apiVersion !== void 0) {
      if ("integrations.kodex.io/v1" !== data.apiVersion) {
        const err5 = { instancePath: instancePath + "/apiVersion", schemaPath: "#/properties/apiVersion/const", keyword: "const", params: { allowedValue: "integrations.kodex.io/v1" }, message: "must be equal to constant" };
        if (vErrors === null) {
          vErrors = [err5];
        } else {
          vErrors.push(err5);
        }
        errors++;
      }
    }
    if (data.kind !== void 0) {
      if ("IntegrationPackage" !== data.kind) {
        const err6 = { instancePath: instancePath + "/kind", schemaPath: "#/properties/kind/const", keyword: "const", params: { allowedValue: "IntegrationPackage" }, message: "must be equal to constant" };
        if (vErrors === null) {
          vErrors = [err6];
        } else {
          vErrors.push(err6);
        }
        errors++;
      }
    }
    if (data.metadata !== void 0) {
      let data2 = data.metadata;
      if (data2 && typeof data2 == "object" && !Array.isArray(data2)) {
        if (data2.key === void 0) {
          const err7 = { instancePath: instancePath + "/metadata", schemaPath: "#/properties/metadata/required", keyword: "required", params: { missingProperty: "key" }, message: "must have required property 'key'" };
          if (vErrors === null) {
            vErrors = [err7];
          } else {
            vErrors.push(err7);
          }
          errors++;
        }
        if (data2.version === void 0) {
          const err8 = { instancePath: instancePath + "/metadata", schemaPath: "#/properties/metadata/required", keyword: "required", params: { missingProperty: "version" }, message: "must have required property 'version'" };
          if (vErrors === null) {
            vErrors = [err8];
          } else {
            vErrors.push(err8);
          }
          errors++;
        }
        if (data2.origin === void 0) {
          const err9 = { instancePath: instancePath + "/metadata", schemaPath: "#/properties/metadata/required", keyword: "required", params: { missingProperty: "origin" }, message: "must have required property 'origin'" };
          if (vErrors === null) {
            vErrors = [err9];
          } else {
            vErrors.push(err9);
          }
          errors++;
        }
        for (const key1 in data2) {
          if (!(key1 === "key" || key1 === "version" || key1 === "origin")) {
            const err10 = { instancePath: instancePath + "/metadata", schemaPath: "#/properties/metadata/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key1 }, message: "must NOT have additional properties" };
            if (vErrors === null) {
              vErrors = [err10];
            } else {
              vErrors.push(err10);
            }
            errors++;
          }
        }
        if (data2.key !== void 0) {
          let data3 = data2.key;
          if (typeof data3 === "string") {
            if (func1(data3) > 120) {
              const err11 = { instancePath: instancePath + "/metadata/key", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
              if (vErrors === null) {
                vErrors = [err11];
              } else {
                vErrors.push(err11);
              }
              errors++;
            }
            if (!pattern4.test(data3)) {
              const err12 = { instancePath: instancePath + "/metadata/key", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$"' };
              if (vErrors === null) {
                vErrors = [err12];
              } else {
                vErrors.push(err12);
              }
              errors++;
            }
          } else {
            const err13 = { instancePath: instancePath + "/metadata/key", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err13];
            } else {
              vErrors.push(err13);
            }
            errors++;
          }
        }
        if (data2.version !== void 0) {
          let data4 = data2.version;
          if (typeof data4 === "string") {
            if (func1(data4) > 32) {
              const err14 = { instancePath: instancePath + "/metadata/version", schemaPath: "#/properties/metadata/properties/version/maxLength", keyword: "maxLength", params: { limit: 32 }, message: "must NOT have more than 32 characters" };
              if (vErrors === null) {
                vErrors = [err14];
              } else {
                vErrors.push(err14);
              }
              errors++;
            }
            if (!pattern5.test(data4)) {
              const err15 = { instancePath: instancePath + "/metadata/version", schemaPath: "#/properties/metadata/properties/version/pattern", keyword: "pattern", params: { pattern: "^[1-9][0-9]*\\.[0-9]+\\.[0-9]+$" }, message: 'must match pattern "^[1-9][0-9]*\\.[0-9]+\\.[0-9]+$"' };
              if (vErrors === null) {
                vErrors = [err15];
              } else {
                vErrors.push(err15);
              }
              errors++;
            }
          } else {
            const err16 = { instancePath: instancePath + "/metadata/version", schemaPath: "#/properties/metadata/properties/version/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err16];
            } else {
              vErrors.push(err16);
            }
            errors++;
          }
        }
        if (data2.origin !== void 0) {
          if ("SHIPPED" !== data2.origin) {
            const err17 = { instancePath: instancePath + "/metadata/origin", schemaPath: "#/properties/metadata/properties/origin/const", keyword: "const", params: { allowedValue: "SHIPPED" }, message: "must be equal to constant" };
            if (vErrors === null) {
              vErrors = [err17];
            } else {
              vErrors.push(err17);
            }
            errors++;
          }
        }
      } else {
        const err18 = { instancePath: instancePath + "/metadata", schemaPath: "#/properties/metadata/type", keyword: "type", params: { type: "object" }, message: "must be object" };
        if (vErrors === null) {
          vErrors = [err18];
        } else {
          vErrors.push(err18);
        }
        errors++;
      }
    }
    if (data.spec !== void 0) {
      let data6 = data.spec;
      if (data6 && typeof data6 == "object" && !Array.isArray(data6)) {
        if (data6.name === void 0) {
          const err19 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "name" }, message: "must have required property 'name'" };
          if (vErrors === null) {
            vErrors = [err19];
          } else {
            vErrors.push(err19);
          }
          errors++;
        }
        if (data6.description === void 0) {
          const err20 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "description" }, message: "must have required property 'description'" };
          if (vErrors === null) {
            vErrors = [err20];
          } else {
            vErrors.push(err20);
          }
          errors++;
        }
        if (data6.category === void 0) {
          const err21 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "category" }, message: "must have required property 'category'" };
          if (vErrors === null) {
            vErrors = [err21];
          } else {
            vErrors.push(err21);
          }
          errors++;
        }
        if (data6.adapter === void 0) {
          const err22 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "adapter" }, message: "must have required property 'adapter'" };
          if (vErrors === null) {
            vErrors = [err22];
          } else {
            vErrors.push(err22);
          }
          errors++;
        }
        if (data6.adapterOwner === void 0) {
          const err23 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "adapterOwner" }, message: "must have required property 'adapterOwner'" };
          if (vErrors === null) {
            vErrors = [err23];
          } else {
            vErrors.push(err23);
          }
          errors++;
        }
        if (data6.executionRoute === void 0) {
          const err24 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "executionRoute" }, message: "must have required property 'executionRoute'" };
          if (vErrors === null) {
            vErrors = [err24];
          } else {
            vErrors.push(err24);
          }
          errors++;
        }
        if (data6.readiness === void 0) {
          const err25 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "readiness" }, message: "must have required property 'readiness'" };
          if (vErrors === null) {
            vErrors = [err25];
          } else {
            vErrors.push(err25);
          }
          errors++;
        }
        if (data6.configurationFields === void 0) {
          const err26 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "configurationFields" }, message: "must have required property 'configurationFields'" };
          if (vErrors === null) {
            vErrors = [err26];
          } else {
            vErrors.push(err26);
          }
          errors++;
        }
        if (data6.networkDestinations === void 0) {
          const err27 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "networkDestinations" }, message: "must have required property 'networkDestinations'" };
          if (vErrors === null) {
            vErrors = [err27];
          } else {
            vErrors.push(err27);
          }
          errors++;
        }
        if (data6.healthCheck === void 0) {
          const err28 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "healthCheck" }, message: "must have required property 'healthCheck'" };
          if (vErrors === null) {
            vErrors = [err28];
          } else {
            vErrors.push(err28);
          }
          errors++;
        }
        if (data6.capabilities === void 0) {
          const err29 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "capabilities" }, message: "must have required property 'capabilities'" };
          if (vErrors === null) {
            vErrors = [err29];
          } else {
            vErrors.push(err29);
          }
          errors++;
        }
        for (const key2 in data6) {
          if (!func3.call(schema31.properties.spec.properties, key2)) {
            const err30 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key2 }, message: "must NOT have additional properties" };
            if (vErrors === null) {
              vErrors = [err30];
            } else {
              vErrors.push(err30);
            }
            errors++;
          }
        }
        if (data6.name !== void 0) {
          let data7 = data6.name;
          if (typeof data7 === "string") {
            if (func1(data7) > 120) {
              const err31 = { instancePath: instancePath + "/spec/name", schemaPath: "#/properties/spec/properties/name/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
              if (vErrors === null) {
                vErrors = [err31];
              } else {
                vErrors.push(err31);
              }
              errors++;
            }
            if (func1(data7) < 1) {
              const err32 = { instancePath: instancePath + "/spec/name", schemaPath: "#/properties/spec/properties/name/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
              if (vErrors === null) {
                vErrors = [err32];
              } else {
                vErrors.push(err32);
              }
              errors++;
            }
          } else {
            const err33 = { instancePath: instancePath + "/spec/name", schemaPath: "#/properties/spec/properties/name/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err33];
            } else {
              vErrors.push(err33);
            }
            errors++;
          }
        }
        if (data6.description !== void 0) {
          let data8 = data6.description;
          if (typeof data8 === "string") {
            if (func1(data8) > 500) {
              const err34 = { instancePath: instancePath + "/spec/description", schemaPath: "#/properties/spec/properties/description/maxLength", keyword: "maxLength", params: { limit: 500 }, message: "must NOT have more than 500 characters" };
              if (vErrors === null) {
                vErrors = [err34];
              } else {
                vErrors.push(err34);
              }
              errors++;
            }
            if (func1(data8) < 1) {
              const err35 = { instancePath: instancePath + "/spec/description", schemaPath: "#/properties/spec/properties/description/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
              if (vErrors === null) {
                vErrors = [err35];
              } else {
                vErrors.push(err35);
              }
              errors++;
            }
          } else {
            const err36 = { instancePath: instancePath + "/spec/description", schemaPath: "#/properties/spec/properties/description/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err36];
            } else {
              vErrors.push(err36);
            }
            errors++;
          }
        }
        if (data6.category !== void 0) {
          let data9 = data6.category;
          if (typeof data9 === "string") {
            if (func1(data9) > 120) {
              const err37 = { instancePath: instancePath + "/spec/category", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
              if (vErrors === null) {
                vErrors = [err37];
              } else {
                vErrors.push(err37);
              }
              errors++;
            }
            if (!pattern4.test(data9)) {
              const err38 = { instancePath: instancePath + "/spec/category", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$"' };
              if (vErrors === null) {
                vErrors = [err38];
              } else {
                vErrors.push(err38);
              }
              errors++;
            }
          } else {
            const err39 = { instancePath: instancePath + "/spec/category", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err39];
            } else {
              vErrors.push(err39);
            }
            errors++;
          }
        }
        if (data6.adapter !== void 0) {
          let data10 = data6.adapter;
          if (!(data10 === "SYNTHETIC_HTTP" || data10 === "GITHUB" || data10 === "GITLAB" || data10 === "JIRA" || data10 === "CONFLUENCE" || data10 === "EMAIL_HTTPS" || data10 === "MATTERMOST_INTERACTION")) {
            const err40 = { instancePath: instancePath + "/spec/adapter", schemaPath: "#/properties/spec/properties/adapter/enum", keyword: "enum", params: { allowedValues: schema31.properties.spec.properties.adapter.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err40];
            } else {
              vErrors.push(err40);
            }
            errors++;
          }
        }
        if (data6.adapterOwner !== void 0) {
          let data11 = data6.adapterOwner;
          if (!(data11 === "integration-gateway" || data11 === "interaction-gateway")) {
            const err41 = { instancePath: instancePath + "/spec/adapterOwner", schemaPath: "#/properties/spec/properties/adapterOwner/enum", keyword: "enum", params: { allowedValues: schema31.properties.spec.properties.adapterOwner.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err41];
            } else {
              vErrors.push(err41);
            }
            errors++;
          }
        }
        if (data6.executionRoute !== void 0) {
          let data12 = data6.executionRoute;
          if (!(data12 === "MANAGED_MCP" || data12 === "INTERACTION")) {
            const err42 = { instancePath: instancePath + "/spec/executionRoute", schemaPath: "#/properties/spec/properties/executionRoute/enum", keyword: "enum", params: { allowedValues: schema31.properties.spec.properties.executionRoute.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err42];
            } else {
              vErrors.push(err42);
            }
            errors++;
          }
        }
        if (data6.readiness !== void 0) {
          let data13 = data6.readiness;
          if (!(data13 === "READY" || data13 === "NOT_READY")) {
            const err43 = { instancePath: instancePath + "/spec/readiness", schemaPath: "#/properties/spec/properties/readiness/enum", keyword: "enum", params: { allowedValues: schema31.properties.spec.properties.readiness.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err43];
            } else {
              vErrors.push(err43);
            }
            errors++;
          }
        }
        if (data6.credential !== void 0) {
          if (!validate21(data6.credential, { instancePath: instancePath + "/spec/credential", parentData: data6, parentDataProperty: "credential", rootData, dynamicAnchors })) {
            vErrors = vErrors === null ? validate21.errors : vErrors.concat(validate21.errors);
            errors = vErrors.length;
          }
        }
        if (data6.configurationFields !== void 0) {
          let data15 = data6.configurationFields;
          if (Array.isArray(data15)) {
            if (data15.length > 24) {
              const err44 = { instancePath: instancePath + "/spec/configurationFields", schemaPath: "#/properties/spec/properties/configurationFields/maxItems", keyword: "maxItems", params: { limit: 24 }, message: "must NOT have more than 24 items" };
              if (vErrors === null) {
                vErrors = [err44];
              } else {
                vErrors.push(err44);
              }
              errors++;
            }
            const len0 = data15.length;
            for (let i0 = 0; i0 < len0; i0++) {
              let data16 = data15[i0];
              if (!validate23(data16, { instancePath: instancePath + "/spec/configurationFields/" + i0, parentData: data15, parentDataProperty: i0, rootData, dynamicAnchors })) {
                vErrors = vErrors === null ? validate23.errors : vErrors.concat(validate23.errors);
                errors = vErrors.length;
              }
              if (data16 && typeof data16 == "object" && !Array.isArray(data16)) {
                if (data16.allowEmpty !== void 0) {
                  if (false !== data16.allowEmpty) {
                    const err45 = { instancePath: instancePath + "/spec/configurationFields/" + i0 + "/allowEmpty", schemaPath: "#/properties/spec/properties/configurationFields/items/properties/allowEmpty/const", keyword: "const", params: { allowedValue: false }, message: "must be equal to constant" };
                    if (vErrors === null) {
                      vErrors = [err45];
                    } else {
                      vErrors.push(err45);
                    }
                    errors++;
                  }
                }
              } else {
                const err46 = { instancePath: instancePath + "/spec/configurationFields/" + i0, schemaPath: "#/properties/spec/properties/configurationFields/items/type", keyword: "type", params: { type: "object" }, message: "must be object" };
                if (vErrors === null) {
                  vErrors = [err46];
                } else {
                  vErrors.push(err46);
                }
                errors++;
              }
            }
          } else {
            const err47 = { instancePath: instancePath + "/spec/configurationFields", schemaPath: "#/properties/spec/properties/configurationFields/type", keyword: "type", params: { type: "array" }, message: "must be array" };
            if (vErrors === null) {
              vErrors = [err47];
            } else {
              vErrors.push(err47);
            }
            errors++;
          }
        }
        if (data6.networkDestinations !== void 0) {
          let data18 = data6.networkDestinations;
          if (Array.isArray(data18)) {
            if (data18.length > 16) {
              const err48 = { instancePath: instancePath + "/spec/networkDestinations", schemaPath: "#/properties/spec/properties/networkDestinations/maxItems", keyword: "maxItems", params: { limit: 16 }, message: "must NOT have more than 16 items" };
              if (vErrors === null) {
                vErrors = [err48];
              } else {
                vErrors.push(err48);
              }
              errors++;
            }
            if (data18.length < 1) {
              const err49 = { instancePath: instancePath + "/spec/networkDestinations", schemaPath: "#/properties/spec/properties/networkDestinations/minItems", keyword: "minItems", params: { limit: 1 }, message: "must NOT have fewer than 1 items" };
              if (vErrors === null) {
                vErrors = [err49];
              } else {
                vErrors.push(err49);
              }
              errors++;
            }
            const len1 = data18.length;
            for (let i1 = 0; i1 < len1; i1++) {
              if (!validate25(data18[i1], { instancePath: instancePath + "/spec/networkDestinations/" + i1, parentData: data18, parentDataProperty: i1, rootData, dynamicAnchors })) {
                vErrors = vErrors === null ? validate25.errors : vErrors.concat(validate25.errors);
                errors = vErrors.length;
              }
            }
          } else {
            const err50 = { instancePath: instancePath + "/spec/networkDestinations", schemaPath: "#/properties/spec/properties/networkDestinations/type", keyword: "type", params: { type: "array" }, message: "must be array" };
            if (vErrors === null) {
              vErrors = [err50];
            } else {
              vErrors.push(err50);
            }
            errors++;
          }
        }
        if (data6.healthCheck !== void 0) {
          if (!validate27(data6.healthCheck, { instancePath: instancePath + "/spec/healthCheck", parentData: data6, parentDataProperty: "healthCheck", rootData, dynamicAnchors })) {
            vErrors = vErrors === null ? validate27.errors : vErrors.concat(validate27.errors);
            errors = vErrors.length;
          }
        }
        if (data6.capabilities !== void 0) {
          let data21 = data6.capabilities;
          if (Array.isArray(data21)) {
            if (data21.length > 48) {
              const err51 = { instancePath: instancePath + "/spec/capabilities", schemaPath: "#/properties/spec/properties/capabilities/maxItems", keyword: "maxItems", params: { limit: 48 }, message: "must NOT have more than 48 items" };
              if (vErrors === null) {
                vErrors = [err51];
              } else {
                vErrors.push(err51);
              }
              errors++;
            }
            if (data21.length < 1) {
              const err52 = { instancePath: instancePath + "/spec/capabilities", schemaPath: "#/properties/spec/properties/capabilities/minItems", keyword: "minItems", params: { limit: 1 }, message: "must NOT have fewer than 1 items" };
              if (vErrors === null) {
                vErrors = [err52];
              } else {
                vErrors.push(err52);
              }
              errors++;
            }
            const len2 = data21.length;
            for (let i2 = 0; i2 < len2; i2++) {
              if (!validate29(data21[i2], { instancePath: instancePath + "/spec/capabilities/" + i2, parentData: data21, parentDataProperty: i2, rootData, dynamicAnchors })) {
                vErrors = vErrors === null ? validate29.errors : vErrors.concat(validate29.errors);
                errors = vErrors.length;
              }
            }
          } else {
            const err53 = { instancePath: instancePath + "/spec/capabilities", schemaPath: "#/properties/spec/properties/capabilities/type", keyword: "type", params: { type: "array" }, message: "must be array" };
            if (vErrors === null) {
              vErrors = [err53];
            } else {
              vErrors.push(err53);
            }
            errors++;
          }
        }
      } else {
        const err54 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/type", keyword: "type", params: { type: "object" }, message: "must be object" };
        if (vErrors === null) {
          vErrors = [err54];
        } else {
          vErrors.push(err54);
        }
        errors++;
      }
    }
  } else {
    const err55 = { instancePath, schemaPath: "#/type", keyword: "type", params: { type: "object" }, message: "must be object" };
    if (vErrors === null) {
      vErrors = [err55];
    } else {
      vErrors.push(err55);
    }
    errors++;
  }
  validate20.errors = vErrors;
  return errors === 0;
}
validate20.evaluated = { "props": true, "dynamicProps": false, "dynamicItems": false };
export {
  integration_package_default as default,
  validate
};

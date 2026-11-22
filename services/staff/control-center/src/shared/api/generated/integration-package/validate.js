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

// node_modules/ajv-formats/dist/formats.js
var require_formats = __commonJS({
  "node_modules/ajv-formats/dist/formats.js"(exports) {
    "use strict";
    Object.defineProperty(exports, "__esModule", { value: true });
    exports.formatNames = exports.fastFormats = exports.fullFormats = void 0;
    function fmtDef(validate2, compare) {
      return { validate: validate2, compare };
    }
    exports.fullFormats = {
      // date: http://tools.ietf.org/html/rfc3339#section-5.6
      date: fmtDef(date, compareDate),
      // date-time: http://tools.ietf.org/html/rfc3339#section-5.6
      time: fmtDef(time, compareTime),
      "date-time": fmtDef(date_time, compareDateTime),
      // duration: https://tools.ietf.org/html/rfc3339#appendix-A
      duration: /^P(?!$)((\d+Y)?(\d+M)?(\d+D)?(T(?=\d)(\d+H)?(\d+M)?(\d+S)?)?|(\d+W)?)$/,
      uri,
      "uri-reference": /^(?:[a-z][a-z0-9+\-.]*:)?(?:\/?\/(?:(?:[a-z0-9\-._~!$&'()*+,;=:]|%[0-9a-f]{2})*@)?(?:\[(?:(?:(?:(?:[0-9a-f]{1,4}:){6}|::(?:[0-9a-f]{1,4}:){5}|(?:[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){4}|(?:(?:[0-9a-f]{1,4}:){0,1}[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){3}|(?:(?:[0-9a-f]{1,4}:){0,2}[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){2}|(?:(?:[0-9a-f]{1,4}:){0,3}[0-9a-f]{1,4})?::[0-9a-f]{1,4}:|(?:(?:[0-9a-f]{1,4}:){0,4}[0-9a-f]{1,4})?::)(?:[0-9a-f]{1,4}:[0-9a-f]{1,4}|(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?))|(?:(?:[0-9a-f]{1,4}:){0,5}[0-9a-f]{1,4})?::[0-9a-f]{1,4}|(?:(?:[0-9a-f]{1,4}:){0,6}[0-9a-f]{1,4})?::)|[Vv][0-9a-f]+\.[a-z0-9\-._~!$&'()*+,;=:]+)\]|(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)|(?:[a-z0-9\-._~!$&'"()*+,;=]|%[0-9a-f]{2})*)(?::\d*)?(?:\/(?:[a-z0-9\-._~!$&'"()*+,;=:@]|%[0-9a-f]{2})*)*|\/(?:(?:[a-z0-9\-._~!$&'"()*+,;=:@]|%[0-9a-f]{2})+(?:\/(?:[a-z0-9\-._~!$&'"()*+,;=:@]|%[0-9a-f]{2})*)*)?|(?:[a-z0-9\-._~!$&'"()*+,;=:@]|%[0-9a-f]{2})+(?:\/(?:[a-z0-9\-._~!$&'"()*+,;=:@]|%[0-9a-f]{2})*)*)?(?:\?(?:[a-z0-9\-._~!$&'"()*+,;=:@/?]|%[0-9a-f]{2})*)?(?:#(?:[a-z0-9\-._~!$&'"()*+,;=:@/?]|%[0-9a-f]{2})*)?$/i,
      // uri-template: https://tools.ietf.org/html/rfc6570
      "uri-template": /^(?:(?:[^\x00-\x20"'<>%\\^`{|}]|%[0-9a-f]{2})|\{[+#./;?&=,!@|]?(?:[a-z0-9_]|%[0-9a-f]{2})+(?::[1-9][0-9]{0,3}|\*)?(?:,(?:[a-z0-9_]|%[0-9a-f]{2})+(?::[1-9][0-9]{0,3}|\*)?)*\})*$/i,
      // For the source: https://gist.github.com/dperini/729294
      // For test cases: https://mathiasbynens.be/demo/url-regex
      url: /^(?:https?|ftp):\/\/(?:\S+(?::\S*)?@)?(?:(?!(?:10|127)(?:\.\d{1,3}){3})(?!(?:169\.254|192\.168)(?:\.\d{1,3}){2})(?!172\.(?:1[6-9]|2\d|3[0-1])(?:\.\d{1,3}){2})(?:[1-9]\d?|1\d\d|2[01]\d|22[0-3])(?:\.(?:1?\d{1,2}|2[0-4]\d|25[0-5])){2}(?:\.(?:[1-9]\d?|1\d\d|2[0-4]\d|25[0-4]))|(?:(?:[a-z0-9\u{00a1}-\u{ffff}]+-)*[a-z0-9\u{00a1}-\u{ffff}]+)(?:\.(?:[a-z0-9\u{00a1}-\u{ffff}]+-)*[a-z0-9\u{00a1}-\u{ffff}]+)*(?:\.(?:[a-z\u{00a1}-\u{ffff}]{2,})))(?::\d{2,5})?(?:\/[^\s]*)?$/iu,
      email: /^[a-z0-9!#$%&'*+/=?^_`{|}~-]+(?:\.[a-z0-9!#$%&'*+/=?^_`{|}~-]+)*@(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/i,
      hostname: /^(?=.{1,253}\.?$)[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[-0-9a-z]{0,61}[0-9a-z])?)*\.?$/i,
      // optimized https://www.safaribooksonline.com/library/view/regular-expressions-cookbook/9780596802837/ch07s16.html
      ipv4: /^(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)$/,
      ipv6: /^((([0-9a-f]{1,4}:){7}([0-9a-f]{1,4}|:))|(([0-9a-f]{1,4}:){6}(:[0-9a-f]{1,4}|((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})|:))|(([0-9a-f]{1,4}:){5}(((:[0-9a-f]{1,4}){1,2})|:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})|:))|(([0-9a-f]{1,4}:){4}(((:[0-9a-f]{1,4}){1,3})|((:[0-9a-f]{1,4})?:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9a-f]{1,4}:){3}(((:[0-9a-f]{1,4}){1,4})|((:[0-9a-f]{1,4}){0,2}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9a-f]{1,4}:){2}(((:[0-9a-f]{1,4}){1,5})|((:[0-9a-f]{1,4}){0,3}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9a-f]{1,4}:){1}(((:[0-9a-f]{1,4}){1,6})|((:[0-9a-f]{1,4}){0,4}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(:(((:[0-9a-f]{1,4}){1,7})|((:[0-9a-f]{1,4}){0,5}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:)))$/i,
      regex,
      // uuid: http://tools.ietf.org/html/rfc4122
      uuid: /^(?:urn:uuid:)?[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i,
      // JSON-pointer: https://tools.ietf.org/html/rfc6901
      // uri fragment: https://tools.ietf.org/html/rfc3986#appendix-A
      "json-pointer": /^(?:\/(?:[^~/]|~0|~1)*)*$/,
      "json-pointer-uri-fragment": /^#(?:\/(?:[a-z0-9_\-.!$&'()*+,;:=@]|%[0-9a-f]{2}|~0|~1)*)*$/i,
      // relative JSON-pointer: http://tools.ietf.org/html/draft-luff-relative-json-pointer-00
      "relative-json-pointer": /^(?:0|[1-9][0-9]*)(?:#|(?:\/(?:[^~/]|~0|~1)*)*)$/,
      // the following formats are used by the openapi specification: https://spec.openapis.org/oas/v3.0.0#data-types
      // byte: https://github.com/miguelmota/is-base64
      byte,
      // signed 32 bit integer
      int32: { type: "number", validate: validateInt32 },
      // signed 64 bit integer
      int64: { type: "number", validate: validateInt64 },
      // C-type float
      float: { type: "number", validate: validateNumber },
      // C-type double
      double: { type: "number", validate: validateNumber },
      // hint to the UI to hide input strings
      password: true,
      // unchecked string payload
      binary: true
    };
    exports.fastFormats = {
      ...exports.fullFormats,
      date: fmtDef(/^\d\d\d\d-[0-1]\d-[0-3]\d$/, compareDate),
      time: fmtDef(/^(?:[0-2]\d:[0-5]\d:[0-5]\d|23:59:60)(?:\.\d+)?(?:z|[+-]\d\d(?::?\d\d)?)?$/i, compareTime),
      "date-time": fmtDef(/^\d\d\d\d-[0-1]\d-[0-3]\d[t\s](?:[0-2]\d:[0-5]\d:[0-5]\d|23:59:60)(?:\.\d+)?(?:z|[+-]\d\d(?::?\d\d)?)$/i, compareDateTime),
      // uri: https://github.com/mafintosh/is-my-json-valid/blob/master/formats.js
      uri: /^(?:[a-z][a-z0-9+\-.]*:)(?:\/?\/)?[^\s]*$/i,
      "uri-reference": /^(?:(?:[a-z][a-z0-9+\-.]*:)?\/?\/)?(?:[^\\\s#][^\s#]*)?(?:#[^\\\s]*)?$/i,
      // email (sources from jsen validator):
      // http://stackoverflow.com/questions/201323/using-a-regular-expression-to-validate-an-email-address#answer-8829363
      // http://www.w3.org/TR/html5/forms.html#valid-e-mail-address (search for 'wilful violation')
      email: /^[a-z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$/i
    };
    exports.formatNames = Object.keys(exports.fullFormats);
    function isLeapYear(year) {
      return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
    }
    var DATE = /^(\d\d\d\d)-(\d\d)-(\d\d)$/;
    var DAYS = [0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
    function date(str) {
      const matches = DATE.exec(str);
      if (!matches)
        return false;
      const year = +matches[1];
      const month = +matches[2];
      const day = +matches[3];
      return month >= 1 && month <= 12 && day >= 1 && day <= (month === 2 && isLeapYear(year) ? 29 : DAYS[month]);
    }
    function compareDate(d1, d2) {
      if (!(d1 && d2))
        return void 0;
      if (d1 > d2)
        return 1;
      if (d1 < d2)
        return -1;
      return 0;
    }
    var TIME = /^(\d\d):(\d\d):(\d\d)(\.\d+)?(z|[+-]\d\d(?::?\d\d)?)?$/i;
    function time(str, withTimeZone) {
      const matches = TIME.exec(str);
      if (!matches)
        return false;
      const hour = +matches[1];
      const minute = +matches[2];
      const second = +matches[3];
      const timeZone = matches[5];
      return (hour <= 23 && minute <= 59 && second <= 59 || hour === 23 && minute === 59 && second === 60) && (!withTimeZone || timeZone !== "");
    }
    function compareTime(t1, t2) {
      if (!(t1 && t2))
        return void 0;
      const a1 = TIME.exec(t1);
      const a2 = TIME.exec(t2);
      if (!(a1 && a2))
        return void 0;
      t1 = a1[1] + a1[2] + a1[3] + (a1[4] || "");
      t2 = a2[1] + a2[2] + a2[3] + (a2[4] || "");
      if (t1 > t2)
        return 1;
      if (t1 < t2)
        return -1;
      return 0;
    }
    var DATE_TIME_SEPARATOR = /t|\s/i;
    function date_time(str) {
      const dateTime = str.split(DATE_TIME_SEPARATOR);
      return dateTime.length === 2 && date(dateTime[0]) && time(dateTime[1], true);
    }
    function compareDateTime(dt1, dt2) {
      if (!(dt1 && dt2))
        return void 0;
      const [d1, t1] = dt1.split(DATE_TIME_SEPARATOR);
      const [d2, t2] = dt2.split(DATE_TIME_SEPARATOR);
      const res = compareDate(d1, d2);
      if (res === void 0)
        return void 0;
      return res || compareTime(t1, t2);
    }
    var NOT_URI_FRAGMENT = /\/|:/;
    var URI = /^(?:[a-z][a-z0-9+\-.]*:)(?:\/?\/(?:(?:[a-z0-9\-._~!$&'()*+,;=:]|%[0-9a-f]{2})*@)?(?:\[(?:(?:(?:(?:[0-9a-f]{1,4}:){6}|::(?:[0-9a-f]{1,4}:){5}|(?:[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){4}|(?:(?:[0-9a-f]{1,4}:){0,1}[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){3}|(?:(?:[0-9a-f]{1,4}:){0,2}[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){2}|(?:(?:[0-9a-f]{1,4}:){0,3}[0-9a-f]{1,4})?::[0-9a-f]{1,4}:|(?:(?:[0-9a-f]{1,4}:){0,4}[0-9a-f]{1,4})?::)(?:[0-9a-f]{1,4}:[0-9a-f]{1,4}|(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?))|(?:(?:[0-9a-f]{1,4}:){0,5}[0-9a-f]{1,4})?::[0-9a-f]{1,4}|(?:(?:[0-9a-f]{1,4}:){0,6}[0-9a-f]{1,4})?::)|[Vv][0-9a-f]+\.[a-z0-9\-._~!$&'()*+,;=:]+)\]|(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)|(?:[a-z0-9\-._~!$&'()*+,;=]|%[0-9a-f]{2})*)(?::\d*)?(?:\/(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})*)*|\/(?:(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})+(?:\/(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})*)*)?|(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})+(?:\/(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})*)*)(?:\?(?:[a-z0-9\-._~!$&'()*+,;=:@/?]|%[0-9a-f]{2})*)?(?:#(?:[a-z0-9\-._~!$&'()*+,;=:@/?]|%[0-9a-f]{2})*)?$/i;
    function uri(str) {
      return NOT_URI_FRAGMENT.test(str) && URI.test(str);
    }
    var BYTE = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/gm;
    function byte(str) {
      BYTE.lastIndex = 0;
      return BYTE.test(str);
    }
    var MIN_INT32 = -(2 ** 31);
    var MAX_INT32 = 2 ** 31 - 1;
    function validateInt32(value) {
      return Number.isInteger(value) && value <= MAX_INT32 && value >= MIN_INT32;
    }
    function validateInt64(value) {
      return Number.isInteger(value);
    }
    function validateNumber() {
      return true;
    }
    var Z_ANCHOR = /[^\\]\\Z/;
    function regex(str) {
      if (Z_ANCHOR.test(str))
        return false;
      try {
        new RegExp(str);
        return true;
      } catch (e) {
        return false;
      }
    }
  }
});

// integration-package.js
var validate = validate20;
var integration_package_default = validate20;
var schema31 = { "$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "https://kodex.dev/contracts/integrations/v1/integration-package.schema.json", "title": "Kodex Integration Package v1", "type": "object", "additionalProperties": false, "required": ["apiVersion", "kind", "metadata", "spec"], "properties": { "apiVersion": { "const": "integrations.kodex.io/v1" }, "kind": { "const": "IntegrationPackage" }, "metadata": { "type": "object", "additionalProperties": false, "required": ["key", "version", "origin"], "properties": { "key": { "$ref": "#/$defs/key" }, "version": { "type": "string", "pattern": "^[1-9][0-9]*\\.[0-9]+\\.[0-9]+$", "maxLength": 32 }, "origin": { "enum": ["SHIPPED", "UI", "GIT"] } } }, "spec": { "type": "object", "additionalProperties": false, "required": ["name", "description", "category", "adapter", "adapterOwner", "executionRoute", "readiness", "configurationFields", "networkDestinations", "healthCheck", "capabilities"], "properties": { "name": { "type": "string", "minLength": 1, "maxLength": 120 }, "description": { "type": "string", "minLength": 1, "maxLength": 500 }, "category": { "$ref": "#/$defs/key" }, "adapter": { "enum": ["SYNTHETIC_HTTP", "GITHUB", "GITLAB", "JIRA", "CONFLUENCE", "EMAIL_HTTPS", "HTTPS_JSON_READ", "OPENAPI_MCP", "MATTERMOST_INTERACTION", "TELEGRAM_INTERACTION", "CONTEXT7", "SENTRY", "OBSERVABILITY", "SSH_COMPOSE", "SSH_TARGET", "KUBERNETES"] }, "adapterOwner": { "enum": ["integration-gateway", "interaction-gateway"] }, "executionRoute": { "enum": ["MANAGED_MCP", "INTERACTION"] }, "readiness": { "enum": ["READY", "NOT_READY"] }, "credential": { "$ref": "#/$defs/credential" }, "configurationFields": { "type": "array", "maxItems": 24, "items": { "$ref": "#/$defs/field", "type": "object", "properties": { "allowEmpty": { "const": false } } } }, "networkDestinations": { "type": "array", "minItems": 1, "maxItems": 16, "items": { "$ref": "#/$defs/networkDestination" } }, "healthCheck": { "$ref": "#/$defs/healthCheck" }, "capabilities": { "type": "array", "minItems": 1, "maxItems": 64, "items": { "$ref": "#/$defs/capability" } } }, "allOf": [{ "if": { "required": ["credential"], "properties": { "credential": { "type": "object", "properties": { "kind": { "const": "BROKER_IDENTITY" } } } } }, "then": { "oneOf": [{ "properties": { "adapter": { "const": "KUBERNETES" }, "credential": { "type": "object", "properties": { "brokerPurpose": { "const": "KUBERNETES_TARGET" } } } } }, { "properties": { "adapter": { "const": "SSH_TARGET" }, "credential": { "type": "object", "properties": { "brokerPurpose": { "const": "SSH_TARGET" } } } } }] } }, { "if": { "required": ["adapter"], "properties": { "adapter": { "const": "KUBERNETES" } } }, "then": { "required": ["credential"], "properties": { "credential": { "type": "object", "required": ["kind", "brokerPurpose"], "properties": { "kind": { "const": "BROKER_IDENTITY" }, "brokerPurpose": { "const": "KUBERNETES_TARGET" } } } } } }, { "if": { "required": ["adapter"], "properties": { "adapter": { "const": "SSH_TARGET" } } }, "then": { "required": ["credential"], "properties": { "credential": { "type": "object", "required": ["kind", "brokerPurpose"], "properties": { "kind": { "const": "BROKER_IDENTITY" }, "brokerPurpose": { "const": "SSH_TARGET" } } } } } }] } }, "$defs": { "key": { "type": "string", "pattern": "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$", "maxLength": 120 }, "field": { "type": "object", "additionalProperties": false, "required": ["key", "type", "required"], "properties": { "key": { "$ref": "#/$defs/key" }, "label": { "type": "string", "maxLength": 120, "pattern": "^[^\\u0000\\r\\n]*$" }, "help": { "type": "string", "maxLength": 500, "pattern": "^[^\\u0000\\r\\n]*$" }, "type": { "enum": ["STRING", "INTEGER", "BOOLEAN"] }, "format": { "enum": ["PLAIN", "HTTPS_ORIGIN", "HTTPS_URL", "HTTPS_PATH", "EMAIL", "HOST", "IDENTIFIER", "PUBLIC_IP"] }, "required": { "type": "boolean" }, "maximumLength": { "type": "integer", "minimum": 1, "maximum": 349528 }, "allowEmpty": { "type": "boolean", "description": "\u041F\u0443\u0441\u0442\u0430\u044F \u0441\u0442\u0440\u043E\u043A\u0430 \u0434\u043E\u043F\u0443\u0441\u0442\u0438\u043C\u0430 \u0442\u043E\u043B\u044C\u043A\u043E \u0432 PLAIN input/output, \u043D\u0435 \u0432 \u043A\u043E\u043D\u0444\u0438\u0433\u0443\u0440\u0430\u0446\u0438\u0438 \u043F\u043E\u0434\u043A\u043B\u044E\u0447\u0435\u043D\u0438\u044F." }, "minimum": { "type": "integer", "minimum": 0 }, "maximum": { "type": "integer", "minimum": 0 }, "allowedValues": { "type": "array", "minItems": 1, "maxItems": 32, "uniqueItems": true, "items": { "type": "string", "minLength": 1, "maxLength": 120 } } }, "allOf": [{ "if": { "properties": { "allowEmpty": { "const": true } }, "required": ["allowEmpty"] }, "then": { "properties": { "type": { "const": "STRING" }, "format": { "const": "PLAIN" } }, "required": ["format"], "not": { "required": ["allowedValues"] } } }] }, "credential": { "type": "object", "additionalProperties": false, "required": ["kind"], "properties": { "secretKey": { "$ref": "#/$defs/key" }, "kind": { "enum": ["TOKEN", "PASSWORD", "BROKER_IDENTITY"] }, "brokerPurpose": { "enum": ["KUBERNETES_TARGET", "SSH_TARGET"] } }, "oneOf": [{ "required": ["secretKey"], "properties": { "kind": { "enum": ["TOKEN", "PASSWORD"] } }, "not": { "required": ["brokerPurpose"] } }, { "required": ["brokerPurpose"], "properties": { "kind": { "const": "BROKER_IDENTITY" } }, "not": { "required": ["secretKey"] } }] }, "networkDestination": { "type": "object", "additionalProperties": false, "required": ["key", "source", "port", "tls"], "properties": { "key": { "$ref": "#/$defs/key" }, "source": { "enum": ["STATIC", "CONFIGURATION"] }, "hostname": { "type": "string", "pattern": "^[a-z0-9](?:[a-z0-9.\\-]{0,251}[a-z0-9])?$", "maxLength": 253 }, "configurationField": { "$ref": "#/$defs/key" }, "port": { "type": "integer", "minimum": 1, "maximum": 65535 }, "tls": { "enum": ["REQUIRED", "NONE", "SSH_PINNED"] } }, "allOf": [{ "if": { "properties": { "source": { "const": "STATIC" } } }, "then": { "required": ["hostname"], "not": { "required": ["configurationField"] } } }, { "if": { "properties": { "source": { "const": "CONFIGURATION" } } }, "then": { "required": ["configurationField"], "not": { "required": ["hostname"] } } }] }, "healthCheck": { "type": "object", "additionalProperties": false, "required": ["operation", "timeoutSeconds", "maxAttempts"], "properties": { "operation": { "$ref": "#/$defs/key" }, "timeoutSeconds": { "type": "integer", "minimum": 1, "maximum": 60 }, "maxAttempts": { "type": "integer", "minimum": 1, "maximum": 3 } } }, "resourceScope": { "type": "object", "additionalProperties": false, "required": ["kind", "connectionFields"], "properties": { "kind": { "enum": ["SYNTHETIC_JOURNAL", "GITHUB_REPOSITORY", "GITHUB_SCOPE", "GITLAB_PROJECT", "GITLAB_SCOPE", "JIRA_PROJECT", "CONFLUENCE_SPACE", "EMAIL_SENDER", "MATTERMOST_CHANNEL", "HTTPS_RESOURCE", "SSH_TARGET", "KUBERNETES_TARGET"] }, "connectionFields": { "type": "array", "minItems": 1, "maxItems": 8, "uniqueItems": true, "items": { "$ref": "#/$defs/key" } } } }, "execution": { "type": "object", "additionalProperties": false, "required": ["idempotency", "timeoutSeconds", "maxAttempts", "retryBackoffMilliseconds"], "allOf": [{ "if": { "required": ["leaseProfile"] }, "then": { "properties": { "maxAttempts": { "const": 1 } } }, "else": { "properties": { "timeoutSeconds": { "type": "integer", "maximum": 120 } } } }, { "if": { "required": ["leaseProfile"], "properties": { "leaseProfile": { "const": "SSH_COMPOSE_V1" } } }, "then": { "properties": { "idempotency": { "const": "EFFECT_KEY" } } } }], "properties": { "leaseProfile": { "enum": ["SSH_COMPOSE_V1", "MANAGED_KUBERNETES_V1"] }, "idempotency": { "enum": ["READ_ONLY", "EFFECT_KEY", "PROVIDER_NATIVE", "ONE_ATTEMPT"] }, "timeoutSeconds": { "type": "integer", "minimum": 1, "maximum": 600 }, "maxAttempts": { "type": "integer", "minimum": 1, "maximum": 4 }, "retryBackoffMilliseconds": { "type": "integer", "minimum": 50, "maximum": 5e3 } } }, "capability": { "type": "object", "additionalProperties": false, "required": ["key", "name", "description", "operation", "risk", "approvalPolicy", "allowedApprovalPolicies", "resourceScope", "inputFields", "outputFields", "execution"], "properties": { "key": { "$ref": "#/$defs/key" }, "name": { "type": "string", "minLength": 1, "maxLength": 120 }, "description": { "type": "string", "minLength": 1, "maxLength": 500 }, "operation": { "$ref": "#/$defs/key" }, "risk": { "enum": ["READ", "WRITE", "SENSITIVE", "DESTRUCTIVE"] }, "approvalPolicy": { "enum": ["NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"] }, "allowedApprovalPolicies": { "type": "array", "minItems": 1, "maxItems": 3, "uniqueItems": true, "items": { "enum": ["NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"] } }, "resourceScope": { "$ref": "#/$defs/resourceScope" }, "approvalScope": { "type": "object", "additionalProperties": false, "required": ["name", "requiredInputPaths"], "properties": { "name": { "type": "string", "minLength": 1, "maxLength": 128, "pattern": "^[^\\u0000\\r\\n]*$" }, "allowedLifetimes": { "type": "array", "minItems": 1, "maxItems": 2, "uniqueItems": true, "items": { "enum": ["ROOT_RUN", "INITIATOR_RESOURCE"] } }, "requiredInputPaths": { "type": "array", "maxItems": 16, "uniqueItems": true, "items": { "type": "string", "minLength": 2, "maxLength": 256, "pattern": "^/" } }, "limits": { "type": "object", "additionalProperties": false, "required": ["durationSeconds", "maxEffects"], "properties": { "durationSeconds": { "type": "integer", "minimum": 1, "maximum": 604800 }, "maxEffects": { "type": "integer", "minimum": 1, "maximum": 1e4 } } } } }, "inputFields": { "type": "array", "maxItems": 24, "items": { "$ref": "#/$defs/field" } }, "outputFields": { "type": "array", "minItems": 1, "maxItems": 24, "items": { "$ref": "#/$defs/field" } }, "execution": { "$ref": "#/$defs/execution" }, "openapi": { "type": "object", "additionalProperties": false, "required": ["operationId", "method", "path", "authScheme", "inputSchema"], "properties": { "operationId": { "type": "string", "minLength": 1, "maxLength": 128 }, "sourceDigest": { "type": "string", "pattern": "^[a-f0-9]{64}$" }, "serverOrigin": { "type": "string", "format": "uri", "maxLength": 2048 }, "method": { "enum": ["GET", "POST", "PUT", "PATCH", "DELETE"] }, "path": { "type": "string", "minLength": 1, "maxLength": 512 }, "authScheme": { "enum": ["NONE", "BEARER", "API_KEY_HEADER"] }, "authHeader": { "type": "string", "maxLength": 64 }, "idempotencyHeader": { "type": "string", "maxLength": 64 }, "inputSchema": { "type": "object", "additionalProperties": true } } } } } } };
var func1 = require_ucs2length().default;
var func3 = Object.prototype.hasOwnProperty;
var pattern4 = new RegExp("^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$", "u");
var pattern5 = new RegExp("^[1-9][0-9]*\\.[0-9]+\\.[0-9]+$", "u");
var schema34 = { "type": "object", "additionalProperties": false, "required": ["kind"], "properties": { "secretKey": { "$ref": "#/$defs/key" }, "kind": { "enum": ["TOKEN", "PASSWORD", "BROKER_IDENTITY"] }, "brokerPurpose": { "enum": ["KUBERNETES_TARGET", "SSH_TARGET"] } }, "oneOf": [{ "required": ["secretKey"], "properties": { "kind": { "enum": ["TOKEN", "PASSWORD"] } }, "not": { "required": ["brokerPurpose"] } }, { "required": ["brokerPurpose"], "properties": { "kind": { "const": "BROKER_IDENTITY" } }, "not": { "required": ["secretKey"] } }] };
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
  const _errs1 = errors;
  let valid0 = false;
  let passing0 = null;
  const _errs2 = errors;
  const _errs3 = errors;
  const _errs4 = errors;
  if (data && typeof data == "object" && !Array.isArray(data)) {
    let missing0;
    if (data.brokerPurpose === void 0 && (missing0 = "brokerPurpose")) {
      const err0 = {};
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    }
  }
  var valid1 = _errs4 === errors;
  if (valid1) {
    const err1 = { instancePath, schemaPath: "#/oneOf/0/not", keyword: "not", params: {}, message: "must NOT be valid" };
    if (vErrors === null) {
      vErrors = [err1];
    } else {
      vErrors.push(err1);
    }
    errors++;
  } else {
    errors = _errs3;
    if (vErrors !== null) {
      if (_errs3) {
        vErrors.length = _errs3;
      } else {
        vErrors = null;
      }
    }
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.secretKey === void 0) {
      const err2 = { instancePath, schemaPath: "#/oneOf/0/required", keyword: "required", params: { missingProperty: "secretKey" }, message: "must have required property 'secretKey'" };
      if (vErrors === null) {
        vErrors = [err2];
      } else {
        vErrors.push(err2);
      }
      errors++;
    }
    if (data.kind !== void 0) {
      let data0 = data.kind;
      if (!(data0 === "TOKEN" || data0 === "PASSWORD")) {
        const err3 = { instancePath: instancePath + "/kind", schemaPath: "#/oneOf/0/properties/kind/enum", keyword: "enum", params: { allowedValues: schema34.oneOf[0].properties.kind.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err3];
        } else {
          vErrors.push(err3);
        }
        errors++;
      }
    }
  }
  var _valid0 = _errs2 === errors;
  if (_valid0) {
    valid0 = true;
    passing0 = 0;
    var props0 = {};
    props0.kind = true;
  }
  const _errs6 = errors;
  const _errs7 = errors;
  const _errs8 = errors;
  if (data && typeof data == "object" && !Array.isArray(data)) {
    let missing1;
    if (data.secretKey === void 0 && (missing1 = "secretKey")) {
      const err4 = {};
      if (vErrors === null) {
        vErrors = [err4];
      } else {
        vErrors.push(err4);
      }
      errors++;
    }
  }
  var valid3 = _errs8 === errors;
  if (valid3) {
    const err5 = { instancePath, schemaPath: "#/oneOf/1/not", keyword: "not", params: {}, message: "must NOT be valid" };
    if (vErrors === null) {
      vErrors = [err5];
    } else {
      vErrors.push(err5);
    }
    errors++;
  } else {
    errors = _errs7;
    if (vErrors !== null) {
      if (_errs7) {
        vErrors.length = _errs7;
      } else {
        vErrors = null;
      }
    }
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.brokerPurpose === void 0) {
      const err6 = { instancePath, schemaPath: "#/oneOf/1/required", keyword: "required", params: { missingProperty: "brokerPurpose" }, message: "must have required property 'brokerPurpose'" };
      if (vErrors === null) {
        vErrors = [err6];
      } else {
        vErrors.push(err6);
      }
      errors++;
    }
    if (data.kind !== void 0) {
      if ("BROKER_IDENTITY" !== data.kind) {
        const err7 = { instancePath: instancePath + "/kind", schemaPath: "#/oneOf/1/properties/kind/const", keyword: "const", params: { allowedValue: "BROKER_IDENTITY" }, message: "must be equal to constant" };
        if (vErrors === null) {
          vErrors = [err7];
        } else {
          vErrors.push(err7);
        }
        errors++;
      }
    }
  }
  var _valid0 = _errs6 === errors;
  if (_valid0 && valid0) {
    valid0 = false;
    passing0 = [passing0, 1];
  } else {
    if (_valid0) {
      valid0 = true;
      passing0 = 1;
      if (props0 !== true) {
        props0 = props0 || {};
        props0.kind = true;
      }
    }
  }
  if (!valid0) {
    const err8 = { instancePath, schemaPath: "#/oneOf", keyword: "oneOf", params: { passingSchemas: passing0 }, message: "must match exactly one schema in oneOf" };
    if (vErrors === null) {
      vErrors = [err8];
    } else {
      vErrors.push(err8);
    }
    errors++;
  } else {
    errors = _errs1;
    if (vErrors !== null) {
      if (_errs1) {
        vErrors.length = _errs1;
      } else {
        vErrors = null;
      }
    }
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.kind === void 0) {
      const err9 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "kind" }, message: "must have required property 'kind'" };
      if (vErrors === null) {
        vErrors = [err9];
      } else {
        vErrors.push(err9);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!(key0 === "secretKey" || key0 === "kind" || key0 === "brokerPurpose")) {
        const err10 = { instancePath, schemaPath: "#/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key0 }, message: "must NOT have additional properties" };
        if (vErrors === null) {
          vErrors = [err10];
        } else {
          vErrors.push(err10);
        }
        errors++;
      }
    }
    if (data.secretKey !== void 0) {
      let data2 = data.secretKey;
      if (typeof data2 === "string") {
        if (func1(data2) > 120) {
          const err11 = { instancePath: instancePath + "/secretKey", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err11];
          } else {
            vErrors.push(err11);
          }
          errors++;
        }
        if (!pattern4.test(data2)) {
          const err12 = { instancePath: instancePath + "/secretKey", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$"' };
          if (vErrors === null) {
            vErrors = [err12];
          } else {
            vErrors.push(err12);
          }
          errors++;
        }
      } else {
        const err13 = { instancePath: instancePath + "/secretKey", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err13];
        } else {
          vErrors.push(err13);
        }
        errors++;
      }
    }
    if (data.kind !== void 0) {
      let data3 = data.kind;
      if (!(data3 === "TOKEN" || data3 === "PASSWORD" || data3 === "BROKER_IDENTITY")) {
        const err14 = { instancePath: instancePath + "/kind", schemaPath: "#/properties/kind/enum", keyword: "enum", params: { allowedValues: schema34.properties.kind.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err14];
        } else {
          vErrors.push(err14);
        }
        errors++;
      }
    }
    if (data.brokerPurpose !== void 0) {
      let data4 = data.brokerPurpose;
      if (!(data4 === "KUBERNETES_TARGET" || data4 === "SSH_TARGET")) {
        const err15 = { instancePath: instancePath + "/brokerPurpose", schemaPath: "#/properties/brokerPurpose/enum", keyword: "enum", params: { allowedValues: schema34.properties.brokerPurpose.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err15];
        } else {
          vErrors.push(err15);
        }
        errors++;
      }
    }
  } else {
    const err16 = { instancePath, schemaPath: "#/type", keyword: "type", params: { type: "object" }, message: "must be object" };
    if (vErrors === null) {
      vErrors = [err16];
    } else {
      vErrors.push(err16);
    }
    errors++;
  }
  validate21.errors = vErrors;
  return errors === 0;
}
validate21.evaluated = { "props": true, "dynamicProps": false, "dynamicItems": false };
var schema36 = { "type": "object", "additionalProperties": false, "required": ["key", "type", "required"], "properties": { "key": { "$ref": "#/$defs/key" }, "label": { "type": "string", "maxLength": 120, "pattern": "^[^\\u0000\\r\\n]*$" }, "help": { "type": "string", "maxLength": 500, "pattern": "^[^\\u0000\\r\\n]*$" }, "type": { "enum": ["STRING", "INTEGER", "BOOLEAN"] }, "format": { "enum": ["PLAIN", "HTTPS_ORIGIN", "HTTPS_URL", "HTTPS_PATH", "EMAIL", "HOST", "IDENTIFIER", "PUBLIC_IP"] }, "required": { "type": "boolean" }, "maximumLength": { "type": "integer", "minimum": 1, "maximum": 349528 }, "allowEmpty": { "type": "boolean", "description": "\u041F\u0443\u0441\u0442\u0430\u044F \u0441\u0442\u0440\u043E\u043A\u0430 \u0434\u043E\u043F\u0443\u0441\u0442\u0438\u043C\u0430 \u0442\u043E\u043B\u044C\u043A\u043E \u0432 PLAIN input/output, \u043D\u0435 \u0432 \u043A\u043E\u043D\u0444\u0438\u0433\u0443\u0440\u0430\u0446\u0438\u0438 \u043F\u043E\u0434\u043A\u043B\u044E\u0447\u0435\u043D\u0438\u044F." }, "minimum": { "type": "integer", "minimum": 0 }, "maximum": { "type": "integer", "minimum": 0 }, "allowedValues": { "type": "array", "minItems": 1, "maxItems": 32, "uniqueItems": true, "items": { "type": "string", "minLength": 1, "maxLength": 120 } } }, "allOf": [{ "if": { "properties": { "allowEmpty": { "const": true } }, "required": ["allowEmpty"] }, "then": { "properties": { "type": { "const": "STRING" }, "format": { "const": "PLAIN" } }, "required": ["format"], "not": { "required": ["allowedValues"] } } }] };
var pattern9 = new RegExp("^[^\\u0000\\r\\n]*$", "u");
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
  const _errs2 = errors;
  let valid1 = true;
  const _errs3 = errors;
  if (data && typeof data == "object" && !Array.isArray(data)) {
    let missing0;
    if (data.allowEmpty === void 0 && (missing0 = "allowEmpty")) {
      const err0 = {};
      if (vErrors === null) {
        vErrors = [err0];
      } else {
        vErrors.push(err0);
      }
      errors++;
    } else {
      if (data.allowEmpty !== void 0) {
        if (true !== data.allowEmpty) {
          const err1 = {};
          if (vErrors === null) {
            vErrors = [err1];
          } else {
            vErrors.push(err1);
          }
          errors++;
        }
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
      let missing1;
      if (data.allowedValues === void 0 && (missing1 = "allowedValues")) {
        const err2 = {};
        if (vErrors === null) {
          vErrors = [err2];
        } else {
          vErrors.push(err2);
        }
        errors++;
      }
    }
    var valid3 = _errs7 === errors;
    if (valid3) {
      const err3 = { instancePath, schemaPath: "#/allOf/0/then/not", keyword: "not", params: {}, message: "must NOT be valid" };
      if (vErrors === null) {
        vErrors = [err3];
      } else {
        vErrors.push(err3);
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
      if (data.format === void 0) {
        const err4 = { instancePath, schemaPath: "#/allOf/0/then/required", keyword: "required", params: { missingProperty: "format" }, message: "must have required property 'format'" };
        if (vErrors === null) {
          vErrors = [err4];
        } else {
          vErrors.push(err4);
        }
        errors++;
      }
      if (data.type !== void 0) {
        if ("STRING" !== data.type) {
          const err5 = { instancePath: instancePath + "/type", schemaPath: "#/allOf/0/then/properties/type/const", keyword: "const", params: { allowedValue: "STRING" }, message: "must be equal to constant" };
          if (vErrors === null) {
            vErrors = [err5];
          } else {
            vErrors.push(err5);
          }
          errors++;
        }
      }
      if (data.format !== void 0) {
        if ("PLAIN" !== data.format) {
          const err6 = { instancePath: instancePath + "/format", schemaPath: "#/allOf/0/then/properties/format/const", keyword: "const", params: { allowedValue: "PLAIN" }, message: "must be equal to constant" };
          if (vErrors === null) {
            vErrors = [err6];
          } else {
            vErrors.push(err6);
          }
          errors++;
        }
      }
    }
    var _valid0 = _errs5 === errors;
    valid1 = _valid0;
    if (valid1) {
      var props0 = {};
      props0.type = true;
      props0.format = true;
      props0.allowEmpty = true;
    }
  }
  if (!valid1) {
    const err7 = { instancePath, schemaPath: "#/allOf/0/if", keyword: "if", params: { failingKeyword: "then" }, message: 'must match "then" schema' };
    if (vErrors === null) {
      vErrors = [err7];
    } else {
      vErrors.push(err7);
    }
    errors++;
  }
  if (data && typeof data == "object" && !Array.isArray(data)) {
    if (data.key === void 0) {
      const err8 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "key" }, message: "must have required property 'key'" };
      if (vErrors === null) {
        vErrors = [err8];
      } else {
        vErrors.push(err8);
      }
      errors++;
    }
    if (data.type === void 0) {
      const err9 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "type" }, message: "must have required property 'type'" };
      if (vErrors === null) {
        vErrors = [err9];
      } else {
        vErrors.push(err9);
      }
      errors++;
    }
    if (data.required === void 0) {
      const err10 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "required" }, message: "must have required property 'required'" };
      if (vErrors === null) {
        vErrors = [err10];
      } else {
        vErrors.push(err10);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!func3.call(schema36.properties, key0)) {
        const err11 = { instancePath, schemaPath: "#/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key0 }, message: "must NOT have additional properties" };
        if (vErrors === null) {
          vErrors = [err11];
        } else {
          vErrors.push(err11);
        }
        errors++;
      }
    }
    if (data.key !== void 0) {
      let data3 = data.key;
      if (typeof data3 === "string") {
        if (func1(data3) > 120) {
          const err12 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err12];
          } else {
            vErrors.push(err12);
          }
          errors++;
        }
        if (!pattern4.test(data3)) {
          const err13 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$"' };
          if (vErrors === null) {
            vErrors = [err13];
          } else {
            vErrors.push(err13);
          }
          errors++;
        }
      } else {
        const err14 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err14];
        } else {
          vErrors.push(err14);
        }
        errors++;
      }
    }
    if (data.label !== void 0) {
      let data4 = data.label;
      if (typeof data4 === "string") {
        if (func1(data4) > 120) {
          const err15 = { instancePath: instancePath + "/label", schemaPath: "#/properties/label/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err15];
          } else {
            vErrors.push(err15);
          }
          errors++;
        }
        if (!pattern9.test(data4)) {
          const err16 = { instancePath: instancePath + "/label", schemaPath: "#/properties/label/pattern", keyword: "pattern", params: { pattern: "^[^\\u0000\\r\\n]*$" }, message: 'must match pattern "^[^\\u0000\\r\\n]*$"' };
          if (vErrors === null) {
            vErrors = [err16];
          } else {
            vErrors.push(err16);
          }
          errors++;
        }
      } else {
        const err17 = { instancePath: instancePath + "/label", schemaPath: "#/properties/label/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err17];
        } else {
          vErrors.push(err17);
        }
        errors++;
      }
    }
    if (data.help !== void 0) {
      let data5 = data.help;
      if (typeof data5 === "string") {
        if (func1(data5) > 500) {
          const err18 = { instancePath: instancePath + "/help", schemaPath: "#/properties/help/maxLength", keyword: "maxLength", params: { limit: 500 }, message: "must NOT have more than 500 characters" };
          if (vErrors === null) {
            vErrors = [err18];
          } else {
            vErrors.push(err18);
          }
          errors++;
        }
        if (!pattern9.test(data5)) {
          const err19 = { instancePath: instancePath + "/help", schemaPath: "#/properties/help/pattern", keyword: "pattern", params: { pattern: "^[^\\u0000\\r\\n]*$" }, message: 'must match pattern "^[^\\u0000\\r\\n]*$"' };
          if (vErrors === null) {
            vErrors = [err19];
          } else {
            vErrors.push(err19);
          }
          errors++;
        }
      } else {
        const err20 = { instancePath: instancePath + "/help", schemaPath: "#/properties/help/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err20];
        } else {
          vErrors.push(err20);
        }
        errors++;
      }
    }
    if (data.type !== void 0) {
      let data6 = data.type;
      if (!(data6 === "STRING" || data6 === "INTEGER" || data6 === "BOOLEAN")) {
        const err21 = { instancePath: instancePath + "/type", schemaPath: "#/properties/type/enum", keyword: "enum", params: { allowedValues: schema36.properties.type.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err21];
        } else {
          vErrors.push(err21);
        }
        errors++;
      }
    }
    if (data.format !== void 0) {
      let data7 = data.format;
      if (!(data7 === "PLAIN" || data7 === "HTTPS_ORIGIN" || data7 === "HTTPS_URL" || data7 === "HTTPS_PATH" || data7 === "EMAIL" || data7 === "HOST" || data7 === "IDENTIFIER" || data7 === "PUBLIC_IP")) {
        const err22 = { instancePath: instancePath + "/format", schemaPath: "#/properties/format/enum", keyword: "enum", params: { allowedValues: schema36.properties.format.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err22];
        } else {
          vErrors.push(err22);
        }
        errors++;
      }
    }
    if (data.required !== void 0) {
      if (typeof data.required !== "boolean") {
        const err23 = { instancePath: instancePath + "/required", schemaPath: "#/properties/required/type", keyword: "type", params: { type: "boolean" }, message: "must be boolean" };
        if (vErrors === null) {
          vErrors = [err23];
        } else {
          vErrors.push(err23);
        }
        errors++;
      }
    }
    if (data.maximumLength !== void 0) {
      let data9 = data.maximumLength;
      if (!(typeof data9 == "number" && (!(data9 % 1) && !isNaN(data9)) && isFinite(data9))) {
        const err24 = { instancePath: instancePath + "/maximumLength", schemaPath: "#/properties/maximumLength/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
        if (vErrors === null) {
          vErrors = [err24];
        } else {
          vErrors.push(err24);
        }
        errors++;
      }
      if (typeof data9 == "number" && isFinite(data9)) {
        if (data9 > 349528 || isNaN(data9)) {
          const err25 = { instancePath: instancePath + "/maximumLength", schemaPath: "#/properties/maximumLength/maximum", keyword: "maximum", params: { comparison: "<=", limit: 349528 }, message: "must be <= 349528" };
          if (vErrors === null) {
            vErrors = [err25];
          } else {
            vErrors.push(err25);
          }
          errors++;
        }
        if (data9 < 1 || isNaN(data9)) {
          const err26 = { instancePath: instancePath + "/maximumLength", schemaPath: "#/properties/maximumLength/minimum", keyword: "minimum", params: { comparison: ">=", limit: 1 }, message: "must be >= 1" };
          if (vErrors === null) {
            vErrors = [err26];
          } else {
            vErrors.push(err26);
          }
          errors++;
        }
      }
    }
    if (data.allowEmpty !== void 0) {
      if (typeof data.allowEmpty !== "boolean") {
        const err27 = { instancePath: instancePath + "/allowEmpty", schemaPath: "#/properties/allowEmpty/type", keyword: "type", params: { type: "boolean" }, message: "must be boolean" };
        if (vErrors === null) {
          vErrors = [err27];
        } else {
          vErrors.push(err27);
        }
        errors++;
      }
    }
    if (data.minimum !== void 0) {
      let data11 = data.minimum;
      if (!(typeof data11 == "number" && (!(data11 % 1) && !isNaN(data11)) && isFinite(data11))) {
        const err28 = { instancePath: instancePath + "/minimum", schemaPath: "#/properties/minimum/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
        if (vErrors === null) {
          vErrors = [err28];
        } else {
          vErrors.push(err28);
        }
        errors++;
      }
      if (typeof data11 == "number" && isFinite(data11)) {
        if (data11 < 0 || isNaN(data11)) {
          const err29 = { instancePath: instancePath + "/minimum", schemaPath: "#/properties/minimum/minimum", keyword: "minimum", params: { comparison: ">=", limit: 0 }, message: "must be >= 0" };
          if (vErrors === null) {
            vErrors = [err29];
          } else {
            vErrors.push(err29);
          }
          errors++;
        }
      }
    }
    if (data.maximum !== void 0) {
      let data12 = data.maximum;
      if (!(typeof data12 == "number" && (!(data12 % 1) && !isNaN(data12)) && isFinite(data12))) {
        const err30 = { instancePath: instancePath + "/maximum", schemaPath: "#/properties/maximum/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
        if (vErrors === null) {
          vErrors = [err30];
        } else {
          vErrors.push(err30);
        }
        errors++;
      }
      if (typeof data12 == "number" && isFinite(data12)) {
        if (data12 < 0 || isNaN(data12)) {
          const err31 = { instancePath: instancePath + "/maximum", schemaPath: "#/properties/maximum/minimum", keyword: "minimum", params: { comparison: ">=", limit: 0 }, message: "must be >= 0" };
          if (vErrors === null) {
            vErrors = [err31];
          } else {
            vErrors.push(err31);
          }
          errors++;
        }
      }
    }
    if (data.allowedValues !== void 0) {
      let data13 = data.allowedValues;
      if (Array.isArray(data13)) {
        if (data13.length > 32) {
          const err32 = { instancePath: instancePath + "/allowedValues", schemaPath: "#/properties/allowedValues/maxItems", keyword: "maxItems", params: { limit: 32 }, message: "must NOT have more than 32 items" };
          if (vErrors === null) {
            vErrors = [err32];
          } else {
            vErrors.push(err32);
          }
          errors++;
        }
        if (data13.length < 1) {
          const err33 = { instancePath: instancePath + "/allowedValues", schemaPath: "#/properties/allowedValues/minItems", keyword: "minItems", params: { limit: 1 }, message: "must NOT have fewer than 1 items" };
          if (vErrors === null) {
            vErrors = [err33];
          } else {
            vErrors.push(err33);
          }
          errors++;
        }
        const len0 = data13.length;
        for (let i0 = 0; i0 < len0; i0++) {
          let data14 = data13[i0];
          if (typeof data14 === "string") {
            if (func1(data14) > 120) {
              const err34 = { instancePath: instancePath + "/allowedValues/" + i0, schemaPath: "#/properties/allowedValues/items/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
              if (vErrors === null) {
                vErrors = [err34];
              } else {
                vErrors.push(err34);
              }
              errors++;
            }
            if (func1(data14) < 1) {
              const err35 = { instancePath: instancePath + "/allowedValues/" + i0, schemaPath: "#/properties/allowedValues/items/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
              if (vErrors === null) {
                vErrors = [err35];
              } else {
                vErrors.push(err35);
              }
              errors++;
            }
          } else {
            const err36 = { instancePath: instancePath + "/allowedValues/" + i0, schemaPath: "#/properties/allowedValues/items/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err36];
            } else {
              vErrors.push(err36);
            }
            errors++;
          }
        }
        let i1 = data13.length;
        let j0;
        if (i1 > 1) {
          const indices0 = {};
          for (; i1--; ) {
            let item0 = data13[i1];
            if (typeof item0 !== "string") {
              continue;
            }
            if (typeof indices0[item0] == "number") {
              j0 = indices0[item0];
              const err37 = { instancePath: instancePath + "/allowedValues", schemaPath: "#/properties/allowedValues/uniqueItems", keyword: "uniqueItems", params: { i: i1, j: j0 }, message: "must NOT have duplicate items (items ## " + j0 + " and " + i1 + " are identical)" };
              if (vErrors === null) {
                vErrors = [err37];
              } else {
                vErrors.push(err37);
              }
              errors++;
              break;
            }
            indices0[item0] = i1;
          }
        }
      } else {
        const err38 = { instancePath: instancePath + "/allowedValues", schemaPath: "#/properties/allowedValues/type", keyword: "type", params: { type: "array" }, message: "must be array" };
        if (vErrors === null) {
          vErrors = [err38];
        } else {
          vErrors.push(err38);
        }
        errors++;
      }
    }
  } else {
    const err39 = { instancePath, schemaPath: "#/type", keyword: "type", params: { type: "object" }, message: "must be object" };
    if (vErrors === null) {
      vErrors = [err39];
    } else {
      vErrors.push(err39);
    }
    errors++;
  }
  validate23.errors = vErrors;
  return errors === 0;
}
validate23.evaluated = { "props": true, "dynamicProps": false, "dynamicItems": false };
var schema38 = { "type": "object", "additionalProperties": false, "required": ["key", "source", "port", "tls"], "properties": { "key": { "$ref": "#/$defs/key" }, "source": { "enum": ["STATIC", "CONFIGURATION"] }, "hostname": { "type": "string", "pattern": "^[a-z0-9](?:[a-z0-9.\\-]{0,251}[a-z0-9])?$", "maxLength": 253 }, "configurationField": { "$ref": "#/$defs/key" }, "port": { "type": "integer", "minimum": 1, "maximum": 65535 }, "tls": { "enum": ["REQUIRED", "NONE", "SSH_PINNED"] } }, "allOf": [{ "if": { "properties": { "source": { "const": "STATIC" } } }, "then": { "required": ["hostname"], "not": { "required": ["configurationField"] } } }, { "if": { "properties": { "source": { "const": "CONFIGURATION" } } }, "then": { "required": ["configurationField"], "not": { "required": ["hostname"] } } }] };
var pattern12 = new RegExp("^[a-z0-9](?:[a-z0-9.\\-]{0,251}[a-z0-9])?$", "u");
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
          const err16 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$"' };
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
        if (!pattern12.test(data4)) {
          const err20 = { instancePath: instancePath + "/hostname", schemaPath: "#/properties/hostname/pattern", keyword: "pattern", params: { pattern: "^[a-z0-9](?:[a-z0-9.\\-]{0,251}[a-z0-9])?$" }, message: 'must match pattern "^[a-z0-9](?:[a-z0-9.\\-]{0,251}[a-z0-9])?$"' };
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
          const err23 = { instancePath: instancePath + "/configurationField", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$"' };
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
      if (!(data7 === "REQUIRED" || data7 === "NONE" || data7 === "SSH_PINNED")) {
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
          const err5 = { instancePath: instancePath + "/operation", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$"' };
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
var schema43 = { "type": "object", "additionalProperties": false, "required": ["key", "name", "description", "operation", "risk", "approvalPolicy", "allowedApprovalPolicies", "resourceScope", "inputFields", "outputFields", "execution"], "properties": { "key": { "$ref": "#/$defs/key" }, "name": { "type": "string", "minLength": 1, "maxLength": 120 }, "description": { "type": "string", "minLength": 1, "maxLength": 500 }, "operation": { "$ref": "#/$defs/key" }, "risk": { "enum": ["READ", "WRITE", "SENSITIVE", "DESTRUCTIVE"] }, "approvalPolicy": { "enum": ["NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"] }, "allowedApprovalPolicies": { "type": "array", "minItems": 1, "maxItems": 3, "uniqueItems": true, "items": { "enum": ["NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"] } }, "resourceScope": { "$ref": "#/$defs/resourceScope" }, "approvalScope": { "type": "object", "additionalProperties": false, "required": ["name", "requiredInputPaths"], "properties": { "name": { "type": "string", "minLength": 1, "maxLength": 128, "pattern": "^[^\\u0000\\r\\n]*$" }, "allowedLifetimes": { "type": "array", "minItems": 1, "maxItems": 2, "uniqueItems": true, "items": { "enum": ["ROOT_RUN", "INITIATOR_RESOURCE"] } }, "requiredInputPaths": { "type": "array", "maxItems": 16, "uniqueItems": true, "items": { "type": "string", "minLength": 2, "maxLength": 256, "pattern": "^/" } }, "limits": { "type": "object", "additionalProperties": false, "required": ["durationSeconds", "maxEffects"], "properties": { "durationSeconds": { "type": "integer", "minimum": 1, "maximum": 604800 }, "maxEffects": { "type": "integer", "minimum": 1, "maximum": 1e4 } } } } }, "inputFields": { "type": "array", "maxItems": 24, "items": { "$ref": "#/$defs/field" } }, "outputFields": { "type": "array", "minItems": 1, "maxItems": 24, "items": { "$ref": "#/$defs/field" } }, "execution": { "$ref": "#/$defs/execution" }, "openapi": { "type": "object", "additionalProperties": false, "required": ["operationId", "method", "path", "authScheme", "inputSchema"], "properties": { "operationId": { "type": "string", "minLength": 1, "maxLength": 128 }, "sourceDigest": { "type": "string", "pattern": "^[a-f0-9]{64}$" }, "serverOrigin": { "type": "string", "format": "uri", "maxLength": 2048 }, "method": { "enum": ["GET", "POST", "PUT", "PATCH", "DELETE"] }, "path": { "type": "string", "minLength": 1, "maxLength": 512 }, "authScheme": { "enum": ["NONE", "BEARER", "API_KEY_HEADER"] }, "authHeader": { "type": "string", "maxLength": 64 }, "idempotencyHeader": { "type": "string", "maxLength": 64 }, "inputSchema": { "type": "object", "additionalProperties": true } } } } };
var schema48 = { "type": "object", "additionalProperties": false, "required": ["idempotency", "timeoutSeconds", "maxAttempts", "retryBackoffMilliseconds"], "allOf": [{ "if": { "required": ["leaseProfile"] }, "then": { "properties": { "maxAttempts": { "const": 1 } } }, "else": { "properties": { "timeoutSeconds": { "type": "integer", "maximum": 120 } } } }, { "if": { "required": ["leaseProfile"], "properties": { "leaseProfile": { "const": "SSH_COMPOSE_V1" } } }, "then": { "properties": { "idempotency": { "const": "EFFECT_KEY" } } } }], "properties": { "leaseProfile": { "enum": ["SSH_COMPOSE_V1", "MANAGED_KUBERNETES_V1"] }, "idempotency": { "enum": ["READ_ONLY", "EFFECT_KEY", "PROVIDER_NATIVE", "ONE_ATTEMPT"] }, "timeoutSeconds": { "type": "integer", "minimum": 1, "maximum": 600 }, "maxAttempts": { "type": "integer", "minimum": 1, "maximum": 4 }, "retryBackoffMilliseconds": { "type": "integer", "minimum": 50, "maximum": 5e3 } } };
var func0 = require_equal().default;
var pattern19 = new RegExp("^/", "u");
var pattern20 = new RegExp("^[a-f0-9]{64}$", "u");
var schema46 = { "type": "object", "additionalProperties": false, "required": ["kind", "connectionFields"], "properties": { "kind": { "enum": ["SYNTHETIC_JOURNAL", "GITHUB_REPOSITORY", "GITHUB_SCOPE", "GITLAB_PROJECT", "GITLAB_SCOPE", "JIRA_PROJECT", "CONFLUENCE_SPACE", "EMAIL_SENDER", "MATTERMOST_CHANNEL", "HTTPS_RESOURCE", "SSH_TARGET", "KUBERNETES_TARGET"] }, "connectionFields": { "type": "array", "minItems": 1, "maxItems": 8, "uniqueItems": true, "items": { "$ref": "#/$defs/key" } } } };
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
      if (!(data0 === "SYNTHETIC_JOURNAL" || data0 === "GITHUB_REPOSITORY" || data0 === "GITHUB_SCOPE" || data0 === "GITLAB_PROJECT" || data0 === "GITLAB_SCOPE" || data0 === "JIRA_PROJECT" || data0 === "CONFLUENCE_SPACE" || data0 === "EMAIL_SENDER" || data0 === "MATTERMOST_CHANNEL" || data0 === "HTTPS_RESOURCE" || data0 === "SSH_TARGET" || data0 === "KUBERNETES_TARGET")) {
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
              const err7 = { instancePath: instancePath + "/connectionFields/" + i0, schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$"' };
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
var formats0 = require_formats().fullFormats.uri;
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
    if (data.allowedApprovalPolicies === void 0) {
      const err6 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "allowedApprovalPolicies" }, message: "must have required property 'allowedApprovalPolicies'" };
      if (vErrors === null) {
        vErrors = [err6];
      } else {
        vErrors.push(err6);
      }
      errors++;
    }
    if (data.resourceScope === void 0) {
      const err7 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "resourceScope" }, message: "must have required property 'resourceScope'" };
      if (vErrors === null) {
        vErrors = [err7];
      } else {
        vErrors.push(err7);
      }
      errors++;
    }
    if (data.inputFields === void 0) {
      const err8 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "inputFields" }, message: "must have required property 'inputFields'" };
      if (vErrors === null) {
        vErrors = [err8];
      } else {
        vErrors.push(err8);
      }
      errors++;
    }
    if (data.outputFields === void 0) {
      const err9 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "outputFields" }, message: "must have required property 'outputFields'" };
      if (vErrors === null) {
        vErrors = [err9];
      } else {
        vErrors.push(err9);
      }
      errors++;
    }
    if (data.execution === void 0) {
      const err10 = { instancePath, schemaPath: "#/required", keyword: "required", params: { missingProperty: "execution" }, message: "must have required property 'execution'" };
      if (vErrors === null) {
        vErrors = [err10];
      } else {
        vErrors.push(err10);
      }
      errors++;
    }
    for (const key0 in data) {
      if (!func3.call(schema43.properties, key0)) {
        const err11 = { instancePath, schemaPath: "#/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key0 }, message: "must NOT have additional properties" };
        if (vErrors === null) {
          vErrors = [err11];
        } else {
          vErrors.push(err11);
        }
        errors++;
      }
    }
    if (data.key !== void 0) {
      let data0 = data.key;
      if (typeof data0 === "string") {
        if (func1(data0) > 120) {
          const err12 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err12];
          } else {
            vErrors.push(err12);
          }
          errors++;
        }
        if (!pattern4.test(data0)) {
          const err13 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$"' };
          if (vErrors === null) {
            vErrors = [err13];
          } else {
            vErrors.push(err13);
          }
          errors++;
        }
      } else {
        const err14 = { instancePath: instancePath + "/key", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err14];
        } else {
          vErrors.push(err14);
        }
        errors++;
      }
    }
    if (data.name !== void 0) {
      let data1 = data.name;
      if (typeof data1 === "string") {
        if (func1(data1) > 120) {
          const err15 = { instancePath: instancePath + "/name", schemaPath: "#/properties/name/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err15];
          } else {
            vErrors.push(err15);
          }
          errors++;
        }
        if (func1(data1) < 1) {
          const err16 = { instancePath: instancePath + "/name", schemaPath: "#/properties/name/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
          if (vErrors === null) {
            vErrors = [err16];
          } else {
            vErrors.push(err16);
          }
          errors++;
        }
      } else {
        const err17 = { instancePath: instancePath + "/name", schemaPath: "#/properties/name/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err17];
        } else {
          vErrors.push(err17);
        }
        errors++;
      }
    }
    if (data.description !== void 0) {
      let data2 = data.description;
      if (typeof data2 === "string") {
        if (func1(data2) > 500) {
          const err18 = { instancePath: instancePath + "/description", schemaPath: "#/properties/description/maxLength", keyword: "maxLength", params: { limit: 500 }, message: "must NOT have more than 500 characters" };
          if (vErrors === null) {
            vErrors = [err18];
          } else {
            vErrors.push(err18);
          }
          errors++;
        }
        if (func1(data2) < 1) {
          const err19 = { instancePath: instancePath + "/description", schemaPath: "#/properties/description/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
          if (vErrors === null) {
            vErrors = [err19];
          } else {
            vErrors.push(err19);
          }
          errors++;
        }
      } else {
        const err20 = { instancePath: instancePath + "/description", schemaPath: "#/properties/description/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err20];
        } else {
          vErrors.push(err20);
        }
        errors++;
      }
    }
    if (data.operation !== void 0) {
      let data3 = data.operation;
      if (typeof data3 === "string") {
        if (func1(data3) > 120) {
          const err21 = { instancePath: instancePath + "/operation", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
          if (vErrors === null) {
            vErrors = [err21];
          } else {
            vErrors.push(err21);
          }
          errors++;
        }
        if (!pattern4.test(data3)) {
          const err22 = { instancePath: instancePath + "/operation", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$"' };
          if (vErrors === null) {
            vErrors = [err22];
          } else {
            vErrors.push(err22);
          }
          errors++;
        }
      } else {
        const err23 = { instancePath: instancePath + "/operation", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
        if (vErrors === null) {
          vErrors = [err23];
        } else {
          vErrors.push(err23);
        }
        errors++;
      }
    }
    if (data.risk !== void 0) {
      let data4 = data.risk;
      if (!(data4 === "READ" || data4 === "WRITE" || data4 === "SENSITIVE" || data4 === "DESTRUCTIVE")) {
        const err24 = { instancePath: instancePath + "/risk", schemaPath: "#/properties/risk/enum", keyword: "enum", params: { allowedValues: schema43.properties.risk.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err24];
        } else {
          vErrors.push(err24);
        }
        errors++;
      }
    }
    if (data.approvalPolicy !== void 0) {
      let data5 = data.approvalPolicy;
      if (!(data5 === "NONE" || data5 === "HUMAN_EACH_EFFECT" || data5 === "HUMAN_SCOPED")) {
        const err25 = { instancePath: instancePath + "/approvalPolicy", schemaPath: "#/properties/approvalPolicy/enum", keyword: "enum", params: { allowedValues: schema43.properties.approvalPolicy.enum }, message: "must be equal to one of the allowed values" };
        if (vErrors === null) {
          vErrors = [err25];
        } else {
          vErrors.push(err25);
        }
        errors++;
      }
    }
    if (data.allowedApprovalPolicies !== void 0) {
      let data6 = data.allowedApprovalPolicies;
      if (Array.isArray(data6)) {
        if (data6.length > 3) {
          const err26 = { instancePath: instancePath + "/allowedApprovalPolicies", schemaPath: "#/properties/allowedApprovalPolicies/maxItems", keyword: "maxItems", params: { limit: 3 }, message: "must NOT have more than 3 items" };
          if (vErrors === null) {
            vErrors = [err26];
          } else {
            vErrors.push(err26);
          }
          errors++;
        }
        if (data6.length < 1) {
          const err27 = { instancePath: instancePath + "/allowedApprovalPolicies", schemaPath: "#/properties/allowedApprovalPolicies/minItems", keyword: "minItems", params: { limit: 1 }, message: "must NOT have fewer than 1 items" };
          if (vErrors === null) {
            vErrors = [err27];
          } else {
            vErrors.push(err27);
          }
          errors++;
        }
        const len0 = data6.length;
        for (let i0 = 0; i0 < len0; i0++) {
          let data7 = data6[i0];
          if (!(data7 === "NONE" || data7 === "HUMAN_EACH_EFFECT" || data7 === "HUMAN_SCOPED")) {
            const err28 = { instancePath: instancePath + "/allowedApprovalPolicies/" + i0, schemaPath: "#/properties/allowedApprovalPolicies/items/enum", keyword: "enum", params: { allowedValues: schema43.properties.allowedApprovalPolicies.items.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err28];
            } else {
              vErrors.push(err28);
            }
            errors++;
          }
        }
        let i1 = data6.length;
        let j0;
        if (i1 > 1) {
          outer0: for (; i1--; ) {
            for (j0 = i1; j0--; ) {
              if (func0(data6[i1], data6[j0])) {
                const err29 = { instancePath: instancePath + "/allowedApprovalPolicies", schemaPath: "#/properties/allowedApprovalPolicies/uniqueItems", keyword: "uniqueItems", params: { i: i1, j: j0 }, message: "must NOT have duplicate items (items ## " + j0 + " and " + i1 + " are identical)" };
                if (vErrors === null) {
                  vErrors = [err29];
                } else {
                  vErrors.push(err29);
                }
                errors++;
                break outer0;
              }
            }
          }
        }
      } else {
        const err30 = { instancePath: instancePath + "/allowedApprovalPolicies", schemaPath: "#/properties/allowedApprovalPolicies/type", keyword: "type", params: { type: "array" }, message: "must be array" };
        if (vErrors === null) {
          vErrors = [err30];
        } else {
          vErrors.push(err30);
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
    if (data.approvalScope !== void 0) {
      let data9 = data.approvalScope;
      if (data9 && typeof data9 == "object" && !Array.isArray(data9)) {
        if (data9.name === void 0) {
          const err31 = { instancePath: instancePath + "/approvalScope", schemaPath: "#/properties/approvalScope/required", keyword: "required", params: { missingProperty: "name" }, message: "must have required property 'name'" };
          if (vErrors === null) {
            vErrors = [err31];
          } else {
            vErrors.push(err31);
          }
          errors++;
        }
        if (data9.requiredInputPaths === void 0) {
          const err32 = { instancePath: instancePath + "/approvalScope", schemaPath: "#/properties/approvalScope/required", keyword: "required", params: { missingProperty: "requiredInputPaths" }, message: "must have required property 'requiredInputPaths'" };
          if (vErrors === null) {
            vErrors = [err32];
          } else {
            vErrors.push(err32);
          }
          errors++;
        }
        for (const key1 in data9) {
          if (!(key1 === "name" || key1 === "allowedLifetimes" || key1 === "requiredInputPaths" || key1 === "limits")) {
            const err33 = { instancePath: instancePath + "/approvalScope", schemaPath: "#/properties/approvalScope/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key1 }, message: "must NOT have additional properties" };
            if (vErrors === null) {
              vErrors = [err33];
            } else {
              vErrors.push(err33);
            }
            errors++;
          }
        }
        if (data9.name !== void 0) {
          let data10 = data9.name;
          if (typeof data10 === "string") {
            if (func1(data10) > 128) {
              const err34 = { instancePath: instancePath + "/approvalScope/name", schemaPath: "#/properties/approvalScope/properties/name/maxLength", keyword: "maxLength", params: { limit: 128 }, message: "must NOT have more than 128 characters" };
              if (vErrors === null) {
                vErrors = [err34];
              } else {
                vErrors.push(err34);
              }
              errors++;
            }
            if (func1(data10) < 1) {
              const err35 = { instancePath: instancePath + "/approvalScope/name", schemaPath: "#/properties/approvalScope/properties/name/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
              if (vErrors === null) {
                vErrors = [err35];
              } else {
                vErrors.push(err35);
              }
              errors++;
            }
            if (!pattern9.test(data10)) {
              const err36 = { instancePath: instancePath + "/approvalScope/name", schemaPath: "#/properties/approvalScope/properties/name/pattern", keyword: "pattern", params: { pattern: "^[^\\u0000\\r\\n]*$" }, message: 'must match pattern "^[^\\u0000\\r\\n]*$"' };
              if (vErrors === null) {
                vErrors = [err36];
              } else {
                vErrors.push(err36);
              }
              errors++;
            }
          } else {
            const err37 = { instancePath: instancePath + "/approvalScope/name", schemaPath: "#/properties/approvalScope/properties/name/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err37];
            } else {
              vErrors.push(err37);
            }
            errors++;
          }
        }
        if (data9.allowedLifetimes !== void 0) {
          let data11 = data9.allowedLifetimes;
          if (Array.isArray(data11)) {
            if (data11.length > 2) {
              const err38 = { instancePath: instancePath + "/approvalScope/allowedLifetimes", schemaPath: "#/properties/approvalScope/properties/allowedLifetimes/maxItems", keyword: "maxItems", params: { limit: 2 }, message: "must NOT have more than 2 items" };
              if (vErrors === null) {
                vErrors = [err38];
              } else {
                vErrors.push(err38);
              }
              errors++;
            }
            if (data11.length < 1) {
              const err39 = { instancePath: instancePath + "/approvalScope/allowedLifetimes", schemaPath: "#/properties/approvalScope/properties/allowedLifetimes/minItems", keyword: "minItems", params: { limit: 1 }, message: "must NOT have fewer than 1 items" };
              if (vErrors === null) {
                vErrors = [err39];
              } else {
                vErrors.push(err39);
              }
              errors++;
            }
            const len1 = data11.length;
            for (let i2 = 0; i2 < len1; i2++) {
              let data12 = data11[i2];
              if (!(data12 === "ROOT_RUN" || data12 === "INITIATOR_RESOURCE")) {
                const err40 = { instancePath: instancePath + "/approvalScope/allowedLifetimes/" + i2, schemaPath: "#/properties/approvalScope/properties/allowedLifetimes/items/enum", keyword: "enum", params: { allowedValues: schema43.properties.approvalScope.properties.allowedLifetimes.items.enum }, message: "must be equal to one of the allowed values" };
                if (vErrors === null) {
                  vErrors = [err40];
                } else {
                  vErrors.push(err40);
                }
                errors++;
              }
            }
            let i3 = data11.length;
            let j1;
            if (i3 > 1) {
              outer1: for (; i3--; ) {
                for (j1 = i3; j1--; ) {
                  if (func0(data11[i3], data11[j1])) {
                    const err41 = { instancePath: instancePath + "/approvalScope/allowedLifetimes", schemaPath: "#/properties/approvalScope/properties/allowedLifetimes/uniqueItems", keyword: "uniqueItems", params: { i: i3, j: j1 }, message: "must NOT have duplicate items (items ## " + j1 + " and " + i3 + " are identical)" };
                    if (vErrors === null) {
                      vErrors = [err41];
                    } else {
                      vErrors.push(err41);
                    }
                    errors++;
                    break outer1;
                  }
                }
              }
            }
          } else {
            const err42 = { instancePath: instancePath + "/approvalScope/allowedLifetimes", schemaPath: "#/properties/approvalScope/properties/allowedLifetimes/type", keyword: "type", params: { type: "array" }, message: "must be array" };
            if (vErrors === null) {
              vErrors = [err42];
            } else {
              vErrors.push(err42);
            }
            errors++;
          }
        }
        if (data9.requiredInputPaths !== void 0) {
          let data13 = data9.requiredInputPaths;
          if (Array.isArray(data13)) {
            if (data13.length > 16) {
              const err43 = { instancePath: instancePath + "/approvalScope/requiredInputPaths", schemaPath: "#/properties/approvalScope/properties/requiredInputPaths/maxItems", keyword: "maxItems", params: { limit: 16 }, message: "must NOT have more than 16 items" };
              if (vErrors === null) {
                vErrors = [err43];
              } else {
                vErrors.push(err43);
              }
              errors++;
            }
            const len2 = data13.length;
            for (let i4 = 0; i4 < len2; i4++) {
              let data14 = data13[i4];
              if (typeof data14 === "string") {
                if (func1(data14) > 256) {
                  const err44 = { instancePath: instancePath + "/approvalScope/requiredInputPaths/" + i4, schemaPath: "#/properties/approvalScope/properties/requiredInputPaths/items/maxLength", keyword: "maxLength", params: { limit: 256 }, message: "must NOT have more than 256 characters" };
                  if (vErrors === null) {
                    vErrors = [err44];
                  } else {
                    vErrors.push(err44);
                  }
                  errors++;
                }
                if (func1(data14) < 2) {
                  const err45 = { instancePath: instancePath + "/approvalScope/requiredInputPaths/" + i4, schemaPath: "#/properties/approvalScope/properties/requiredInputPaths/items/minLength", keyword: "minLength", params: { limit: 2 }, message: "must NOT have fewer than 2 characters" };
                  if (vErrors === null) {
                    vErrors = [err45];
                  } else {
                    vErrors.push(err45);
                  }
                  errors++;
                }
                if (!pattern19.test(data14)) {
                  const err46 = { instancePath: instancePath + "/approvalScope/requiredInputPaths/" + i4, schemaPath: "#/properties/approvalScope/properties/requiredInputPaths/items/pattern", keyword: "pattern", params: { pattern: "^/" }, message: 'must match pattern "^/"' };
                  if (vErrors === null) {
                    vErrors = [err46];
                  } else {
                    vErrors.push(err46);
                  }
                  errors++;
                }
              } else {
                const err47 = { instancePath: instancePath + "/approvalScope/requiredInputPaths/" + i4, schemaPath: "#/properties/approvalScope/properties/requiredInputPaths/items/type", keyword: "type", params: { type: "string" }, message: "must be string" };
                if (vErrors === null) {
                  vErrors = [err47];
                } else {
                  vErrors.push(err47);
                }
                errors++;
              }
            }
            let i5 = data13.length;
            let j2;
            if (i5 > 1) {
              const indices0 = {};
              for (; i5--; ) {
                let item0 = data13[i5];
                if (typeof item0 !== "string") {
                  continue;
                }
                if (typeof indices0[item0] == "number") {
                  j2 = indices0[item0];
                  const err48 = { instancePath: instancePath + "/approvalScope/requiredInputPaths", schemaPath: "#/properties/approvalScope/properties/requiredInputPaths/uniqueItems", keyword: "uniqueItems", params: { i: i5, j: j2 }, message: "must NOT have duplicate items (items ## " + j2 + " and " + i5 + " are identical)" };
                  if (vErrors === null) {
                    vErrors = [err48];
                  } else {
                    vErrors.push(err48);
                  }
                  errors++;
                  break;
                }
                indices0[item0] = i5;
              }
            }
          } else {
            const err49 = { instancePath: instancePath + "/approvalScope/requiredInputPaths", schemaPath: "#/properties/approvalScope/properties/requiredInputPaths/type", keyword: "type", params: { type: "array" }, message: "must be array" };
            if (vErrors === null) {
              vErrors = [err49];
            } else {
              vErrors.push(err49);
            }
            errors++;
          }
        }
        if (data9.limits !== void 0) {
          let data15 = data9.limits;
          if (data15 && typeof data15 == "object" && !Array.isArray(data15)) {
            if (data15.durationSeconds === void 0) {
              const err50 = { instancePath: instancePath + "/approvalScope/limits", schemaPath: "#/properties/approvalScope/properties/limits/required", keyword: "required", params: { missingProperty: "durationSeconds" }, message: "must have required property 'durationSeconds'" };
              if (vErrors === null) {
                vErrors = [err50];
              } else {
                vErrors.push(err50);
              }
              errors++;
            }
            if (data15.maxEffects === void 0) {
              const err51 = { instancePath: instancePath + "/approvalScope/limits", schemaPath: "#/properties/approvalScope/properties/limits/required", keyword: "required", params: { missingProperty: "maxEffects" }, message: "must have required property 'maxEffects'" };
              if (vErrors === null) {
                vErrors = [err51];
              } else {
                vErrors.push(err51);
              }
              errors++;
            }
            for (const key2 in data15) {
              if (!(key2 === "durationSeconds" || key2 === "maxEffects")) {
                const err52 = { instancePath: instancePath + "/approvalScope/limits", schemaPath: "#/properties/approvalScope/properties/limits/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key2 }, message: "must NOT have additional properties" };
                if (vErrors === null) {
                  vErrors = [err52];
                } else {
                  vErrors.push(err52);
                }
                errors++;
              }
            }
            if (data15.durationSeconds !== void 0) {
              let data16 = data15.durationSeconds;
              if (!(typeof data16 == "number" && (!(data16 % 1) && !isNaN(data16)) && isFinite(data16))) {
                const err53 = { instancePath: instancePath + "/approvalScope/limits/durationSeconds", schemaPath: "#/properties/approvalScope/properties/limits/properties/durationSeconds/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
                if (vErrors === null) {
                  vErrors = [err53];
                } else {
                  vErrors.push(err53);
                }
                errors++;
              }
              if (typeof data16 == "number" && isFinite(data16)) {
                if (data16 > 604800 || isNaN(data16)) {
                  const err54 = { instancePath: instancePath + "/approvalScope/limits/durationSeconds", schemaPath: "#/properties/approvalScope/properties/limits/properties/durationSeconds/maximum", keyword: "maximum", params: { comparison: "<=", limit: 604800 }, message: "must be <= 604800" };
                  if (vErrors === null) {
                    vErrors = [err54];
                  } else {
                    vErrors.push(err54);
                  }
                  errors++;
                }
                if (data16 < 1 || isNaN(data16)) {
                  const err55 = { instancePath: instancePath + "/approvalScope/limits/durationSeconds", schemaPath: "#/properties/approvalScope/properties/limits/properties/durationSeconds/minimum", keyword: "minimum", params: { comparison: ">=", limit: 1 }, message: "must be >= 1" };
                  if (vErrors === null) {
                    vErrors = [err55];
                  } else {
                    vErrors.push(err55);
                  }
                  errors++;
                }
              }
            }
            if (data15.maxEffects !== void 0) {
              let data17 = data15.maxEffects;
              if (!(typeof data17 == "number" && (!(data17 % 1) && !isNaN(data17)) && isFinite(data17))) {
                const err56 = { instancePath: instancePath + "/approvalScope/limits/maxEffects", schemaPath: "#/properties/approvalScope/properties/limits/properties/maxEffects/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
                if (vErrors === null) {
                  vErrors = [err56];
                } else {
                  vErrors.push(err56);
                }
                errors++;
              }
              if (typeof data17 == "number" && isFinite(data17)) {
                if (data17 > 1e4 || isNaN(data17)) {
                  const err57 = { instancePath: instancePath + "/approvalScope/limits/maxEffects", schemaPath: "#/properties/approvalScope/properties/limits/properties/maxEffects/maximum", keyword: "maximum", params: { comparison: "<=", limit: 1e4 }, message: "must be <= 10000" };
                  if (vErrors === null) {
                    vErrors = [err57];
                  } else {
                    vErrors.push(err57);
                  }
                  errors++;
                }
                if (data17 < 1 || isNaN(data17)) {
                  const err58 = { instancePath: instancePath + "/approvalScope/limits/maxEffects", schemaPath: "#/properties/approvalScope/properties/limits/properties/maxEffects/minimum", keyword: "minimum", params: { comparison: ">=", limit: 1 }, message: "must be >= 1" };
                  if (vErrors === null) {
                    vErrors = [err58];
                  } else {
                    vErrors.push(err58);
                  }
                  errors++;
                }
              }
            }
          } else {
            const err59 = { instancePath: instancePath + "/approvalScope/limits", schemaPath: "#/properties/approvalScope/properties/limits/type", keyword: "type", params: { type: "object" }, message: "must be object" };
            if (vErrors === null) {
              vErrors = [err59];
            } else {
              vErrors.push(err59);
            }
            errors++;
          }
        }
      } else {
        const err60 = { instancePath: instancePath + "/approvalScope", schemaPath: "#/properties/approvalScope/type", keyword: "type", params: { type: "object" }, message: "must be object" };
        if (vErrors === null) {
          vErrors = [err60];
        } else {
          vErrors.push(err60);
        }
        errors++;
      }
    }
    if (data.inputFields !== void 0) {
      let data18 = data.inputFields;
      if (Array.isArray(data18)) {
        if (data18.length > 24) {
          const err61 = { instancePath: instancePath + "/inputFields", schemaPath: "#/properties/inputFields/maxItems", keyword: "maxItems", params: { limit: 24 }, message: "must NOT have more than 24 items" };
          if (vErrors === null) {
            vErrors = [err61];
          } else {
            vErrors.push(err61);
          }
          errors++;
        }
        const len3 = data18.length;
        for (let i6 = 0; i6 < len3; i6++) {
          if (!validate23(data18[i6], { instancePath: instancePath + "/inputFields/" + i6, parentData: data18, parentDataProperty: i6, rootData, dynamicAnchors })) {
            vErrors = vErrors === null ? validate23.errors : vErrors.concat(validate23.errors);
            errors = vErrors.length;
          }
        }
      } else {
        const err62 = { instancePath: instancePath + "/inputFields", schemaPath: "#/properties/inputFields/type", keyword: "type", params: { type: "array" }, message: "must be array" };
        if (vErrors === null) {
          vErrors = [err62];
        } else {
          vErrors.push(err62);
        }
        errors++;
      }
    }
    if (data.outputFields !== void 0) {
      let data20 = data.outputFields;
      if (Array.isArray(data20)) {
        if (data20.length > 24) {
          const err63 = { instancePath: instancePath + "/outputFields", schemaPath: "#/properties/outputFields/maxItems", keyword: "maxItems", params: { limit: 24 }, message: "must NOT have more than 24 items" };
          if (vErrors === null) {
            vErrors = [err63];
          } else {
            vErrors.push(err63);
          }
          errors++;
        }
        if (data20.length < 1) {
          const err64 = { instancePath: instancePath + "/outputFields", schemaPath: "#/properties/outputFields/minItems", keyword: "minItems", params: { limit: 1 }, message: "must NOT have fewer than 1 items" };
          if (vErrors === null) {
            vErrors = [err64];
          } else {
            vErrors.push(err64);
          }
          errors++;
        }
        const len4 = data20.length;
        for (let i7 = 0; i7 < len4; i7++) {
          if (!validate23(data20[i7], { instancePath: instancePath + "/outputFields/" + i7, parentData: data20, parentDataProperty: i7, rootData, dynamicAnchors })) {
            vErrors = vErrors === null ? validate23.errors : vErrors.concat(validate23.errors);
            errors = vErrors.length;
          }
        }
      } else {
        const err65 = { instancePath: instancePath + "/outputFields", schemaPath: "#/properties/outputFields/type", keyword: "type", params: { type: "array" }, message: "must be array" };
        if (vErrors === null) {
          vErrors = [err65];
        } else {
          vErrors.push(err65);
        }
        errors++;
      }
    }
    if (data.execution !== void 0) {
      let data22 = data.execution;
      const _errs47 = errors;
      let valid20 = true;
      const _errs48 = errors;
      if (data22 && typeof data22 == "object" && !Array.isArray(data22)) {
        let missing0;
        if (data22.leaseProfile === void 0 && (missing0 = "leaseProfile")) {
          const err66 = {};
          if (vErrors === null) {
            vErrors = [err66];
          } else {
            vErrors.push(err66);
          }
          errors++;
        }
      }
      var _valid0 = _errs48 === errors;
      errors = _errs47;
      if (vErrors !== null) {
        if (_errs47) {
          vErrors.length = _errs47;
        } else {
          vErrors = null;
        }
      }
      let ifClause0;
      if (_valid0) {
        const _errs49 = errors;
        if (data22 && typeof data22 == "object" && !Array.isArray(data22)) {
          if (data22.maxAttempts !== void 0) {
            if (1 !== data22.maxAttempts) {
              const err67 = { instancePath: instancePath + "/execution/maxAttempts", schemaPath: "#/$defs/execution/allOf/0/then/properties/maxAttempts/const", keyword: "const", params: { allowedValue: 1 }, message: "must be equal to constant" };
              if (vErrors === null) {
                vErrors = [err67];
              } else {
                vErrors.push(err67);
              }
              errors++;
            }
          }
        }
        var _valid0 = _errs49 === errors;
        valid20 = _valid0;
        if (valid20) {
          var props0 = {};
          props0.maxAttempts = true;
        }
        ifClause0 = "then";
      } else {
        const _errs51 = errors;
        if (data22 && typeof data22 == "object" && !Array.isArray(data22)) {
          if (data22.timeoutSeconds !== void 0) {
            let data24 = data22.timeoutSeconds;
            if (!(typeof data24 == "number" && (!(data24 % 1) && !isNaN(data24)) && isFinite(data24))) {
              const err68 = { instancePath: instancePath + "/execution/timeoutSeconds", schemaPath: "#/$defs/execution/allOf/0/else/properties/timeoutSeconds/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
              if (vErrors === null) {
                vErrors = [err68];
              } else {
                vErrors.push(err68);
              }
              errors++;
            }
            if (typeof data24 == "number" && isFinite(data24)) {
              if (data24 > 120 || isNaN(data24)) {
                const err69 = { instancePath: instancePath + "/execution/timeoutSeconds", schemaPath: "#/$defs/execution/allOf/0/else/properties/timeoutSeconds/maximum", keyword: "maximum", params: { comparison: "<=", limit: 120 }, message: "must be <= 120" };
                if (vErrors === null) {
                  vErrors = [err69];
                } else {
                  vErrors.push(err69);
                }
                errors++;
              }
            }
          }
        }
        var _valid0 = _errs51 === errors;
        valid20 = _valid0;
        if (valid20) {
          if (props0 !== true) {
            props0 = props0 || {};
            props0.timeoutSeconds = true;
          }
        }
        ifClause0 = "else";
      }
      if (!valid20) {
        const err70 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/allOf/0/if", keyword: "if", params: { failingKeyword: ifClause0 }, message: 'must match "' + ifClause0 + '" schema' };
        if (vErrors === null) {
          vErrors = [err70];
        } else {
          vErrors.push(err70);
        }
        errors++;
      }
      const _errs55 = errors;
      let valid23 = true;
      const _errs56 = errors;
      if (data22 && typeof data22 == "object" && !Array.isArray(data22)) {
        let missing1;
        if (data22.leaseProfile === void 0 && (missing1 = "leaseProfile")) {
          const err71 = {};
          if (vErrors === null) {
            vErrors = [err71];
          } else {
            vErrors.push(err71);
          }
          errors++;
        } else {
          if (data22.leaseProfile !== void 0) {
            if ("SSH_COMPOSE_V1" !== data22.leaseProfile) {
              const err72 = {};
              if (vErrors === null) {
                vErrors = [err72];
              } else {
                vErrors.push(err72);
              }
              errors++;
            }
          }
        }
      }
      var _valid1 = _errs56 === errors;
      errors = _errs55;
      if (vErrors !== null) {
        if (_errs55) {
          vErrors.length = _errs55;
        } else {
          vErrors = null;
        }
      }
      if (_valid1) {
        const _errs58 = errors;
        if (data22 && typeof data22 == "object" && !Array.isArray(data22)) {
          if (data22.idempotency !== void 0) {
            if ("EFFECT_KEY" !== data22.idempotency) {
              const err73 = { instancePath: instancePath + "/execution/idempotency", schemaPath: "#/$defs/execution/allOf/1/then/properties/idempotency/const", keyword: "const", params: { allowedValue: "EFFECT_KEY" }, message: "must be equal to constant" };
              if (vErrors === null) {
                vErrors = [err73];
              } else {
                vErrors.push(err73);
              }
              errors++;
            }
          }
        }
        var _valid1 = _errs58 === errors;
        valid23 = _valid1;
        if (valid23) {
          var props1 = {};
          props1.idempotency = true;
          props1.leaseProfile = true;
        }
      }
      if (!valid23) {
        const err74 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/allOf/1/if", keyword: "if", params: { failingKeyword: "then" }, message: 'must match "then" schema' };
        if (vErrors === null) {
          vErrors = [err74];
        } else {
          vErrors.push(err74);
        }
        errors++;
      }
      if (props0 !== true && props1 !== void 0) {
        if (props1 === true) {
          props0 = true;
        } else {
          props0 = props0 || {};
          Object.assign(props0, props1);
        }
      }
      if (data22 && typeof data22 == "object" && !Array.isArray(data22)) {
        if (data22.idempotency === void 0) {
          const err75 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/required", keyword: "required", params: { missingProperty: "idempotency" }, message: "must have required property 'idempotency'" };
          if (vErrors === null) {
            vErrors = [err75];
          } else {
            vErrors.push(err75);
          }
          errors++;
        }
        if (data22.timeoutSeconds === void 0) {
          const err76 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/required", keyword: "required", params: { missingProperty: "timeoutSeconds" }, message: "must have required property 'timeoutSeconds'" };
          if (vErrors === null) {
            vErrors = [err76];
          } else {
            vErrors.push(err76);
          }
          errors++;
        }
        if (data22.maxAttempts === void 0) {
          const err77 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/required", keyword: "required", params: { missingProperty: "maxAttempts" }, message: "must have required property 'maxAttempts'" };
          if (vErrors === null) {
            vErrors = [err77];
          } else {
            vErrors.push(err77);
          }
          errors++;
        }
        if (data22.retryBackoffMilliseconds === void 0) {
          const err78 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/required", keyword: "required", params: { missingProperty: "retryBackoffMilliseconds" }, message: "must have required property 'retryBackoffMilliseconds'" };
          if (vErrors === null) {
            vErrors = [err78];
          } else {
            vErrors.push(err78);
          }
          errors++;
        }
        for (const key3 in data22) {
          if (!(key3 === "leaseProfile" || key3 === "idempotency" || key3 === "timeoutSeconds" || key3 === "maxAttempts" || key3 === "retryBackoffMilliseconds")) {
            const err79 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key3 }, message: "must NOT have additional properties" };
            if (vErrors === null) {
              vErrors = [err79];
            } else {
              vErrors.push(err79);
            }
            errors++;
          }
        }
        if (data22.leaseProfile !== void 0) {
          let data27 = data22.leaseProfile;
          if (!(data27 === "SSH_COMPOSE_V1" || data27 === "MANAGED_KUBERNETES_V1")) {
            const err80 = { instancePath: instancePath + "/execution/leaseProfile", schemaPath: "#/$defs/execution/properties/leaseProfile/enum", keyword: "enum", params: { allowedValues: schema48.properties.leaseProfile.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err80];
            } else {
              vErrors.push(err80);
            }
            errors++;
          }
        }
        if (data22.idempotency !== void 0) {
          let data28 = data22.idempotency;
          if (!(data28 === "READ_ONLY" || data28 === "EFFECT_KEY" || data28 === "PROVIDER_NATIVE" || data28 === "ONE_ATTEMPT")) {
            const err81 = { instancePath: instancePath + "/execution/idempotency", schemaPath: "#/$defs/execution/properties/idempotency/enum", keyword: "enum", params: { allowedValues: schema48.properties.idempotency.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err81];
            } else {
              vErrors.push(err81);
            }
            errors++;
          }
        }
        if (data22.timeoutSeconds !== void 0) {
          let data29 = data22.timeoutSeconds;
          if (!(typeof data29 == "number" && (!(data29 % 1) && !isNaN(data29)) && isFinite(data29))) {
            const err82 = { instancePath: instancePath + "/execution/timeoutSeconds", schemaPath: "#/$defs/execution/properties/timeoutSeconds/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
            if (vErrors === null) {
              vErrors = [err82];
            } else {
              vErrors.push(err82);
            }
            errors++;
          }
          if (typeof data29 == "number" && isFinite(data29)) {
            if (data29 > 600 || isNaN(data29)) {
              const err83 = { instancePath: instancePath + "/execution/timeoutSeconds", schemaPath: "#/$defs/execution/properties/timeoutSeconds/maximum", keyword: "maximum", params: { comparison: "<=", limit: 600 }, message: "must be <= 600" };
              if (vErrors === null) {
                vErrors = [err83];
              } else {
                vErrors.push(err83);
              }
              errors++;
            }
            if (data29 < 1 || isNaN(data29)) {
              const err84 = { instancePath: instancePath + "/execution/timeoutSeconds", schemaPath: "#/$defs/execution/properties/timeoutSeconds/minimum", keyword: "minimum", params: { comparison: ">=", limit: 1 }, message: "must be >= 1" };
              if (vErrors === null) {
                vErrors = [err84];
              } else {
                vErrors.push(err84);
              }
              errors++;
            }
          }
        }
        if (data22.maxAttempts !== void 0) {
          let data30 = data22.maxAttempts;
          if (!(typeof data30 == "number" && (!(data30 % 1) && !isNaN(data30)) && isFinite(data30))) {
            const err85 = { instancePath: instancePath + "/execution/maxAttempts", schemaPath: "#/$defs/execution/properties/maxAttempts/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
            if (vErrors === null) {
              vErrors = [err85];
            } else {
              vErrors.push(err85);
            }
            errors++;
          }
          if (typeof data30 == "number" && isFinite(data30)) {
            if (data30 > 4 || isNaN(data30)) {
              const err86 = { instancePath: instancePath + "/execution/maxAttempts", schemaPath: "#/$defs/execution/properties/maxAttempts/maximum", keyword: "maximum", params: { comparison: "<=", limit: 4 }, message: "must be <= 4" };
              if (vErrors === null) {
                vErrors = [err86];
              } else {
                vErrors.push(err86);
              }
              errors++;
            }
            if (data30 < 1 || isNaN(data30)) {
              const err87 = { instancePath: instancePath + "/execution/maxAttempts", schemaPath: "#/$defs/execution/properties/maxAttempts/minimum", keyword: "minimum", params: { comparison: ">=", limit: 1 }, message: "must be >= 1" };
              if (vErrors === null) {
                vErrors = [err87];
              } else {
                vErrors.push(err87);
              }
              errors++;
            }
          }
        }
        if (data22.retryBackoffMilliseconds !== void 0) {
          let data31 = data22.retryBackoffMilliseconds;
          if (!(typeof data31 == "number" && (!(data31 % 1) && !isNaN(data31)) && isFinite(data31))) {
            const err88 = { instancePath: instancePath + "/execution/retryBackoffMilliseconds", schemaPath: "#/$defs/execution/properties/retryBackoffMilliseconds/type", keyword: "type", params: { type: "integer" }, message: "must be integer" };
            if (vErrors === null) {
              vErrors = [err88];
            } else {
              vErrors.push(err88);
            }
            errors++;
          }
          if (typeof data31 == "number" && isFinite(data31)) {
            if (data31 > 5e3 || isNaN(data31)) {
              const err89 = { instancePath: instancePath + "/execution/retryBackoffMilliseconds", schemaPath: "#/$defs/execution/properties/retryBackoffMilliseconds/maximum", keyword: "maximum", params: { comparison: "<=", limit: 5e3 }, message: "must be <= 5000" };
              if (vErrors === null) {
                vErrors = [err89];
              } else {
                vErrors.push(err89);
              }
              errors++;
            }
            if (data31 < 50 || isNaN(data31)) {
              const err90 = { instancePath: instancePath + "/execution/retryBackoffMilliseconds", schemaPath: "#/$defs/execution/properties/retryBackoffMilliseconds/minimum", keyword: "minimum", params: { comparison: ">=", limit: 50 }, message: "must be >= 50" };
              if (vErrors === null) {
                vErrors = [err90];
              } else {
                vErrors.push(err90);
              }
              errors++;
            }
          }
        }
      } else {
        const err91 = { instancePath: instancePath + "/execution", schemaPath: "#/$defs/execution/type", keyword: "type", params: { type: "object" }, message: "must be object" };
        if (vErrors === null) {
          vErrors = [err91];
        } else {
          vErrors.push(err91);
        }
        errors++;
      }
    }
    if (data.openapi !== void 0) {
      let data32 = data.openapi;
      if (data32 && typeof data32 == "object" && !Array.isArray(data32)) {
        if (data32.operationId === void 0) {
          const err92 = { instancePath: instancePath + "/openapi", schemaPath: "#/properties/openapi/required", keyword: "required", params: { missingProperty: "operationId" }, message: "must have required property 'operationId'" };
          if (vErrors === null) {
            vErrors = [err92];
          } else {
            vErrors.push(err92);
          }
          errors++;
        }
        if (data32.method === void 0) {
          const err93 = { instancePath: instancePath + "/openapi", schemaPath: "#/properties/openapi/required", keyword: "required", params: { missingProperty: "method" }, message: "must have required property 'method'" };
          if (vErrors === null) {
            vErrors = [err93];
          } else {
            vErrors.push(err93);
          }
          errors++;
        }
        if (data32.path === void 0) {
          const err94 = { instancePath: instancePath + "/openapi", schemaPath: "#/properties/openapi/required", keyword: "required", params: { missingProperty: "path" }, message: "must have required property 'path'" };
          if (vErrors === null) {
            vErrors = [err94];
          } else {
            vErrors.push(err94);
          }
          errors++;
        }
        if (data32.authScheme === void 0) {
          const err95 = { instancePath: instancePath + "/openapi", schemaPath: "#/properties/openapi/required", keyword: "required", params: { missingProperty: "authScheme" }, message: "must have required property 'authScheme'" };
          if (vErrors === null) {
            vErrors = [err95];
          } else {
            vErrors.push(err95);
          }
          errors++;
        }
        if (data32.inputSchema === void 0) {
          const err96 = { instancePath: instancePath + "/openapi", schemaPath: "#/properties/openapi/required", keyword: "required", params: { missingProperty: "inputSchema" }, message: "must have required property 'inputSchema'" };
          if (vErrors === null) {
            vErrors = [err96];
          } else {
            vErrors.push(err96);
          }
          errors++;
        }
        for (const key4 in data32) {
          if (!func3.call(schema43.properties.openapi.properties, key4)) {
            const err97 = { instancePath: instancePath + "/openapi", schemaPath: "#/properties/openapi/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key4 }, message: "must NOT have additional properties" };
            if (vErrors === null) {
              vErrors = [err97];
            } else {
              vErrors.push(err97);
            }
            errors++;
          }
        }
        if (data32.operationId !== void 0) {
          let data33 = data32.operationId;
          if (typeof data33 === "string") {
            if (func1(data33) > 128) {
              const err98 = { instancePath: instancePath + "/openapi/operationId", schemaPath: "#/properties/openapi/properties/operationId/maxLength", keyword: "maxLength", params: { limit: 128 }, message: "must NOT have more than 128 characters" };
              if (vErrors === null) {
                vErrors = [err98];
              } else {
                vErrors.push(err98);
              }
              errors++;
            }
            if (func1(data33) < 1) {
              const err99 = { instancePath: instancePath + "/openapi/operationId", schemaPath: "#/properties/openapi/properties/operationId/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
              if (vErrors === null) {
                vErrors = [err99];
              } else {
                vErrors.push(err99);
              }
              errors++;
            }
          } else {
            const err100 = { instancePath: instancePath + "/openapi/operationId", schemaPath: "#/properties/openapi/properties/operationId/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err100];
            } else {
              vErrors.push(err100);
            }
            errors++;
          }
        }
        if (data32.sourceDigest !== void 0) {
          let data34 = data32.sourceDigest;
          if (typeof data34 === "string") {
            if (!pattern20.test(data34)) {
              const err101 = { instancePath: instancePath + "/openapi/sourceDigest", schemaPath: "#/properties/openapi/properties/sourceDigest/pattern", keyword: "pattern", params: { pattern: "^[a-f0-9]{64}$" }, message: 'must match pattern "^[a-f0-9]{64}$"' };
              if (vErrors === null) {
                vErrors = [err101];
              } else {
                vErrors.push(err101);
              }
              errors++;
            }
          } else {
            const err102 = { instancePath: instancePath + "/openapi/sourceDigest", schemaPath: "#/properties/openapi/properties/sourceDigest/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err102];
            } else {
              vErrors.push(err102);
            }
            errors++;
          }
        }
        if (data32.serverOrigin !== void 0) {
          let data35 = data32.serverOrigin;
          if (typeof data35 === "string") {
            if (func1(data35) > 2048) {
              const err103 = { instancePath: instancePath + "/openapi/serverOrigin", schemaPath: "#/properties/openapi/properties/serverOrigin/maxLength", keyword: "maxLength", params: { limit: 2048 }, message: "must NOT have more than 2048 characters" };
              if (vErrors === null) {
                vErrors = [err103];
              } else {
                vErrors.push(err103);
              }
              errors++;
            }
            if (!formats0(data35)) {
              const err104 = { instancePath: instancePath + "/openapi/serverOrigin", schemaPath: "#/properties/openapi/properties/serverOrigin/format", keyword: "format", params: { format: "uri" }, message: 'must match format "uri"' };
              if (vErrors === null) {
                vErrors = [err104];
              } else {
                vErrors.push(err104);
              }
              errors++;
            }
          } else {
            const err105 = { instancePath: instancePath + "/openapi/serverOrigin", schemaPath: "#/properties/openapi/properties/serverOrigin/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err105];
            } else {
              vErrors.push(err105);
            }
            errors++;
          }
        }
        if (data32.method !== void 0) {
          let data36 = data32.method;
          if (!(data36 === "GET" || data36 === "POST" || data36 === "PUT" || data36 === "PATCH" || data36 === "DELETE")) {
            const err106 = { instancePath: instancePath + "/openapi/method", schemaPath: "#/properties/openapi/properties/method/enum", keyword: "enum", params: { allowedValues: schema43.properties.openapi.properties.method.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err106];
            } else {
              vErrors.push(err106);
            }
            errors++;
          }
        }
        if (data32.path !== void 0) {
          let data37 = data32.path;
          if (typeof data37 === "string") {
            if (func1(data37) > 512) {
              const err107 = { instancePath: instancePath + "/openapi/path", schemaPath: "#/properties/openapi/properties/path/maxLength", keyword: "maxLength", params: { limit: 512 }, message: "must NOT have more than 512 characters" };
              if (vErrors === null) {
                vErrors = [err107];
              } else {
                vErrors.push(err107);
              }
              errors++;
            }
            if (func1(data37) < 1) {
              const err108 = { instancePath: instancePath + "/openapi/path", schemaPath: "#/properties/openapi/properties/path/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
              if (vErrors === null) {
                vErrors = [err108];
              } else {
                vErrors.push(err108);
              }
              errors++;
            }
          } else {
            const err109 = { instancePath: instancePath + "/openapi/path", schemaPath: "#/properties/openapi/properties/path/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err109];
            } else {
              vErrors.push(err109);
            }
            errors++;
          }
        }
        if (data32.authScheme !== void 0) {
          let data38 = data32.authScheme;
          if (!(data38 === "NONE" || data38 === "BEARER" || data38 === "API_KEY_HEADER")) {
            const err110 = { instancePath: instancePath + "/openapi/authScheme", schemaPath: "#/properties/openapi/properties/authScheme/enum", keyword: "enum", params: { allowedValues: schema43.properties.openapi.properties.authScheme.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err110];
            } else {
              vErrors.push(err110);
            }
            errors++;
          }
        }
        if (data32.authHeader !== void 0) {
          let data39 = data32.authHeader;
          if (typeof data39 === "string") {
            if (func1(data39) > 64) {
              const err111 = { instancePath: instancePath + "/openapi/authHeader", schemaPath: "#/properties/openapi/properties/authHeader/maxLength", keyword: "maxLength", params: { limit: 64 }, message: "must NOT have more than 64 characters" };
              if (vErrors === null) {
                vErrors = [err111];
              } else {
                vErrors.push(err111);
              }
              errors++;
            }
          } else {
            const err112 = { instancePath: instancePath + "/openapi/authHeader", schemaPath: "#/properties/openapi/properties/authHeader/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err112];
            } else {
              vErrors.push(err112);
            }
            errors++;
          }
        }
        if (data32.idempotencyHeader !== void 0) {
          let data40 = data32.idempotencyHeader;
          if (typeof data40 === "string") {
            if (func1(data40) > 64) {
              const err113 = { instancePath: instancePath + "/openapi/idempotencyHeader", schemaPath: "#/properties/openapi/properties/idempotencyHeader/maxLength", keyword: "maxLength", params: { limit: 64 }, message: "must NOT have more than 64 characters" };
              if (vErrors === null) {
                vErrors = [err113];
              } else {
                vErrors.push(err113);
              }
              errors++;
            }
          } else {
            const err114 = { instancePath: instancePath + "/openapi/idempotencyHeader", schemaPath: "#/properties/openapi/properties/idempotencyHeader/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err114];
            } else {
              vErrors.push(err114);
            }
            errors++;
          }
        }
        if (data32.inputSchema !== void 0) {
          let data41 = data32.inputSchema;
          if (data41 && typeof data41 == "object" && !Array.isArray(data41)) {
          } else {
            const err115 = { instancePath: instancePath + "/openapi/inputSchema", schemaPath: "#/properties/openapi/properties/inputSchema/type", keyword: "type", params: { type: "object" }, message: "must be object" };
            if (vErrors === null) {
              vErrors = [err115];
            } else {
              vErrors.push(err115);
            }
            errors++;
          }
        }
      } else {
        const err116 = { instancePath: instancePath + "/openapi", schemaPath: "#/properties/openapi/type", keyword: "type", params: { type: "object" }, message: "must be object" };
        if (vErrors === null) {
          vErrors = [err116];
        } else {
          vErrors.push(err116);
        }
        errors++;
      }
    }
  } else {
    const err117 = { instancePath, schemaPath: "#/type", keyword: "type", params: { type: "object" }, message: "must be object" };
    if (vErrors === null) {
      vErrors = [err117];
    } else {
      vErrors.push(err117);
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
              const err12 = { instancePath: instancePath + "/metadata/key", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$"' };
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
          let data5 = data2.origin;
          if (!(data5 === "SHIPPED" || data5 === "UI" || data5 === "GIT")) {
            const err17 = { instancePath: instancePath + "/metadata/origin", schemaPath: "#/properties/metadata/properties/origin/enum", keyword: "enum", params: { allowedValues: schema31.properties.metadata.properties.origin.enum }, message: "must be equal to one of the allowed values" };
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
      const _errs16 = errors;
      let valid4 = true;
      const _errs17 = errors;
      if (data6 && typeof data6 == "object" && !Array.isArray(data6)) {
        let missing0;
        if (data6.credential === void 0 && (missing0 = "credential")) {
          const err19 = {};
          if (vErrors === null) {
            vErrors = [err19];
          } else {
            vErrors.push(err19);
          }
          errors++;
        } else {
          if (data6.credential !== void 0) {
            let data7 = data6.credential;
            const _errs18 = errors;
            if (errors === _errs18) {
              if (data7 && typeof data7 == "object" && !Array.isArray(data7)) {
                if (data7.kind !== void 0) {
                  if ("BROKER_IDENTITY" !== data7.kind) {
                    const err20 = {};
                    if (vErrors === null) {
                      vErrors = [err20];
                    } else {
                      vErrors.push(err20);
                    }
                    errors++;
                  }
                }
              } else {
                const err21 = {};
                if (vErrors === null) {
                  vErrors = [err21];
                } else {
                  vErrors.push(err21);
                }
                errors++;
              }
            }
          }
        }
      }
      var _valid0 = _errs17 === errors;
      errors = _errs16;
      if (vErrors !== null) {
        if (_errs16) {
          vErrors.length = _errs16;
        } else {
          vErrors = null;
        }
      }
      if (_valid0) {
        const _errs21 = errors;
        const _errs22 = errors;
        let valid7 = false;
        let passing0 = null;
        const _errs23 = errors;
        if (data6 && typeof data6 == "object" && !Array.isArray(data6)) {
          if (data6.adapter !== void 0) {
            if ("KUBERNETES" !== data6.adapter) {
              const err22 = { instancePath: instancePath + "/spec/adapter", schemaPath: "#/properties/spec/allOf/0/then/oneOf/0/properties/adapter/const", keyword: "const", params: { allowedValue: "KUBERNETES" }, message: "must be equal to constant" };
              if (vErrors === null) {
                vErrors = [err22];
              } else {
                vErrors.push(err22);
              }
              errors++;
            }
          }
          if (data6.credential !== void 0) {
            let data10 = data6.credential;
            if (data10 && typeof data10 == "object" && !Array.isArray(data10)) {
              if (data10.brokerPurpose !== void 0) {
                if ("KUBERNETES_TARGET" !== data10.brokerPurpose) {
                  const err23 = { instancePath: instancePath + "/spec/credential/brokerPurpose", schemaPath: "#/properties/spec/allOf/0/then/oneOf/0/properties/credential/properties/brokerPurpose/const", keyword: "const", params: { allowedValue: "KUBERNETES_TARGET" }, message: "must be equal to constant" };
                  if (vErrors === null) {
                    vErrors = [err23];
                  } else {
                    vErrors.push(err23);
                  }
                  errors++;
                }
              }
            } else {
              const err24 = { instancePath: instancePath + "/spec/credential", schemaPath: "#/properties/spec/allOf/0/then/oneOf/0/properties/credential/type", keyword: "type", params: { type: "object" }, message: "must be object" };
              if (vErrors === null) {
                vErrors = [err24];
              } else {
                vErrors.push(err24);
              }
              errors++;
            }
          }
        }
        var _valid1 = _errs23 === errors;
        if (_valid1) {
          valid7 = true;
          passing0 = 0;
          var props0 = {};
          props0.adapter = true;
          props0.credential = true;
        }
        const _errs28 = errors;
        if (data6 && typeof data6 == "object" && !Array.isArray(data6)) {
          if (data6.adapter !== void 0) {
            if ("SSH_TARGET" !== data6.adapter) {
              const err25 = { instancePath: instancePath + "/spec/adapter", schemaPath: "#/properties/spec/allOf/0/then/oneOf/1/properties/adapter/const", keyword: "const", params: { allowedValue: "SSH_TARGET" }, message: "must be equal to constant" };
              if (vErrors === null) {
                vErrors = [err25];
              } else {
                vErrors.push(err25);
              }
              errors++;
            }
          }
          if (data6.credential !== void 0) {
            let data13 = data6.credential;
            if (data13 && typeof data13 == "object" && !Array.isArray(data13)) {
              if (data13.brokerPurpose !== void 0) {
                if ("SSH_TARGET" !== data13.brokerPurpose) {
                  const err26 = { instancePath: instancePath + "/spec/credential/brokerPurpose", schemaPath: "#/properties/spec/allOf/0/then/oneOf/1/properties/credential/properties/brokerPurpose/const", keyword: "const", params: { allowedValue: "SSH_TARGET" }, message: "must be equal to constant" };
                  if (vErrors === null) {
                    vErrors = [err26];
                  } else {
                    vErrors.push(err26);
                  }
                  errors++;
                }
              }
            } else {
              const err27 = { instancePath: instancePath + "/spec/credential", schemaPath: "#/properties/spec/allOf/0/then/oneOf/1/properties/credential/type", keyword: "type", params: { type: "object" }, message: "must be object" };
              if (vErrors === null) {
                vErrors = [err27];
              } else {
                vErrors.push(err27);
              }
              errors++;
            }
          }
        }
        var _valid1 = _errs28 === errors;
        if (_valid1 && valid7) {
          valid7 = false;
          passing0 = [passing0, 1];
        } else {
          if (_valid1) {
            valid7 = true;
            passing0 = 1;
            if (props0 !== true) {
              props0 = props0 || {};
              props0.adapter = true;
              props0.credential = true;
            }
          }
        }
        if (!valid7) {
          const err28 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/allOf/0/then/oneOf", keyword: "oneOf", params: { passingSchemas: passing0 }, message: "must match exactly one schema in oneOf" };
          if (vErrors === null) {
            vErrors = [err28];
          } else {
            vErrors.push(err28);
          }
          errors++;
        } else {
          errors = _errs22;
          if (vErrors !== null) {
            if (_errs22) {
              vErrors.length = _errs22;
            } else {
              vErrors = null;
            }
          }
        }
        var _valid0 = _errs21 === errors;
        valid4 = _valid0;
        if (valid4) {
          if (props0 !== true) {
            props0 = props0 || {};
            props0.credential = true;
          }
        }
      }
      if (!valid4) {
        const err29 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/allOf/0/if", keyword: "if", params: { failingKeyword: "then" }, message: 'must match "then" schema' };
        if (vErrors === null) {
          vErrors = [err29];
        } else {
          vErrors.push(err29);
        }
        errors++;
      }
      const _errs34 = errors;
      let valid12 = true;
      const _errs35 = errors;
      if (data6 && typeof data6 == "object" && !Array.isArray(data6)) {
        let missing1;
        if (data6.adapter === void 0 && (missing1 = "adapter")) {
          const err30 = {};
          if (vErrors === null) {
            vErrors = [err30];
          } else {
            vErrors.push(err30);
          }
          errors++;
        } else {
          if (data6.adapter !== void 0) {
            if ("KUBERNETES" !== data6.adapter) {
              const err31 = {};
              if (vErrors === null) {
                vErrors = [err31];
              } else {
                vErrors.push(err31);
              }
              errors++;
            }
          }
        }
      }
      var _valid2 = _errs35 === errors;
      errors = _errs34;
      if (vErrors !== null) {
        if (_errs34) {
          vErrors.length = _errs34;
        } else {
          vErrors = null;
        }
      }
      if (_valid2) {
        const _errs37 = errors;
        if (data6 && typeof data6 == "object" && !Array.isArray(data6)) {
          if (data6.credential === void 0) {
            const err32 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/allOf/1/then/required", keyword: "required", params: { missingProperty: "credential" }, message: "must have required property 'credential'" };
            if (vErrors === null) {
              vErrors = [err32];
            } else {
              vErrors.push(err32);
            }
            errors++;
          }
          if (data6.credential !== void 0) {
            let data16 = data6.credential;
            if (data16 && typeof data16 == "object" && !Array.isArray(data16)) {
              if (data16.kind === void 0) {
                const err33 = { instancePath: instancePath + "/spec/credential", schemaPath: "#/properties/spec/allOf/1/then/properties/credential/required", keyword: "required", params: { missingProperty: "kind" }, message: "must have required property 'kind'" };
                if (vErrors === null) {
                  vErrors = [err33];
                } else {
                  vErrors.push(err33);
                }
                errors++;
              }
              if (data16.brokerPurpose === void 0) {
                const err34 = { instancePath: instancePath + "/spec/credential", schemaPath: "#/properties/spec/allOf/1/then/properties/credential/required", keyword: "required", params: { missingProperty: "brokerPurpose" }, message: "must have required property 'brokerPurpose'" };
                if (vErrors === null) {
                  vErrors = [err34];
                } else {
                  vErrors.push(err34);
                }
                errors++;
              }
              if (data16.kind !== void 0) {
                if ("BROKER_IDENTITY" !== data16.kind) {
                  const err35 = { instancePath: instancePath + "/spec/credential/kind", schemaPath: "#/properties/spec/allOf/1/then/properties/credential/properties/kind/const", keyword: "const", params: { allowedValue: "BROKER_IDENTITY" }, message: "must be equal to constant" };
                  if (vErrors === null) {
                    vErrors = [err35];
                  } else {
                    vErrors.push(err35);
                  }
                  errors++;
                }
              }
              if (data16.brokerPurpose !== void 0) {
                if ("KUBERNETES_TARGET" !== data16.brokerPurpose) {
                  const err36 = { instancePath: instancePath + "/spec/credential/brokerPurpose", schemaPath: "#/properties/spec/allOf/1/then/properties/credential/properties/brokerPurpose/const", keyword: "const", params: { allowedValue: "KUBERNETES_TARGET" }, message: "must be equal to constant" };
                  if (vErrors === null) {
                    vErrors = [err36];
                  } else {
                    vErrors.push(err36);
                  }
                  errors++;
                }
              }
            } else {
              const err37 = { instancePath: instancePath + "/spec/credential", schemaPath: "#/properties/spec/allOf/1/then/properties/credential/type", keyword: "type", params: { type: "object" }, message: "must be object" };
              if (vErrors === null) {
                vErrors = [err37];
              } else {
                vErrors.push(err37);
              }
              errors++;
            }
          }
        }
        var _valid2 = _errs37 === errors;
        valid12 = _valid2;
        if (valid12) {
          var props1 = {};
          props1.credential = true;
          props1.adapter = true;
        }
      }
      if (!valid12) {
        const err38 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/allOf/1/if", keyword: "if", params: { failingKeyword: "then" }, message: 'must match "then" schema' };
        if (vErrors === null) {
          vErrors = [err38];
        } else {
          vErrors.push(err38);
        }
        errors++;
      }
      if (props0 !== true && props1 !== void 0) {
        if (props1 === true) {
          props0 = true;
        } else {
          props0 = props0 || {};
          Object.assign(props0, props1);
        }
      }
      const _errs43 = errors;
      let valid16 = true;
      const _errs44 = errors;
      if (data6 && typeof data6 == "object" && !Array.isArray(data6)) {
        let missing2;
        if (data6.adapter === void 0 && (missing2 = "adapter")) {
          const err39 = {};
          if (vErrors === null) {
            vErrors = [err39];
          } else {
            vErrors.push(err39);
          }
          errors++;
        } else {
          if (data6.adapter !== void 0) {
            if ("SSH_TARGET" !== data6.adapter) {
              const err40 = {};
              if (vErrors === null) {
                vErrors = [err40];
              } else {
                vErrors.push(err40);
              }
              errors++;
            }
          }
        }
      }
      var _valid3 = _errs44 === errors;
      errors = _errs43;
      if (vErrors !== null) {
        if (_errs43) {
          vErrors.length = _errs43;
        } else {
          vErrors = null;
        }
      }
      if (_valid3) {
        const _errs46 = errors;
        if (data6 && typeof data6 == "object" && !Array.isArray(data6)) {
          if (data6.credential === void 0) {
            const err41 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/allOf/2/then/required", keyword: "required", params: { missingProperty: "credential" }, message: "must have required property 'credential'" };
            if (vErrors === null) {
              vErrors = [err41];
            } else {
              vErrors.push(err41);
            }
            errors++;
          }
          if (data6.credential !== void 0) {
            let data20 = data6.credential;
            if (data20 && typeof data20 == "object" && !Array.isArray(data20)) {
              if (data20.kind === void 0) {
                const err42 = { instancePath: instancePath + "/spec/credential", schemaPath: "#/properties/spec/allOf/2/then/properties/credential/required", keyword: "required", params: { missingProperty: "kind" }, message: "must have required property 'kind'" };
                if (vErrors === null) {
                  vErrors = [err42];
                } else {
                  vErrors.push(err42);
                }
                errors++;
              }
              if (data20.brokerPurpose === void 0) {
                const err43 = { instancePath: instancePath + "/spec/credential", schemaPath: "#/properties/spec/allOf/2/then/properties/credential/required", keyword: "required", params: { missingProperty: "brokerPurpose" }, message: "must have required property 'brokerPurpose'" };
                if (vErrors === null) {
                  vErrors = [err43];
                } else {
                  vErrors.push(err43);
                }
                errors++;
              }
              if (data20.kind !== void 0) {
                if ("BROKER_IDENTITY" !== data20.kind) {
                  const err44 = { instancePath: instancePath + "/spec/credential/kind", schemaPath: "#/properties/spec/allOf/2/then/properties/credential/properties/kind/const", keyword: "const", params: { allowedValue: "BROKER_IDENTITY" }, message: "must be equal to constant" };
                  if (vErrors === null) {
                    vErrors = [err44];
                  } else {
                    vErrors.push(err44);
                  }
                  errors++;
                }
              }
              if (data20.brokerPurpose !== void 0) {
                if ("SSH_TARGET" !== data20.brokerPurpose) {
                  const err45 = { instancePath: instancePath + "/spec/credential/brokerPurpose", schemaPath: "#/properties/spec/allOf/2/then/properties/credential/properties/brokerPurpose/const", keyword: "const", params: { allowedValue: "SSH_TARGET" }, message: "must be equal to constant" };
                  if (vErrors === null) {
                    vErrors = [err45];
                  } else {
                    vErrors.push(err45);
                  }
                  errors++;
                }
              }
            } else {
              const err46 = { instancePath: instancePath + "/spec/credential", schemaPath: "#/properties/spec/allOf/2/then/properties/credential/type", keyword: "type", params: { type: "object" }, message: "must be object" };
              if (vErrors === null) {
                vErrors = [err46];
              } else {
                vErrors.push(err46);
              }
              errors++;
            }
          }
        }
        var _valid3 = _errs46 === errors;
        valid16 = _valid3;
        if (valid16) {
          var props2 = {};
          props2.credential = true;
          props2.adapter = true;
        }
      }
      if (!valid16) {
        const err47 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/allOf/2/if", keyword: "if", params: { failingKeyword: "then" }, message: 'must match "then" schema' };
        if (vErrors === null) {
          vErrors = [err47];
        } else {
          vErrors.push(err47);
        }
        errors++;
      }
      if (props0 !== true && props2 !== void 0) {
        if (props2 === true) {
          props0 = true;
        } else {
          props0 = props0 || {};
          Object.assign(props0, props2);
        }
      }
      if (data6 && typeof data6 == "object" && !Array.isArray(data6)) {
        if (data6.name === void 0) {
          const err48 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "name" }, message: "must have required property 'name'" };
          if (vErrors === null) {
            vErrors = [err48];
          } else {
            vErrors.push(err48);
          }
          errors++;
        }
        if (data6.description === void 0) {
          const err49 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "description" }, message: "must have required property 'description'" };
          if (vErrors === null) {
            vErrors = [err49];
          } else {
            vErrors.push(err49);
          }
          errors++;
        }
        if (data6.category === void 0) {
          const err50 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "category" }, message: "must have required property 'category'" };
          if (vErrors === null) {
            vErrors = [err50];
          } else {
            vErrors.push(err50);
          }
          errors++;
        }
        if (data6.adapter === void 0) {
          const err51 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "adapter" }, message: "must have required property 'adapter'" };
          if (vErrors === null) {
            vErrors = [err51];
          } else {
            vErrors.push(err51);
          }
          errors++;
        }
        if (data6.adapterOwner === void 0) {
          const err52 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "adapterOwner" }, message: "must have required property 'adapterOwner'" };
          if (vErrors === null) {
            vErrors = [err52];
          } else {
            vErrors.push(err52);
          }
          errors++;
        }
        if (data6.executionRoute === void 0) {
          const err53 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "executionRoute" }, message: "must have required property 'executionRoute'" };
          if (vErrors === null) {
            vErrors = [err53];
          } else {
            vErrors.push(err53);
          }
          errors++;
        }
        if (data6.readiness === void 0) {
          const err54 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "readiness" }, message: "must have required property 'readiness'" };
          if (vErrors === null) {
            vErrors = [err54];
          } else {
            vErrors.push(err54);
          }
          errors++;
        }
        if (data6.configurationFields === void 0) {
          const err55 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "configurationFields" }, message: "must have required property 'configurationFields'" };
          if (vErrors === null) {
            vErrors = [err55];
          } else {
            vErrors.push(err55);
          }
          errors++;
        }
        if (data6.networkDestinations === void 0) {
          const err56 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "networkDestinations" }, message: "must have required property 'networkDestinations'" };
          if (vErrors === null) {
            vErrors = [err56];
          } else {
            vErrors.push(err56);
          }
          errors++;
        }
        if (data6.healthCheck === void 0) {
          const err57 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "healthCheck" }, message: "must have required property 'healthCheck'" };
          if (vErrors === null) {
            vErrors = [err57];
          } else {
            vErrors.push(err57);
          }
          errors++;
        }
        if (data6.capabilities === void 0) {
          const err58 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/required", keyword: "required", params: { missingProperty: "capabilities" }, message: "must have required property 'capabilities'" };
          if (vErrors === null) {
            vErrors = [err58];
          } else {
            vErrors.push(err58);
          }
          errors++;
        }
        for (const key2 in data6) {
          if (!func3.call(schema31.properties.spec.properties, key2)) {
            const err59 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/additionalProperties", keyword: "additionalProperties", params: { additionalProperty: key2 }, message: "must NOT have additional properties" };
            if (vErrors === null) {
              vErrors = [err59];
            } else {
              vErrors.push(err59);
            }
            errors++;
          }
        }
        if (data6.name !== void 0) {
          let data23 = data6.name;
          if (typeof data23 === "string") {
            if (func1(data23) > 120) {
              const err60 = { instancePath: instancePath + "/spec/name", schemaPath: "#/properties/spec/properties/name/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
              if (vErrors === null) {
                vErrors = [err60];
              } else {
                vErrors.push(err60);
              }
              errors++;
            }
            if (func1(data23) < 1) {
              const err61 = { instancePath: instancePath + "/spec/name", schemaPath: "#/properties/spec/properties/name/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
              if (vErrors === null) {
                vErrors = [err61];
              } else {
                vErrors.push(err61);
              }
              errors++;
            }
          } else {
            const err62 = { instancePath: instancePath + "/spec/name", schemaPath: "#/properties/spec/properties/name/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err62];
            } else {
              vErrors.push(err62);
            }
            errors++;
          }
        }
        if (data6.description !== void 0) {
          let data24 = data6.description;
          if (typeof data24 === "string") {
            if (func1(data24) > 500) {
              const err63 = { instancePath: instancePath + "/spec/description", schemaPath: "#/properties/spec/properties/description/maxLength", keyword: "maxLength", params: { limit: 500 }, message: "must NOT have more than 500 characters" };
              if (vErrors === null) {
                vErrors = [err63];
              } else {
                vErrors.push(err63);
              }
              errors++;
            }
            if (func1(data24) < 1) {
              const err64 = { instancePath: instancePath + "/spec/description", schemaPath: "#/properties/spec/properties/description/minLength", keyword: "minLength", params: { limit: 1 }, message: "must NOT have fewer than 1 characters" };
              if (vErrors === null) {
                vErrors = [err64];
              } else {
                vErrors.push(err64);
              }
              errors++;
            }
          } else {
            const err65 = { instancePath: instancePath + "/spec/description", schemaPath: "#/properties/spec/properties/description/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err65];
            } else {
              vErrors.push(err65);
            }
            errors++;
          }
        }
        if (data6.category !== void 0) {
          let data25 = data6.category;
          if (typeof data25 === "string") {
            if (func1(data25) > 120) {
              const err66 = { instancePath: instancePath + "/spec/category", schemaPath: "#/$defs/key/maxLength", keyword: "maxLength", params: { limit: 120 }, message: "must NOT have more than 120 characters" };
              if (vErrors === null) {
                vErrors = [err66];
              } else {
                vErrors.push(err66);
              }
              errors++;
            }
            if (!pattern4.test(data25)) {
              const err67 = { instancePath: instancePath + "/spec/category", schemaPath: "#/$defs/key/pattern", keyword: "pattern", params: { pattern: "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$" }, message: 'must match pattern "^[a-z][a-z0-9]*(?:[._\\-][a-z0-9]+)*$"' };
              if (vErrors === null) {
                vErrors = [err67];
              } else {
                vErrors.push(err67);
              }
              errors++;
            }
          } else {
            const err68 = { instancePath: instancePath + "/spec/category", schemaPath: "#/$defs/key/type", keyword: "type", params: { type: "string" }, message: "must be string" };
            if (vErrors === null) {
              vErrors = [err68];
            } else {
              vErrors.push(err68);
            }
            errors++;
          }
        }
        if (data6.adapter !== void 0) {
          let data26 = data6.adapter;
          if (!(data26 === "SYNTHETIC_HTTP" || data26 === "GITHUB" || data26 === "GITLAB" || data26 === "JIRA" || data26 === "CONFLUENCE" || data26 === "EMAIL_HTTPS" || data26 === "HTTPS_JSON_READ" || data26 === "OPENAPI_MCP" || data26 === "MATTERMOST_INTERACTION" || data26 === "TELEGRAM_INTERACTION" || data26 === "CONTEXT7" || data26 === "SENTRY" || data26 === "OBSERVABILITY" || data26 === "SSH_COMPOSE" || data26 === "SSH_TARGET" || data26 === "KUBERNETES")) {
            const err69 = { instancePath: instancePath + "/spec/adapter", schemaPath: "#/properties/spec/properties/adapter/enum", keyword: "enum", params: { allowedValues: schema31.properties.spec.properties.adapter.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err69];
            } else {
              vErrors.push(err69);
            }
            errors++;
          }
        }
        if (data6.adapterOwner !== void 0) {
          let data27 = data6.adapterOwner;
          if (!(data27 === "integration-gateway" || data27 === "interaction-gateway")) {
            const err70 = { instancePath: instancePath + "/spec/adapterOwner", schemaPath: "#/properties/spec/properties/adapterOwner/enum", keyword: "enum", params: { allowedValues: schema31.properties.spec.properties.adapterOwner.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err70];
            } else {
              vErrors.push(err70);
            }
            errors++;
          }
        }
        if (data6.executionRoute !== void 0) {
          let data28 = data6.executionRoute;
          if (!(data28 === "MANAGED_MCP" || data28 === "INTERACTION")) {
            const err71 = { instancePath: instancePath + "/spec/executionRoute", schemaPath: "#/properties/spec/properties/executionRoute/enum", keyword: "enum", params: { allowedValues: schema31.properties.spec.properties.executionRoute.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err71];
            } else {
              vErrors.push(err71);
            }
            errors++;
          }
        }
        if (data6.readiness !== void 0) {
          let data29 = data6.readiness;
          if (!(data29 === "READY" || data29 === "NOT_READY")) {
            const err72 = { instancePath: instancePath + "/spec/readiness", schemaPath: "#/properties/spec/properties/readiness/enum", keyword: "enum", params: { allowedValues: schema31.properties.spec.properties.readiness.enum }, message: "must be equal to one of the allowed values" };
            if (vErrors === null) {
              vErrors = [err72];
            } else {
              vErrors.push(err72);
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
          let data31 = data6.configurationFields;
          if (Array.isArray(data31)) {
            if (data31.length > 24) {
              const err73 = { instancePath: instancePath + "/spec/configurationFields", schemaPath: "#/properties/spec/properties/configurationFields/maxItems", keyword: "maxItems", params: { limit: 24 }, message: "must NOT have more than 24 items" };
              if (vErrors === null) {
                vErrors = [err73];
              } else {
                vErrors.push(err73);
              }
              errors++;
            }
            const len0 = data31.length;
            for (let i0 = 0; i0 < len0; i0++) {
              let data32 = data31[i0];
              if (!validate23(data32, { instancePath: instancePath + "/spec/configurationFields/" + i0, parentData: data31, parentDataProperty: i0, rootData, dynamicAnchors })) {
                vErrors = vErrors === null ? validate23.errors : vErrors.concat(validate23.errors);
                errors = vErrors.length;
              }
              if (data32 && typeof data32 == "object" && !Array.isArray(data32)) {
                if (data32.allowEmpty !== void 0) {
                  if (false !== data32.allowEmpty) {
                    const err74 = { instancePath: instancePath + "/spec/configurationFields/" + i0 + "/allowEmpty", schemaPath: "#/properties/spec/properties/configurationFields/items/properties/allowEmpty/const", keyword: "const", params: { allowedValue: false }, message: "must be equal to constant" };
                    if (vErrors === null) {
                      vErrors = [err74];
                    } else {
                      vErrors.push(err74);
                    }
                    errors++;
                  }
                }
              } else {
                const err75 = { instancePath: instancePath + "/spec/configurationFields/" + i0, schemaPath: "#/properties/spec/properties/configurationFields/items/type", keyword: "type", params: { type: "object" }, message: "must be object" };
                if (vErrors === null) {
                  vErrors = [err75];
                } else {
                  vErrors.push(err75);
                }
                errors++;
              }
            }
          } else {
            const err76 = { instancePath: instancePath + "/spec/configurationFields", schemaPath: "#/properties/spec/properties/configurationFields/type", keyword: "type", params: { type: "array" }, message: "must be array" };
            if (vErrors === null) {
              vErrors = [err76];
            } else {
              vErrors.push(err76);
            }
            errors++;
          }
        }
        if (data6.networkDestinations !== void 0) {
          let data34 = data6.networkDestinations;
          if (Array.isArray(data34)) {
            if (data34.length > 16) {
              const err77 = { instancePath: instancePath + "/spec/networkDestinations", schemaPath: "#/properties/spec/properties/networkDestinations/maxItems", keyword: "maxItems", params: { limit: 16 }, message: "must NOT have more than 16 items" };
              if (vErrors === null) {
                vErrors = [err77];
              } else {
                vErrors.push(err77);
              }
              errors++;
            }
            if (data34.length < 1) {
              const err78 = { instancePath: instancePath + "/spec/networkDestinations", schemaPath: "#/properties/spec/properties/networkDestinations/minItems", keyword: "minItems", params: { limit: 1 }, message: "must NOT have fewer than 1 items" };
              if (vErrors === null) {
                vErrors = [err78];
              } else {
                vErrors.push(err78);
              }
              errors++;
            }
            const len1 = data34.length;
            for (let i1 = 0; i1 < len1; i1++) {
              if (!validate25(data34[i1], { instancePath: instancePath + "/spec/networkDestinations/" + i1, parentData: data34, parentDataProperty: i1, rootData, dynamicAnchors })) {
                vErrors = vErrors === null ? validate25.errors : vErrors.concat(validate25.errors);
                errors = vErrors.length;
              }
            }
          } else {
            const err79 = { instancePath: instancePath + "/spec/networkDestinations", schemaPath: "#/properties/spec/properties/networkDestinations/type", keyword: "type", params: { type: "array" }, message: "must be array" };
            if (vErrors === null) {
              vErrors = [err79];
            } else {
              vErrors.push(err79);
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
          let data37 = data6.capabilities;
          if (Array.isArray(data37)) {
            if (data37.length > 64) {
              const err80 = { instancePath: instancePath + "/spec/capabilities", schemaPath: "#/properties/spec/properties/capabilities/maxItems", keyword: "maxItems", params: { limit: 64 }, message: "must NOT have more than 64 items" };
              if (vErrors === null) {
                vErrors = [err80];
              } else {
                vErrors.push(err80);
              }
              errors++;
            }
            if (data37.length < 1) {
              const err81 = { instancePath: instancePath + "/spec/capabilities", schemaPath: "#/properties/spec/properties/capabilities/minItems", keyword: "minItems", params: { limit: 1 }, message: "must NOT have fewer than 1 items" };
              if (vErrors === null) {
                vErrors = [err81];
              } else {
                vErrors.push(err81);
              }
              errors++;
            }
            const len2 = data37.length;
            for (let i2 = 0; i2 < len2; i2++) {
              if (!validate29(data37[i2], { instancePath: instancePath + "/spec/capabilities/" + i2, parentData: data37, parentDataProperty: i2, rootData, dynamicAnchors })) {
                vErrors = vErrors === null ? validate29.errors : vErrors.concat(validate29.errors);
                errors = vErrors.length;
              }
            }
          } else {
            const err82 = { instancePath: instancePath + "/spec/capabilities", schemaPath: "#/properties/spec/properties/capabilities/type", keyword: "type", params: { type: "array" }, message: "must be array" };
            if (vErrors === null) {
              vErrors = [err82];
            } else {
              vErrors.push(err82);
            }
            errors++;
          }
        }
      } else {
        const err83 = { instancePath: instancePath + "/spec", schemaPath: "#/properties/spec/type", keyword: "type", params: { type: "object" }, message: "must be object" };
        if (vErrors === null) {
          vErrors = [err83];
        } else {
          vErrors.push(err83);
        }
        errors++;
      }
    }
  } else {
    const err84 = { instancePath, schemaPath: "#/type", keyword: "type", params: { type: "object" }, message: "must be object" };
    if (vErrors === null) {
      vErrors = [err84];
    } else {
      vErrors.push(err84);
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

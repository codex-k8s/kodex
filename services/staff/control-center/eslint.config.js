import eslint from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";
import vue from "eslint-plugin-vue";

export default tseslint.config(
  { ignores: ["dist/**", "dist-synthetic/**", "src/shared/api/generated/**"] },
  eslint.configs.recommended,
  ...tseslint.configs.strictTypeChecked,
  ...vue.configs["flat/essential"],
  {
    files: ["**/*.{ts,vue}"],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
        projectService: true,
        extraFileExtensions: [".vue"],
      },
      globals: globals.browser,
    },
    rules: {
      "vue/multi-word-component-names": "off",
      "@typescript-eslint/no-confusing-void-expression": "off",
    },
  },
  {
    files: ["**/*.{js,mjs}"],
    ...tseslint.configs.disableTypeChecked,
  },
  {
    files: ["tools/**/*.{js,mjs}"],
    languageOptions: { globals: globals.node },
  },
  {
    files: ["public/sw.js"],
    languageOptions: { globals: globals.serviceworker },
  },
);

# Воспроизводимые npm-инструменты runner

`package.json` и `package-lock.json` закрепляют прежний набор CLI полного
runner. Зависимости устанавливаются через `npm ci --ignore-scripts`: install
hooks и загрузка отдельного браузера не выполняются. Playwright использует
системный Chromium через `PLAYWRIGHT_MCP_EXECUTABLE_PATH`.

Опубликованные npm 12.2.0 и 11.21.0 содержат bundled зависимости, на которые
root overrides не действуют. Поэтому npm CLI строится отдельным проектом
`npm-cli/`: `npm-source.json` закрепляет официальный tarball и sha512,
`install.mjs` проверяет digest и пути до записи. Upstream source копируется
в свежий каталог без `node_modules` и upstream lock/shrinkwrap. Его зависимости
устанавливаются по собственному manifest/lock с security overrides.
Исходники CLI не переписываются; direct dependency `tar` отдельно закреплена
в версии, удовлетворяющей upstream range.

Перед публикацией относительных CLI/module symlinks проверяется фактическое
дерево обоих проектов, включая версии security-зависимостей. Прежний npm
удаляется штатным `npm uninstall --global npm --prefix /usr/local` новой CLI;
это отдельный image-build шаг, не операция над npm на хосте. Yarn и все
прежние CLI остаются доступны. Изменения всех входов входят в source fingerprint
runner, поскольку лежат внутри `services/jobs/agent-runner`.

Герметичная проверка без Docker, registry и кластера:

```sh
npm test --prefix services/jobs/agent-runner/npm-toolchain
```

Также доступен `node --test services/jobs/agent-runner/npm-toolchain/install.test.mjs`.
Проверка version/help в disposable каталоге не заменяет сборку образа, canonical
inventory всех инструментов и фактический admission. Policy и vulnerability
exceptions здесь не изменяются.

Документы npm проверены через Context7 `/npm/cli`: root overrides, bundled
dependencies и ограничения `npm ci` для global установки. Источники:
[package.json](https://docs.npmjs.com/cli/v11/configuring-npm/package-json/),
[npm ci](https://docs.npmjs.com/cli/v11/commands/npm-ci/),
[официальный npm registry](https://registry.npmjs.org/npm/12.2.0).

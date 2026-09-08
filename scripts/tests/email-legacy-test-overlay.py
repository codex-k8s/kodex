#!/usr/bin/env python3
"""Узкий test overlay: старый CP/SQL остаётся побайтово равным exact Git revision."""
import hashlib
import pathlib
import subprocess
import sys
repository, target, revision = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), sys.argv[3]
assert revision == 'd21a024dbf03fca13b20f0cb30d858d743729ed7'
path = target / 'services/internal/control-plane/internal/repository/postgres/platform/email_authorization_component_test.go'
text = path.read_text()
for marker, addition in [
    ('\tbinding := entity.EmailExecutionBinding{ConnectionTestRef:', '\ttestLegacyEmailGatewayClaim(t, health, "health")\n'),
    ('\tclaim := claims[0]\n', '\ttestLegacyEmailGatewayClaim(t, claim, "invocation")\n'),
]:
    assert text.count(marker) == 1, 'legacy fixture anchor changed'
    text = text.replace(marker, addition + marker if marker.startswith('\tbinding') else marker + addition)
path.write_text(text)
(path.parent / 'email_legacy_capture_test.go').write_bytes((repository / 'scripts/tests/fixtures/email-legacy/capture_test.go').read_bytes())
(path.parent / 'email_legacy_managed_test.go').write_bytes((repository / 'scripts/tests/fixtures/email-legacy/managed_test.go').read_bytes())
configuration = path.parent / 'email_configuration_component_test.go'
body = configuration.read_text()
marker = '\t\ttestEmailProducer(t, ctx, repository, service, owner, *connection.Connection, config)\n\t})'
assert body.count(marker) == 1, 'legacy managed fixture anchor changed'
body = body.replace(marker, marker + '\n\tfor _, origin := range []string{"UI", "GIT"} { t.Run("legacy managed producer "+origin, func(t *testing.T) { testLegacyManagedEmailProducer(t, ctx, repository, service, owner, *connection.Connection, config, origin) }) }')
configuration.write_text(body)
harness = target / 'scripts/tests/control-plane-postgres-test.sh'
harness.write_text(harness.read_text().replace('go run ./cmd/cli', 'go run -p 2 ./cmd/cli').replace('go test -count', 'go test -p 2 -count').replace('go test -v', 'go test -p 2 -v'))
# Ни одного production overlay. Проверяется всё дерево, не только SQL/producer.
allowed = {'services/internal/control-plane/internal/repository/postgres/platform/email_configuration_component_test.go', 'services/internal/control-plane/internal/repository/postgres/platform/email_authorization_component_test.go', 'scripts/tests/control-plane-postgres-test.sh'}
rows = subprocess.check_output(['git', '-C', str(repository), 'ls-tree', '-rz', revision]).split(b'\0')
checked = 0
for row in rows:
    if not row:
        continue
    header, name = row.decode().split('\t', 1)
    if name in allowed:
        continue
    mode, kind, oid = header.split()
    if kind != 'blob':
        continue
    actual = (target / name).read_bytes()
    object_hash = hashlib.sha1(b'blob ' + str(len(actual)).encode() + b'\0' + actual).hexdigest()
    assert object_hash == oid, 'legacy production tree changed'
    checked += 1
print(f'Legacy production source verified: revision={revision} unchanged_files={checked}')

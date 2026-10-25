"""Общая запись синтетических tar members для проверок platform OCI."""

import io
import tarfile


def write_tar_members(path, members):
    """Записывает bytes в порядке mapping с прежними TarInfo defaults.

    Проверка OCI, chmod архива и receipt остаются у вызывающей оснастки.
    Файлы с диска в members не добавляются, извлечения архива здесь нет.
    """
    with tarfile.open(path, "w") as archive:
        for name, value in members.items():
            member = tarfile.TarInfo(name)
            member.size = len(value)
            archive.addfile(member, io.BytesIO(value))


def assert_preserved_archive(test_case, result, archive, output):
    """Проверяет успешный verifier, исходные байты и закрытый режим результата.

    Verifier уже выполнен вызывающим тестом с его собственными аргументами;
    helper сохраняет порядок assertions и не создаёт новый процесс.
    """
    test_case.assertEqual(result.returncode, 0, result.stderr)
    test_case.assertEqual(output.read_bytes(), archive.read_bytes())
    test_case.assertEqual(output.stat().st_mode & 0o777, 0o400)

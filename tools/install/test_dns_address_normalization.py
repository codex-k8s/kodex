"""Отсутствующее семейство DNS не превращается в пустой адрес для HTTP probe."""
from pathlib import Path
import subprocess
import unittest


class DNSAddresses(unittest.TestCase):
    def test_absent_family_produces_zero_mapfile_entries(self):
        script = Path(__file__).with_name('dns-address-normalization.sh')
        for family, raw, expected in [('6', '', '0'), ('6', '192.0.2.1\n', '0'),
                                      ('4', '192.0.2.1\n192.0.2.1\n', '1'),
                                      ('6', '2001:db8::1\n', '1')]:
            result = subprocess.run(['bash', '-c',
                'source "$1"; mapfile -t values < <(canonical_dns_addresses "$2"); printf "%s" "${#values[@]}"',
                'test', str(script), family], input=raw, capture_output=True, text=True, check=True, timeout=5)
            self.assertEqual(result.stdout, expected)


if __name__ == '__main__':unittest.main()

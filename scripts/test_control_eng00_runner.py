import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('runner', Path(__file__).with_name('test-control-eng00.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class IsolationGuardTest(unittest.TestCase):
    def test_local_explicit_pair(self):
        dsn = 'postgres://admin@localhost:5432/artex_research_test_control'
        self.assertEqual(runner.isolated_dsn({'ARTEX_PG_DSN': dsn, 'ARTEX_TEST_PG_DSN': dsn}).path,
                         '/artex_research_test_control')

    def test_rejects_fallback_remote_production_and_overrides(self):
        for dsn in ('', 'postgres://admin@remote/artex_research_test',
                    'postgres://admin@localhost/artex',
                    'postgres://admin@localhost/artex_research_test?host=remote',
                    'postgres://admin@localhost/artex_research_test#fragment',
                    'postgres://admin:secret@localhost/artex_research_test'):
            with self.subTest(dsn=dsn), self.assertRaises(ValueError):
                runner.isolated_dsn({'ARTEX_PG_DSN': dsn, 'ARTEX_TEST_PG_DSN': dsn})
        with self.assertRaises(ValueError):
            runner.isolated_dsn({'ARTEX_TEST_PG_DSN': 'postgres://admin@localhost/artex_research_test'})
        with self.assertRaises(ValueError):
            runner.isolated_dsn({'ARTEX_PG_DSN': 'a', 'ARTEX_TEST_PG_DSN': 'b'})


if __name__ == '__main__':
    unittest.main(verbosity=2)

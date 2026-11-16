"""Tests for doxygen_go_filter.py. Run: python -m unittest tools/test_doxygen_go_filter.py"""
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))


def run_filter(source):
    with tempfile.NamedTemporaryFile('w', suffix='.go', delete=False, encoding='utf-8', newline='') as f:
        f.write(source)
    try:
        out = subprocess.run([sys.executable, os.path.join(HERE, 'doxygen_go_filter.py'), f.name],
                             capture_output=True, check=True)
        return out.stdout.decode('utf-8')
    finally:
        os.unlink(f.name)


class FilterTest(unittest.TestCase):
    def test_function_with_grouped_params_and_two_returns(self):
        out = run_filter('// @brief Split.\n//\n// @param a First.\nfunc split(a, b int, c string) (x []byte, err error) {\n}\n')
        self.assertIn('tuple<byte[], error> split(int a, int b, string c);', out)
        self.assertIn('@brief Split.', out)

    def test_func_typed_parameter_does_not_end_the_parameter_list(self):
        out = run_filter('// @brief Serve.\nfunc serve(s session, onTouch func(x, y int), remote string) {\n}\n')
        self.assertIn('void serve(session s, callback onTouch, string remote);', out)

    def test_method_is_nested_in_its_type(self):
        out = run_filter('// @brief Type.\ntype T struct {\n}\n\n// @brief Method.\nfunc (t *T) run(n int) error {\n}\n')
        type_start = out.index('struct T {')
        self.assertLess(type_start, out.index('error run(int n);'))
        self.assertTrue(out.rstrip().endswith('};'))

    def test_comment_without_brief_is_dropped(self):
        out = run_filter('// plain comment\nfunc f() {\n}\n')
        self.assertNotIn('plain comment', out)
        self.assertNotIn('f(', out)

    def test_crlf_input(self):
        out = run_filter('// @brief Crlf.\r\nfunc f() {\r\n}\r\n')
        self.assertIn('void f();', out)


if __name__ == '__main__':
    unittest.main()

#!/usr/bin/env python3
import json
import unittest
from check_go_test_json import validate
from preflight import parse_version

P='github.com/Autumn-27/artex/db'
REQ=[{'package':P,'name':'TestControlExample'}]
def logs(*events):return '\n'.join(json.dumps(e) for e in events)
def event(action,name=None):
    result={'Action':action,'Package':P}
    if name:result['Test']=name
    return result
GOOD=[event('run','TestControlExample'),event('pass','TestControlExample'),event('pass')]

class TestLogGate(unittest.TestCase):
    def test_valid(self):self.assertEqual(validate(logs(*GOOD),REQ,0)['status'],'PASS')
    def test_nonzero_exit(self):self.assertEqual(validate(logs(*GOOD),REQ,1)['status'],'FAIL')
    def test_missing_test(self):self.assertEqual(validate(logs(event('pass')),REQ,0)['status'],'FAIL')
    def test_skip(self):self.assertEqual(validate(logs(event('run','TestControlExample'),event('skip','TestControlExample'),event('pass')),REQ,0)['status'],'FAIL')
    def test_subtest_skip(self):self.assertEqual(validate(logs(GOOD[0],event('skip','TestControlExample/edge'),*GOOD[1:]),REQ,0)['status'],'FAIL')
    def test_package_fail(self):self.assertEqual(validate(logs(*GOOD,event('fail')),REQ,0)['status'],'FAIL')
    def test_fabricated_text_pass(self):self.assertEqual(validate('PASS: all tests',REQ,0)['status'],'FAIL')
    def test_no_run_event(self):self.assertEqual(validate(logs(*GOOD[1:]),REQ,0)['status'],'FAIL')
    def test_duplicate_runs(self):self.assertEqual(validate(logs(GOOD[0],*GOOD),REQ,0)['status'],'FAIL')
    def test_empty_expected(self):
        with self.assertRaises(ValueError):validate(logs(*GOOD),[],0)
    def test_duplicate_expected(self):
        with self.assertRaises(ValueError):validate(logs(*GOOD),REQ*2,0)
    def test_unrelated_skip_is_not_required(self):
        self.assertEqual(validate(logs(event('skip','TestOtherOptional'),*GOOD),REQ,0)['status'],'PASS')
    def test_unrelated_failure_is_failure(self):
        self.assertEqual(validate(logs(event('fail','TestOther'),*GOOD),REQ,0)['status'],'FAIL')
    def test_bad_json(self):self.assertEqual(validate(logs(*GOOD)+'\n{',REQ,0)['status'],'FAIL')
    def test_json_array(self):self.assertEqual(validate('[]',REQ,0)['status'],'FAIL')
    def test_missing_package_terminal(self):self.assertEqual(validate(logs(*GOOD[:2]),REQ,0)['status'],'FAIL')

class TestVersion(unittest.TestCase):
    def test_version(self):self.assertEqual(parse_version('go version go1.23.2 linux/amd64'),(1,23,2))
    def test_required(self):self.assertEqual(parse_version('1.26.3'),(1,26,3))
    def test_missing(self):self.assertIsNone(parse_version('unavailable'))

if __name__=='__main__':unittest.main(verbosity=2)

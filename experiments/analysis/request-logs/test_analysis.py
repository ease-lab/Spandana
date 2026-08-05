#!/usr/bin/env python3

import unittest
from datetime import datetime, timezone

from aggregate_request_logs import (
    format_timestamp,
    parse_go_duration_ms,
    parse_log_line,
)
from generate_statistics import percentile


class DurationParsingTests(unittest.TestCase):
    def test_go_duration_units(self):
        cases = {
            "1ns": 0.000001,
            "1us": 0.001,
            "1µs": 0.001,
            "1μs": 0.001,
            "1ms": 1.0,
            "1.5s": 1500.0,
            "1m2.5s": 62500.0,
            "1h2m3s": 3723000.0,
            "-1µs": -0.001,
        }
        for value, expected in cases.items():
            with self.subTest(value=value):
                self.assertAlmostEqual(parse_go_duration_ms(value), expected)

    def test_invalid_duration_is_rejected(self):
        with self.assertRaises(ValueError):
            parse_go_duration_ms("12")


class LogParsingTests(unittest.TestCase):
    def test_microsecond_log_line(self):
        line = (
            "[2025-08-09T14:31:16.394659182Z] VM Backend - "
            "Request Duration: 919.371µs,Total Duration: 1.2ms, Status Code: 200"
        )
        record = parse_log_line(line)
        self.assertEqual(record["backend"], "VM Backend")
        self.assertAlmostEqual(record["request_duration_ms"], 0.919371)
        self.assertAlmostEqual(record["total_duration_ms"], 1.2)
        self.assertEqual(record["status_code"], 200)

    def test_utc_timestamp_has_one_suffix(self):
        timestamp = datetime(2025, 8, 9, 14, 31, 16, tzinfo=timezone.utc)
        self.assertEqual(format_timestamp(timestamp), "2025-08-09T14:31:16.000000Z")


class StatisticsTests(unittest.TestCase):
    def test_percentile_interpolates(self):
        self.assertEqual(percentile([1.0, 2.0, 3.0], 0.5), 2.0)
        self.assertAlmostEqual(percentile([1.0, 2.0], 0.95), 1.95)
        self.assertIsNone(percentile([], 0.95))


if __name__ == "__main__":
    unittest.main()

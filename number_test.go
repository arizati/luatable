package luatable

import (
	"reflect"
	"testing"
)

func TestParseLuaNumber(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		cases := []struct {
			src  string
			want any
		}{
			{"0", int64(0)},
			{"123", int64(123)},
			{"-1", int64(-1)},
			{"9223372036854775807", int64(9223372036854775807)},
			{"-9223372036854775808", int64(-9223372036854775808)},

			// Decimal integers that do not fit into int64 degrade to float64.
			{"9223372036854775808", 9.223372036854776e18},

			// Floats.
			{"1.5", 1.5},
			{".5", 0.5},
			{"1.", 1.0},
			{"1e3", 1000.0},
			{"1E-3", 0.001},
			{"2.5e2", 250.0},
			{"1e+10", 1e10},

			// Hexadecimal integers.
			{"0xFF", int64(255)},
			{"0X10", int64(16)},
			{"0x0", int64(0)},
			{"0xFFFFFFFFFFFFFFFF", int64(-1)},
			{"0x8000000000000000", int64(-9223372036854775808)},

			// Hexadecimal floats.
			{"0x1p4", 16.0},
			{"0x1p-1", 0.5},
			{"0x1.8", 1.5},
			{"0x1.8p1", 3.0},
			{"0xA.8p0", 10.5},
			{"0x.8", 0.5},
			{"0xAp2", 40.0},
		}

		for _, tc := range cases {
			t.Run(tc.src, func(t *testing.T) {
				got, err := parseLuaNumber(tc.src)
				if err != nil {
					t.Fatalf("unexpected error: %s", err)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("unexpected value for %q; got %#v; want %#v", tc.src, got, tc.want)
				}
			})
		}
	})

	t.Run("error", func(t *testing.T) {
		cases := []string{
			"",
			"0x",
			"0xg",
			"1e",
			"1e+",
			"1.2.3",
			"xyz",
			"0x1p",
			"0x1p+",
			"0x1pz",
			"1e400",
		}

		for _, src := range cases {
			t.Run(src, func(t *testing.T) {
				if _, err := parseLuaNumber(src); err == nil {
					t.Fatalf("expecting an error for %q", src)
				}
			})
		}
	})
}

func TestParseHexUint64WrapsAround(t *testing.T) {
	cases := []struct {
		src  string
		want uint64
	}{
		{"0", 0},
		{"ff", 255},
		{"ffffffffffffffff", 18446744073709551615},
		{"1ffffffffffffffff", 18446744073709551615}, // wraps modulo 2^64
	}
	for _, tc := range cases {
		got, err := parseHexUint64(tc.src)
		if err != nil {
			t.Fatalf("unexpected error for %q: %s", tc.src, err)
		}
		if got != tc.want {
			t.Fatalf("unexpected value for %q; got %d; want %d", tc.src, got, tc.want)
		}
	}
}

func TestHexValue(t *testing.T) {
	cases := []struct {
		c    byte
		want int
		ok   bool
	}{
		{'0', 0, true},
		{'9', 9, true},
		{'a', 10, true},
		{'F', 15, true},
		{'g', 0, false},
		{' ', 0, false},
	}
	for _, tc := range cases {
		got, ok := hexVal(tc.c)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("hexVal(%q) = (%d, %v); want (%d, %v)", tc.c, got, ok, tc.want, tc.ok)
		}
	}
}

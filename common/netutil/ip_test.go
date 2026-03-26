package netutil

import "testing"

func TestNormalizeIP(t *testing.T) {
	tests := map[string]string{
		"":                "",
		"1.1.1.1":         "1.1.1.1",
		"::ffff:1.1.1.1":  "1.1.1.1",
		"2001:db8::1":     "2001:db8::1",
		"::ffff:::ffff:1": "::ffff:1",
	}

	for input, want := range tests {
		if got := NormalizeIP(input); got != want {
			t.Fatalf("NormalizeIP(%q) = %q, want %q", input, got, want)
		}
	}
}

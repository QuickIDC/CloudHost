package driver

import "testing"

func TestMaskToPrefixInt(t *testing.T) {
	cases := map[string]int{
		"255.255.255.0":   24,
		"255.255.255.128": 25,
		"255.255.0.0":     16,
		"24":              24,
		"":                24,
		"bad":             24,
	}
	for in, want := range cases {
		if got := maskToPrefixInt(in); got != want {
			t.Errorf("maskToPrefixInt(%q)=%d，期望 %d", in, got, want)
		}
	}
}

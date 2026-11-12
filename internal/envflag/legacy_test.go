// SPDX-License-Identifier: MIT

package envflag

import "testing"

func TestGetenvLegacySpelling(t *testing.T) {
	Legacy["ENVFLAG_TEST_NEW"] = "ENVFLAG_TEST_OLD"
	t.Cleanup(func() { delete(Legacy, "ENVFLAG_TEST_NEW") })
	for _, c := range []struct{ canonical, legacy, want string }{
		{"", "", ""},
		{"a", "", "a"},
		{"", "b", "b"},
		{"a", "b", "a"},
	} {
		t.Setenv("ENVFLAG_TEST_NEW", c.canonical)
		t.Setenv("ENVFLAG_TEST_OLD", c.legacy)
		if got := Getenv("ENVFLAG_TEST_NEW"); got != c.want {
			t.Errorf("canonical %q, legacy %q: got %q, want %q", c.canonical, c.legacy, got, c.want)
		}
	}
	t.Setenv("ENVFLAG_TEST_OTHER", "")
	if got := Getenv("ENVFLAG_TEST_OTHER"); got != "" {
		t.Errorf("unset: %q", got)
	}
}

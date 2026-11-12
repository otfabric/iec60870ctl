// SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/otfabric/iec104ctl/internal/buildinfo"
)

// libText and libJSON are how the OT Fabric libraries of this binary appear in the version
// line and in its JSON; their formatting is tested in internal/buildinfo.
var libText, libJSON = func() (string, string) {
	libs := buildinfo.Resolve("", "", "", "", "").Libraries
	var text string
	for _, l := range libs {
		text += l.Name + " " + l.Version + ", "
	}
	js, _ := json.Marshal(libs)
	return text, string(js)
}()

const sha = "1802d0f9afe8642d1e278120b76fcdfac967cb9e"

// version prints one line: with the tag of a release build, without it otherwise, and the
// development fallback when nothing was injected.
func TestVersionLine(t *testing.T) {
	t.Cleanup(func() { SetBuildMeta("dev", "", "", "") })
	platform := runtime.Version() + ", " + runtime.GOOS + "/" + runtime.GOARCH
	SetBuildMeta("1.2.3", "v1.2.3", sha, "2026-01-31T12:00:00Z")
	if r := run(t, "version").want(t, 0); r.stdout != "iec104ctl 1.2.3 (tag v1.2.3, commit "+sha+", built 2026-01-31T12:00:00Z, "+libText+platform+")\n" {
		t.Errorf("release build: %q", r.stdout)
	}
	SetBuildMeta("v1.2.3", " ", sha, "2026-01-31T12:00:00Z")
	if r := run(t, "version").want(t, 0); r.stdout != "iec104ctl 1.2.3 (commit "+sha+", built 2026-01-31T12:00:00Z, "+libText+platform+")\n" {
		t.Errorf("without a tag: %q", r.stdout)
	}
	SetBuildMeta("dev", "", "", "")
	if r := run(t, "version").want(t, 0); !strings.HasPrefix(r.stdout, "iec104ctl dev (commit ") || !strings.HasSuffix(r.stdout, ", built unknown, "+libText+platform+")\n") {
		t.Errorf("development build: %q", r.stdout)
	}
	run(t, "version", "extra").want(t, 2)
}

// --json and IEC104CTL_FORMAT=json print the same JSON line; other formats the text line.
func TestVersionJSON(t *testing.T) {
	SetBuildMeta("1.2.3", "v1.2.3", sha, "2026-01-31T12:00:00Z")
	t.Cleanup(func() { SetBuildMeta("dev", "", "", "") })
	want := `{"name":"iec104ctl","version":"1.2.3","tag":"v1.2.3","commit":"` + sha + `","built":"2026-01-31T12:00:00Z","libraries":` + libJSON + `,"go":"` +
		runtime.Version() + `","platform":"` + runtime.GOOS + "/" + runtime.GOARCH + `"}` + "\n"
	if r := run(t, "version", "--json").want(t, 0); r.stdout != want {
		t.Errorf("version --json:\n got %s\nwant %s", r.stdout, want)
	}
	text := run(t, "version").want(t, 0).stdout
	t.Setenv("IEC104CTL_FORMAT", "json")
	if r := run(t, "version").want(t, 0); r.stdout != want {
		t.Errorf("IEC104CTL_FORMAT=json:\n got %s\nwant %s", r.stdout, want)
	}
	t.Setenv("IEC104CTL_FORMAT", "table")
	if r := run(t, "version").want(t, 0); r.stdout != text {
		t.Errorf("IEC104CTL_FORMAT=table: %q", r.stdout)
	}
}

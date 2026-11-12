// SPDX-License-Identifier: MIT

package buildinfo

import (
	"encoding/json"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

const sha = "1802d0f9afe8642d1e278120b76fcdfac967cb9e"

var platform = runtime.Version() + ", " + runtime.GOOS + "/" + runtime.GOARCH

// withBuildInfo makes Resolve see bi as what the toolchain recorded (nil: nothing).
func withBuildInfo(t *testing.T, bi *debug.BuildInfo) {
	t.Helper()
	old := readBuildInfo
	t.Cleanup(func() { readBuildInfo = old })
	readBuildInfo = func() (*debug.BuildInfo, bool) { return bi, bi != nil }
}

func TestReleaseBuild(t *testing.T) {
	withBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Version: "v0.0.1"}})
	i := Resolve("tool", "3.5.1", "v3.5.1", sha, "2026-11-05T15:41:49Z")
	if got, want := i.String(), "tool 3.5.1 (tag v3.5.1, commit "+sha+", built 2026-11-05T15:41:49Z, "+platform+")"; got != want {
		t.Errorf("text:\n got %s\nwant %s", got, want)
	}
	want := `{"name":"tool","version":"3.5.1","tag":"v3.5.1","commit":"` + sha + `","built":"2026-11-05T15:41:49Z","libraries":[],"go":"` +
		runtime.Version() + `","platform":"` + runtime.GOOS + "/" + runtime.GOARCH + `"}`
	if got := i.JSON(); got != want {
		t.Errorf("json:\n got %s\nwant %s", got, want)
	}
}

func TestWithoutTag(t *testing.T) {
	withBuildInfo(t, nil)
	i := Resolve("tool", "v3.5.1", " ", sha, "2026-11-05T15:41:49Z")
	if got, want := i.String(), "tool 3.5.1 (commit "+sha+", built 2026-11-05T15:41:49Z, "+platform+")"; got != want {
		t.Errorf("text:\n got %s\nwant %s", got, want)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(strings.Replace(i.JSON(), `"libraries":[],`, "", 1)), &m); err != nil || m["tag"] != "" || len(m) != 7 {
		t.Errorf("json %s: %v %v", i.JSON(), m, err)
	}
}

// Nothing injected: go run, go build, go install.
func TestDevFallback(t *testing.T) {
	rev := []debug.BuildSetting{{Key: "vcs.revision", Value: sha}, {Key: "vcs.modified", Value: "true"}}
	for _, c := range []struct {
		name    string
		version string
		bi      *debug.BuildInfo
		want    string
	}{
		{"nothing recorded", "dev", nil, "tool dev (commit unknown, built unknown, " + platform + ")"},
		{"empty injection", "", &debug.BuildInfo{}, "tool dev (commit unknown, built unknown, " + platform + ")"},
		{"go build in a modified clone", "dev", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: rev},
			"tool dev (commit " + sha + "-dirty, built unknown, " + platform + ")"},
		{"clean clone", "dev", &debug.BuildInfo{Settings: rev[:1]}, "tool dev (commit " + sha + ", built unknown, " + platform + ")"},
		{"go install of a version", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v3.5.1"}},
			"tool 3.5.1 (commit unknown, built unknown, " + platform + ")"},
		{"go build in a clone after a tag", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v0.4.1-0.20261107212138-864e49686ecb+dirty"}, Settings: rev},
			"tool dev (commit " + sha + "-dirty, built unknown, " + platform + ")"},
		{"go build at a tag, modified", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v0.4.0+dirty"}},
			"tool dev (commit unknown, built unknown, " + platform + ")"},
	} {
		t.Run(c.name, func(t *testing.T) {
			withBuildInfo(t, c.bi)
			i := Resolve("tool", c.version, "", "", "")
			if got := i.String(); got != c.want {
				t.Errorf("\n got %s\nwant %s", got, c.want)
			}
			if j := i.JSON(); !strings.Contains(j, `"tag":""`) || strings.Count(j, `""`) != 1 {
				t.Errorf("json has an empty field other than tag: %s", j)
			}
		})
	}
}

// An injected commit wins over the recorded revision.
func TestInjectedCommitWins(t *testing.T) {
	withBuildInfo(t, &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "other"}}})
	if i := Resolve("tool", "1.0.0", "", sha, ""); i.Commit != sha {
		t.Errorf("commit %q", i.Commit)
	}
}

func TestJSONKeyOrder(t *testing.T) {
	withBuildInfo(t, nil)
	j := Resolve("tool", "1.0.0", "v1.0.0", sha, "2026-01-01T00:00:00Z").JSON()
	last := -1
	for _, k := range []string{"name", "version", "tag", "commit", "built", "libraries", "go", "platform"} {
		p := strings.Index(j, `"`+k+`":`)
		if p <= last {
			t.Fatalf("key %s out of order in %s", k, j)
		}
		last = p
	}
	if strings.Contains(j, "\n") {
		t.Errorf("not one compact line: %q", j)
	}
}

// The libraries, from an injected list: none, one and two.
func TestLibrariesFormatting(t *testing.T) {
	base := Info{Name: "tool", Version: "1.0.0", Commit: sha, Built: "2026-01-01T00:00:00Z", Go: "go1.26.0", Platform: "linux/amd64"}
	for _, c := range []struct {
		libs     []Library
		text, js string
	}{
		{nil, "built 2026-01-01T00:00:00Z, go1.26.0", `"libraries":[]`},
		{[]Library{{"go-a", "0.8.0"}}, "built 2026-01-01T00:00:00Z, go-a 0.8.0, go1.26.0", `"libraries":[{"name":"go-a","version":"0.8.0"}]`},
		{[]Library{{"go-a", "0.8.0"}, {"go-b", "0.5.1-0.20261101120000-0123456789ab"}},
			"built 2026-01-01T00:00:00Z, go-a 0.8.0, go-b 0.5.1-0.20261101120000-0123456789ab, go1.26.0",
			`"libraries":[{"name":"go-a","version":"0.8.0"},{"name":"go-b","version":"0.5.1-0.20261101120000-0123456789ab"}]`},
	} {
		i := base
		i.Libraries = c.libs
		if !strings.Contains(i.String(), c.text+", linux/amd64)") {
			t.Errorf("text %s lacks %s", i.String(), c.text)
		}
		if !strings.Contains(i.JSON(), `"built":"2026-01-01T00:00:00Z",`+c.js+`,"go":"go1.26.0"`) {
			t.Errorf("json %s lacks %s", i.JSON(), c.js)
		}
	}
}

// Resolve takes the github.com/otfabric/go-* dependencies, replaced where replaced, sorted,
// without the main module and other modules.
func TestLibrariesFromBuildInfo(t *testing.T) {
	withBuildInfo(t, &debug.BuildInfo{
		Main: debug.Module{Path: "github.com/otfabric/go-self"},
		Deps: []*debug.Module{
			{Path: "github.com/otfabric/go-zeta", Version: "v1.2.0"},
			{Path: "github.com/spf13/cobra", Version: "v1.10.2"},
			{Path: "github.com/otfabric/go-self", Version: "v9.0.0"},
			{Path: "github.com/otfabric/docsync", Version: "v0.3.1"},
			{Path: "github.com/otfabric/go-alpha", Version: "v0.1.0", Replace: &debug.Module{Path: "github.com/otfabric/go-alpha", Version: "v0.1.1"}},
		},
	})
	got := Resolve("tool", "1.0.0", "", sha, "").Libraries
	if len(got) != 2 || got[0] != (Library{"go-alpha", "0.1.1"}) || got[1] != (Library{"go-zeta", "1.2.0"}) {
		t.Errorf("libraries: %+v", got)
	}
}

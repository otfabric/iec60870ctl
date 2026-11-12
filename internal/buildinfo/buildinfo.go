// SPDX-License-Identifier: MIT

// Package buildinfo resolves the build metadata that `version` prints: what the release
// injected at link time (-X main.version=… in the Makefile and the release workflow),
// else what the Go toolchain recorded in the binary.
package buildinfo

import (
	"encoding/json"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
)

const (
	// Dev is the version of a build that carries no version: go run, or go build in a clone.
	Dev = "dev"
	// Unknown stands for a commit or a build date that nothing recorded.
	Unknown = "unknown"
)

// Info is what `version` reports. The field order is the key order of the JSON output.
type Info struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Tag is the git tag of a release build, "" for any other build.
	Tag    string `json:"tag"`
	Commit string `json:"commit"`
	Built  string `json:"built"`
	// Libraries are the OT Fabric libraries (github.com/otfabric/go-*) the binary is built
	// on, sorted by name; [] when there are none.
	Libraries []Library `json:"libraries"`
	Go        string    `json:"go"`
	Platform  string    `json:"platform"`
}

// Library is one OT Fabric library the binary is built on.
type Library struct {
	Name    string `json:"name"`    // last element of the module path: go-bacnet
	Version string `json:"version"` // without the leading v
}

// readBuildInfo is debug.ReadBuildInfo; a test replaces it.
var readBuildInfo = debug.ReadBuildInfo

// Resolve returns the build metadata of the binary named name from the link-time values
// (empty, or the "dev" default of main, when not injected). A missing version is the
// module version the toolchain recorded for a release (go install …@vX.Y.Z), else "dev"
// (go run, and go build in a clone, which records a pseudo-version); a missing
// commit is the recorded VCS revision, with "-dirty" for a modified tree, else "unknown".
func Resolve(name, version, tag, commit, buildDate string) Info {
	info := Info{
		Name:      name,
		Version:   trimV(strings.TrimSpace(version)),
		Tag:       strings.TrimSpace(tag),
		Commit:    strings.TrimSpace(commit),
		Built:     strings.TrimSpace(buildDate),
		Libraries: []Library{},
		Go:        runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	bi, ok := readBuildInfo()
	if !ok {
		bi = nil
	}
	if info.Version == "" || info.Version == Dev {
		info.Version = Dev
		if bi != nil && isRelease(bi.Main.Version) {
			info.Version = trimV(bi.Main.Version)
		}
	}
	if info.Commit == "" && bi != nil {
		var dirty bool
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				info.Commit = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if info.Commit != "" && dirty {
			info.Commit += "-dirty"
		}
	}
	if bi != nil {
		info.Libraries = libraries(bi)
	}
	if info.Commit == "" {
		info.Commit = Unknown
	}
	if info.Built == "" {
		info.Built = Unknown
	}
	return info
}

// libraries returns the github.com/otfabric/go-* modules among the dependencies of bi
// (their replacements where replaced), without the main module, sorted by name.
func libraries(bi *debug.BuildInfo) []Library {
	out := []Library{}
	for _, m := range bi.Deps {
		if m.Replace != nil {
			m = m.Replace
		}
		if !strings.HasPrefix(m.Path, "github.com/otfabric/go-") || m.Path == bi.Main.Path {
			continue
		}
		out = append(out, Library{Name: m.Path[strings.LastIndex(m.Path, "/")+1:], Version: trimV(m.Version)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// notRelease matches the module versions the toolchain makes up for a build in a clone: a
// pseudo-version (v0.4.1-0.20261107212138-864e49686ecb) or a modified tree (+dirty).
var notRelease = regexp.MustCompile(`\d{14}-[0-9a-f]{12}|\+dirty$`)

// isRelease reports whether v, a module version the toolchain recorded, is the version of
// a release: what go install …@v1.2.3 records, not (devel) or a pseudo-version.
func isRelease(v string) bool {
	return v != "" && v != "(devel)" && !notRelease.MatchString(v)
}

// trimV drops the leading v of a version: v3.5.1 is 3.5.1.
func trimV(v string) string {
	if len(v) > 1 && v[0] == 'v' && v[1] >= '0' && v[1] <= '9' {
		return v[1:]
	}
	return v
}

// String is the one line of `version`:
//
//	name 3.5.1 (tag v3.5.1, commit <sha>, built <date>, go-lib 1.2.0, go1.26.0, linux/amd64)
//
// without the tag part when there is no tag, and without libraries when there are none.
func (i Info) String() string {
	var b strings.Builder
	b.WriteString(i.Name + " " + i.Version + " (")
	if i.Tag != "" {
		b.WriteString("tag " + i.Tag + ", ")
	}
	b.WriteString("commit " + i.Commit + ", built " + i.Built + ", ")
	for _, l := range i.Libraries {
		b.WriteString(l.Name + " " + l.Version + ", ")
	}
	b.WriteString(i.Go + ", " + i.Platform + ")")
	return b.String()
}

// JSON is the one line of `version --json`, without a trailing newline.
func (i Info) JSON() string {
	if i.Libraries == nil {
		i.Libraries = []Library{}
	}
	b, _ := json.Marshal(i) // a struct of strings always marshals
	return string(b)
}

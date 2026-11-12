# Releasing

The four tools share one version: a release is tagged `vX.Y.Z` and carries iec101ctl,
iec102ctl, iec103ctl and iec104ctl, built from the same commit (decided 2026-11-12). The
first release, v0.9.0, continues the version line of iec104ctl, whose last release from
its own repository was v0.8.0, so the installer upgrades existing iec104ctl installs.

## Steps

1. Add the section to [RELEASE.md](../RELEASE.md), newest first: the heading is exactly
   `## vX.Y.Z`, then `**Date:**` and `**Previous release:**` lines, the notes per tool,
   and a line `---`. Call out every change to commands, flags, JSON output or exit codes.
   Without the section the workflow stops before tagging.
2. Check that CI, Docs and Interop are green on the commit.
3. Run the Release workflow (Actions → Release) with `bump` (`patch`, `minor`, `major`, or
   `none` to rebuild the assets of the newest tag), or with an exact `version`. It runs
   the tests, builds the four tools for linux/amd64, linux/arm64, linux/arm/v7,
   darwin/amd64, darwin/arm64, windows/amd64 and windows/arm64, tags the commit, and
   publishes a GitHub release with the archives and one checksum file per tool.

```bash
gh workflow run release.yml -R otfabric/iec60870ctl -f bump=minor
```

## Assets

Named as the shared `go-binary-release.yml` of otfabric/.github names them, so the
OT Fabric installer (`get.otfabric.io`, [otfabric/install](https://github.com/otfabric/install),
where the four tools are registered with this repository) finds them:
`<tool>-<version>-<os>-<arch>.tar.gz` (`.zip` for Windows, `armv7` for linux/arm/v7), each
holding `<tool>-<version>-<os>-<arch>[.exe]`, and `<tool>-<version>-checksums.txt`
(SHA-256). The binaries report the tag they were built from (`<tool> version`).

## Locally

`make build-nocheck` builds `bin/<tool>` for each tool with the version of the `vX.Y.Z`
tag on HEAD, or `dev`. `make release-all` cross-builds and packages every tool into
`release/`.

`go install github.com/otfabric/iec60870ctl/cmd/<tool>@vX.Y.Z` installs one tool of a
release.

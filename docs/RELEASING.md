# Releasing

The repository ships four tools. Whether they share one version or are versioned on their
own is not decided yet; the tooling supports both, and the choice is made per release by
the Release workflow's `tool` input.

| Scheme | Workflow input | Tag | Next version from | Assets | RELEASE.md heading |
|--------|----------------|-----|-------------------|--------|--------------------|
| Shared version | `tool: all` | `vX.Y.Z` | the newest `v*` tag | all four tools | `## vX.Y.Z` |
| Per tool | `tool: iec104ctl` (or another) | `iec104ctl/vX.Y.Z` | the newest `iec104ctl/v*` tag | that tool only | `## iec104ctl/vX.Y.Z` |

`bump` is `patch`, `minor` or `major`, or `none` to rebuild the assets of the newest tag
of the scheme.

## Steps

1. Add the section to [RELEASE.md](../RELEASE.md), newest first: the heading is exactly the
   tag, then `**Date:**` and `**Previous release:**` lines, the notes, and a line `---`.
   Call out every change to commands, flags, JSON output or exit codes. Without the
   section the workflow stops before tagging.
2. Check that CI, Docs and Interop are green on the commit.
3. Run the Release workflow (Actions → Release) with `tool` and `bump`. It runs the
   tests, builds every tool of the release for linux/amd64, linux/arm64, linux/arm/v7,
   darwin/amd64, darwin/arm64, windows/amd64 and windows/arm64, tags the commit, and
   publishes a GitHub release with the archives and one checksum file per tool.

## Assets

Named as the shared `go-binary-release.yml` of otfabric/.github names them, so the
OT Fabric installer and existing scripts find them:
`<tool>-<version>-<os>-<arch>.tar.gz` (`.zip` for Windows, `armv7` for linux/arm/v7), each
holding `<tool>-<version>-<os>-<arch>[.exe]`, and `<tool>-<version>-checksums.txt`
(SHA-256). The binaries report the tag and version they were built from (`<tool>
version`).

## Locally

`make build-nocheck` builds `bin/<tool>` for each tool with the version of the tag on
HEAD: the tool's own `<tool>/vX.Y.Z` if there is one, else a shared `vX.Y.Z`, else `dev`.
`make release-all` cross-builds and packages every tool into `release/`.

## Before choosing the per-tool scheme

- The installer (`get.otfabric.io`, [otfabric/install](https://github.com/otfabric/install))
  resolves "latest" with the repository's latest release and downloads from
  `releases/download/v<version>/`. Per-tool tags need it to look up the newest
  `<tool>/v*` release instead, and the tools to be registered there with this repository.
- GitHub marks one release of the repository as "Latest": with per-tool tags it is the
  tool released last, and the README's release badge shows that one.
- `go install github.com/otfabric/iec60870ctl/cmd/<tool>@vX.Y.Z` needs a module tag
  `vX.Y.Z`: per-tool tags are not module versions, so `@latest` resolves to the newest
  shared `v*` tag (or a pseudo-version).

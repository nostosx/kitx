# Releasing kitx

This guide is for maintainers publishing `github.com/nostosx/kitx` from
[nostosx/kitx](https://github.com/nostosx/kitx). Releases are manual. The initial
stable version is **v1.0.0**; the commands below use that version as an example.
Replace it consistently when preparing a later release.

## Versioning

All packages belong to one Go module and share a root Git tag. Use semantic
versioning:

- Patch: compatible bug fixes.
- Minor: compatible additions.
- Major: incompatible API or documented behavior changes.

Version v1 uses `github.com/nostosx/kitx` without a `/v1` suffix. Starting with
v2, a major release requires the matching module path suffix and updated imports.
The `go 1.27.3` directive specifies the minimum Go toolchain; it is independent
of the module release version.

## Prepare a candidate

1. Review exported API and behavior changes, including cancellation, ownership,
   errors, and platform guarantees. Confirm the version matches their
   compatibility impact.
2. Finalize the corresponding section in [CHANGELOG.md](CHANGELOG.md). For later
   releases, move entries from `Unreleased` into the versioned section. Add the
   planned publication date when finalizing the candidate. Use this section as
   the GitHub release notes.
3. Update the release status and installation version in both READMEs. Verify
   their examples against the API and include `LICENSE` in the source archive.
4. With Go 1.27.3 and the tools from [CONTRIBUTING.md](CONTRIBUTING.md), run:

   ```sh
   make check
   make lint
   make vulncheck
   ```

5. Review the final diff and record the candidate commit. Publish the candidate
   branch and require successful check jobs on Linux, macOS, and Windows, plus
   lint and vulnerability scans, for that exact commit.
6. Confirm the working tree is clean and the target tag does not already exist.
   If the candidate changes, repeat validation before tagging.

## Publish the release

Tag the validated commit with an annotated root tag, then push the tag to
`github.com/nostosx/kitx`. After checking out the candidate and confirming that
`origin` points to that repository:

```sh
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin refs/tags/v1.0.0
```

Create a [GitHub release](https://github.com/nostosx/kitx/releases/new) using the
existing tag and its changelog section. Check the tag's commit before publishing.
The release is a source module; no binary build or Go module upload is required.

Do not move or replace a published tag. A correction to released code needs a
new version. If a release must be withdrawn, add the appropriate `retract`
directive to `go.mod`, document the reason, and publish a new version carrying
that directive.

## Verify the published module

In a new, empty directory outside the repository, run:

```sh
go mod init example.com/kitx-release-check
GOWORK=off go get github.com/nostosx/kitx@v1.0.0
GOWORK=off go list -m -json github.com/nostosx/kitx
```

Confirm that the selected version is `v1.0.0`, there is no `Replace` field, and
the downloaded module includes `LICENSE`. Run the README's `main.go` in this
consumer with `GOWORK=off go run .`; its output must match the README. Check
the [published API documentation](https://pkg.go.dev/github.com/nostosx/kitx@v1.0.0)
and GitHub release notes against the tag. Module proxies and documentation
indexing may take time to update.

For each later release, also build representative consumers against the
published version and check the documented compatibility guarantees before
announcing it.

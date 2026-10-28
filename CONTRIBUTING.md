# Contributing to kitx

Bug reports, documentation improvements, and focused code changes are welcome.
For vulnerabilities, follow the [security policy](SECURITY.md) instead of
posting details in a public issue.

Install [Task](https://taskfile.dev/docs/installation/) v3.53.1 outside the module,
matching CI, and put its binary on `PATH`:

```sh
go install github.com/go-task/task/v3/cmd/task@v3.53.1
```

Run `task --list` to discover tasks. Tool overrides accept environment variables or
Task arguments, for example `task check GO="/path with spaces/go"`.
Checks force `GOWORK=off`; `check`, `lint` and `vulncheck` do not apply source fixes.

## Set up a checkout

Install Go 1.27.3 or later, Git, and Task v3.53.1 with a POSIX shell. On Windows,
run Task from Git Bash. CI uses Go 1.27.3 on Linux, macOS, and Windows.

Fork [nostosx/kitx](https://github.com/nostosx/kitx), clone your fork, and enter
the checkout. To work from the upstream source directly:

```sh
git clone https://github.com/nostosx/kitx.git
cd "kitx"
task check
```

Install the pinned lint and vulnerability tools, matching CI. Build the linter
with Go 1.27.3. These tools are installed separately from the library module:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
```

Add the installation directory to `PATH`. Alternatively, select executables
with the Task variables `GO`, `GOFMT`, `GOLANGCI_LINT`, and `GOVULNCHECK`.
`Taskfile.yml` sets `GOWORK=off`, so a local workspace cannot supply hidden
dependencies.

## Run checks

Before opening a pull request, run:

```sh
task check
task lint
task vulncheck
```

| Command | Purpose |
| --- | --- |
| `task check` | Check formatting, modernization, module tidiness, vet, builds, dependency boundaries, and race tests. |
| `task test` | Run unit tests with shuffled order and disabled result caching. |
| `task test RACE=true` | Run the same tests with the race detector. |
| `task lint` | Run golangci-lint on default and integration-tag builds. |
| `task vulncheck` | Scan default and integration-tag builds with govulncheck. |
| `task test COVER=true` | Run race tests and write `coverage.out`, then print function coverage. |

`task check` does not apply formatting or source fixes. Use `task fmt`,
`task fix`, or `task tidy` to apply the corresponding changes, then review
the diff. `task --list` lists the main entry points.

The eight public commands are `check`, `test`, `build`, `lint`, `vulncheck`,
`fmt`, `fix` and `tidy`. Running `task` selects `check`. `RACE` and `COVER`
accept only `true` or `false`; coverage implies race. Quality substeps are
internal, so `task --list-all` shows the same commands as `task --list`.

For a focused test run during development:

```sh
go test -race -count=1 ./ttlcache
go test -run '^Example' -count=1 ./...
```

CI runs quality checks on all three platforms; lint and vulnerability scans
are independent jobs. A platform-specific change needs tests on the affected
platform as well as the local checks.

## Design and test a change

Keep each package responsible for one reusable capability. Prefer the standard
library when it already provides the behavior. A new API needs a concrete,
recurring use case, a clear package responsibility, and behavior beyond the
standard library. Show the intended call site and explain why existing APIs
cannot express it clearly; package or function counts are not goals.

Code and tests must use only the standard library. Packages must remain usable
independently, with application policies and adapters at the call site. Do not
add a root facade, a DI container, or a general `utils` package.

- Add a regression test for a bug fix. Assert public behavior and error identity,
  including cancellation, limits, and resource ownership when relevant.
- Use `testing/synctest` for timers and cancellation, explicit synchronization
  for scheduling, and `t.TempDir` for filesystem fixtures. Avoid arbitrary sleeps.
- Join test goroutines before returning. Report worker errors to the test
  goroutine; do not call `t.Fatal` or `t.FailNow` from a worker.
- Prefer external test packages for public contracts and `Example` functions
  for usage. Use internal tests when implementation invariants need coverage.
- Document exported APIs in English. State zero-value behavior, errors,
  cancellation, concurrency, and ownership where applicable. Keep the English
  and Chinese READMEs aligned.

The v1 API is the compatibility baseline. Preserve documented behavior in patch
and minor releases. Add user-visible changes under an `Unreleased` section in
[CHANGELOG.md](CHANGELOG.md); include a migration path for proposed breaking
changes. The [release guide](RELEASING.md) describes versioning and publication.

## Open a pull request

For a bug report, include the kitx version or source revision, Go version,
operating system, a minimal reproducer, and expected versus actual behavior.
For a new API or a broad change, open an issue explaining the use case and
proposed contract before investing in the implementation.

Keep pull requests focused on one problem. Describe the resulting behavior,
why it is needed, the checks you ran, and any platform limitations. Include
documentation and examples with API changes. Keep commits coherent and exclude
unrelated formatting, generated coverage files, and local environment settings.

By contributing, you agree to license your contribution under the project's
[MIT License](LICENSE).

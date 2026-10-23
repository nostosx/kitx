# kitx development

Parent workspace instructions apply. Read README.md and CONTRIBUTING.md before
editing; CI is .github/workflows/ci.yml.

- Always load `golang-how-to` for Go work, then the relevant Go skills.
- Single Go 1.27.3 module; standard library only, including tests.
- Public packages live at the module root. Keep names specific to their role;
  do not add a root facade, utils package, DI container or implicit global state.
- Code comments are English. Every public API documents zero values, errors,
  cancellation, concurrency and ownership where relevant.
- Keep application adapters and policies outside the library. Preserve each
  package's cancellation, persistence and algorithm contracts during reuse.
- `make check`: formatting, modernizers, tidy diff, vet, build, dependency checks
  and randomized race tests. `make lint`: golangci-lint v2.15.0;
  `make vulncheck`: govulncheck. `make test` is ordinary testing; `make race`
  runs the race suite. `make fmt`, `make fix` and `make tidy` modify files.
  CI jobs are independent; manual release verification is in RELEASING.md.
- Use synctest and explicit barriers for concurrency tests; do not add wall-clock
  sleeps or third-party test dependencies. Keep executable public API examples.

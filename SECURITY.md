# Security policy

## Report a vulnerability

If a problem could expose private data or cross a security boundary, use
[GitHub private vulnerability reporting](https://github.com/nostosx/kitx/security/advisories/new)
when it is available for this repository. If that channel is unavailable, open
a public issue requesting a private contact without including exploit details.
Report ordinary bugs through public issues.

Include the following in the private report:

- Affected package and module version or source revision.
- Go version, operating system, and relevant configuration or limits.
- A minimal reproducer using synthetic data.
- Expected behavior, observed behavior, and the potential security impact.

Remove credentials, private file contents, and access-bearing URLs from reports.

## Security boundaries

kitx provides process-local primitives. Applications are responsible for
authorization and tenant isolation, input limits, and the lifetimes of shared
resources. In particular, use authorization-scoped cache and singleflight keys,
trusted parent paths or an appropriate `os.Root` for file replacement,
cancellation-aware callbacks, and synchronization for shared mutable values.

The [README](README.md#usage-boundaries) outlines these responsibilities and
platform limits. The [API reference](https://pkg.go.dev/github.com/nostosx/kitx)
specifies each package's guarantees. When reporting a boundary violation, include the
documented guarantee you expected the operation to preserve.

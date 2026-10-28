# Changelog

User-visible changes to [kitx](https://github.com/nostosx/kitx).
Module versions follow semantic versioning.

## Unreleased

- Development tooling: replace Make entry points with Task v3.53.1 in local
  checks, CI and contribution/release instructions. Use `task check`, `task lint`
  and `task vulncheck`; check coverage, build tags and failure gates are preserved.

## v1.0.0 - 2026-10-23

Initial stable release. Requires Go 1.27.3 or later. Code and tests depend only
on the standard library. All 15 packages share one module version.

### Added

- `backoff` and `retry`: capped exponential backoff, full jitter, explicit retry
  predicates, attempt limits, total and per-attempt timeouts, and delay hints.
- `parallel`: bounded processing of slices and iterators, ordered map results,
  cancellation propagation, and worker joining.
- `singleflight`: same-key request coalescing with first-caller execution or
  shared execution owned independently of waiting requests.
- `semaphore`, `ratelimit`, and `queue`: weighted FIFO admission, token-bucket
  rate limits, and bounded queues with cancellation and drain after close.
- `ttlcache`: capacity limits, LRU eviction, per-entry TTL,
  atomic retrieval and removal, and injectable clocks.
- `circuitbreaker`: consecutive-failure thresholds, cooldown, and bounded
  recovery probes with isolation from stale completions.
- `atomicfile`: file replacement through same-directory temporary files,
  with path-based and `os.Root` APIs.
- `boundedio` and `cleanup`: bounded stream reads and copies, and reverse-order
  resource cleanup with joined errors.
- `dataurl`: data URL encoding and decoding with media type validation,
  preserved parameters, and identifiable errors for invalid input.
- `utf8text`: valid UTF-8 prefixes with a total byte budget that includes
  the truncation suffix.
- `syncmap`: concurrent typed maps with shallow snapshots.

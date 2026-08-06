# ADR 0002: One real provider, behind an interface

**Status:** Accepted
**Date:** 2026-07-30
**Supersedes:** an earlier draft of this ADR that shipped a deterministic mock
provider and a `-mock` flag.

## Context

Whetstone needs a language model for lenses, provocations, and drafts. Three
things had to be true of however that dependency was wired:

1. The credential must live in one place, be easy to set up once, and never
   reach a log line, an error message, or the screen.
2. Changing vendor, or pointing at a gateway, must not be a refactor.
3. The test suite must run in CI with no credential and no network. A suite that
   needs a paid API key is a suite that silently stops being run.

An earlier version of this design satisfied (3) with a shipped `Mock` provider
and a `-mock` flag, copying the shape of `aigateway/orchestrator/backend.py`.
That was reversed. The reasons are in "Alternatives" below.

## Decision

**One real implementation.** `provider.OpenAI` speaks the OpenAI Chat
Completions wire format over `net/http`. There is no mock provider, no offline
mode, and no `-mock` flag. The tool talks to the model you configured or it
tells you exactly why it cannot.

**Behind a two-method interface.** `provider.Provider` is `Name()` and
`Complete(ctx, Request) (Response, error)`. Everything above the package —
`lens`, `provoke`, `tui` — depends only on that. Adding a vendor is one file.
`WHETSTONE_BASE_URL` already covers pointing at a gateway or broker without
touching code.

**Credential resolution in one place.** `provider.LoadKey` tries, in order:
an explicit value (`-key-file`), `$OPENAI_API_KEY`, then the key file written by
`whetstone -set-key`. The environment variable beats the file so a shell can
override for one session. There is deliberately no project-local source: a
credential that lives beside the source tree eventually gets committed.

The key file is written atomically at mode 0600 under the user config
directory. `provider.Redact` scrubs the key from every error, and
`provider.Fingerprint` gives a display form for `-check` that is useless to
anyone reading over a shoulder.

**Testing uses fakes, not a mock provider.** Each package that consumes a
`Provider` defines its own ~20-line `fakeProvider` in its own `_test.go`. It
records requests and returns whatever the test hands it. `internal/provider`
itself is tested against `httptest` servers speaking the real wire format.

## Consequences

**Good**

- No second code path. What you test is what runs, and a demo cannot silently
  succeed against canned output while the real thing is broken.
- Setup is one command (`whetstone -set-key`) and verification is another
  (`whetstone -check`), so a bad key, a wrong endpoint, or a model the account
  cannot reach fails immediately with a specific message rather than three
  screens into a session.
- Tests are more honest. A fake that returns exactly the JSON a test needs makes
  the parser's contract visible at the call site, where a shared mock hid it
  behind a package-level canned response.
- Every package except `internal/tui` and `cmd/whetstone` is stdlib-only, so
  most of the suite builds and runs with no module downloads at all.

**Bad**

- No offline demo. Showing the tool requires a key and a network.
- No zero-cost way to exercise the full loop end to end. Every real run spends
  tokens, so manual testing has a price.
- Vendor-specific features — tool calling, streaming, prompt caching, structured
  outputs beyond `json_object` — are not reachable through the narrow interface.
  Adding one widens the contract for every future implementation.
- We own retries, timeouts, and error mapping by hand. There are currently no
  retries at all.

## Alternatives considered

**Keep the mock provider (the previous decision).** It gave a free offline demo
and let CI run the whole loop. It was dropped because a mock is a second
implementation of the interface that nothing in production exercises: it drifts,
it can keep satisfying a parser the real model has started to violate, and a
green suite against canned output says less than it appears to. It also created
two ways to run the program, and the cheap one is the one people reach for.

**Record/replay against real responses (VCR-style).** Genuinely tempting: real
fixtures, no drift from hand-written canned text, no spend in CI. Rejected for
now as more machinery than a project this size needs, and the fixtures would
still go stale. Worth revisiting if the number of prompts grows.

**Hit the real API in CI.** Costs money per pipeline, needs a credential in CI
variables, and makes the suite fail when the vendor has an outage. A test that
fails for reasons unrelated to the change is a test people learn to ignore.

## Notes

`Request.JSON` asks for a JSON object, but nothing guarantees one arrives.
Models fence output in triple backticks, prepend "Here is the JSON:", and append
a closing sentence. Rather than each feature growing its own tolerance for that,
every JSON-shaped response goes through `provider.ExtractJSON`, which strips
fences, finds the outermost brace-balanced object with string-awareness, and on
failure returns an error quoting what actually came back.

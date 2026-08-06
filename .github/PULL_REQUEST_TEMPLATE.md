## What changed

<!-- One paragraph. What does this do that the code did not do before? -->

## Why

<!-- The problem, not the solution. -->

Closes #

## How to verify

```sh
go test -race ./...
go build -o whetstone ./cmd/whetstone && ./whetstone -web
```

## Design constraints

Whetstone is defined by what it refuses to do, and that erodes one PR at a time.
Tick what applies, or say below why it does not.

- [ ] No new free-text path to the model (`docs/adr/0001-no-chat-box.md`)
- [ ] Nothing generates outline structure — the argument stays the user's
- [ ] Provocations still cannot be resolved without a reason
- [ ] Any new model call has its own `provider.Purpose` and parses via
      `provider.ExtractJSON`
- [ ] The API key cannot reach a log, an error, or the screen
- [ ] Errors surface — "nothing found" and "the provider is down" must not look
      the same

## Checklist

- [ ] `gofmt -l .` is empty, `go vet ./...` passes
- [ ] `go test -race ./...` passes
- [ ] New behaviour has a test; new failure modes have one too
- [ ] Client changes checked with `node --check internal/web/assets/app.js`
- [ ] Doc comments explain *why* where the reason is not obvious
- [ ] An ADR added or updated if this changes a decision

## Screenshots

<!-- For UI changes. -->

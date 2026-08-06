# Contributing

## Getting set up

```sh
git clone https://github.com/tanisha327/whetstone.git
cd whetstone
go mod tidy      # generates go.sum on first checkout
make test
make web         # needs a key: see below
```

You do not need an API key to run the **tests** — every package that consumes a
`Provider` defines its own small `fakeProvider` in its own `_test.go`, and
`internal/provider` is tested against `httptest` servers. You do need one to run
the **program**; there is no mock provider and no offline mode
(see `docs/adr/0002-provider-seam.md`).

```sh
whetstone -set-key   # store it once, mode 0600
whetstone -check     # verify key, endpoint, and model
```

If you find yourself needing a real key to test something, that is usually a
sign the logic belongs above the provider seam rather than below it.

## Before you open a pull request

```sh
make lint     # gofmt + go vet + staticcheck
make test     # -race
go mod tidy   # CI fails if go.mod/go.sum are not tidy
node --check internal/web/assets/app.js   # after any client change
```

## The constraints

Whetstone is defined more by what it refuses than by what it does. These are
enforced by tests, and the pull request template asks about them:

1. **No free-text path to the model.** Every call is a fixed `provider.Purpose`
   with its own prompt and schema. See `docs/adr/0001-no-chat-box.md`.
2. **The outline is the user's.** Nothing generates outline structure. Drafts
   run only from notes and citations the user supplied.
3. **Resolving a provocation requires a reason.** Both `Engage` and `Dismiss`
   reject empty input.
4. **`View()` is pure.** No provider calls, no file writes, no mutation. There
   is a test that asserts this across every mode.
5. **Errors surface.** Never swallow one into a no-op. "No provocations found"
   and "the provider is down" must not look the same to the user.

If you want to change one of these, that is a legitimate thing to want — write
or amend an ADR and argue it there, rather than working around it in code.

## House style

- Doc comment on every exported identifier, and on unexported ones where the
  *why* is not obvious from the name. Prefer explaining the reason over
  restating the signature.
- Errors are lowercase and wrapped with `%w`. The exception is user-facing
  guidance text, which is prose on purpose and lives in the prompt/UI layer.
- Prefer `errors.New` and `fmt.Errorf` over bespoke error types unless callers
  need to branch on the error, in which case give it a name and test the
  `errors.Is`/`errors.As` path.
- Normalise data once, at the boundary that owns it. Trailing-slash handling
  lives in `normalizeBaseURL`, not at each call site.
- Table-driven tests where the cases share a body; plain subtests where they
  do not. Use `strings.Contains`, not a hand-rolled substring search.

## Adding a model call

1. Add a `provider.Purpose` constant. The set of purposes is the complete
   enumeration of how this program may talk to a model; keep it that way.
2. Write the system prompt as a named constant, and add a test asserting the
   clauses that make it a *constraint* are still present. Prompts erode
   silently otherwise.
3. Parse with `provider.ExtractJSON` — never `json.Unmarshal` on raw model
   output. Models fence, preface, and append.
4. Test it with a `fakeProvider` in your package's `_test.go`, feeding it the
   exact JSON your parser expects. Cover the malformed-output path too: it is
   the one that happens in production.

## Commit messages

Imperative mood, scoped by package:

```
provoke: require a reason to dismiss

An empty dismissal is indistinguishable from not reading the objection,
which defeats the mechanism. Both Engage and Dismiss now reject blank
input and the TUI prompts for it.
```

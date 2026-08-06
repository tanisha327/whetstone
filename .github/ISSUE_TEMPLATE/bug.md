---
name: Bug report
about: Something behaves differently from how it is documented
title: "[bug] "
labels: bug
---

## What happened

<!-- What did you see? Paste the exact error text if there was one — errors are
     already scrubbed of your API key before they reach the screen. -->

## What you expected instead

<!-- One sentence. If the README or a doc comment says otherwise, link it. -->

## Steps to reproduce

<!-- Number them, and start from a fresh workspace if you can. The smallest
     sequence that still shows the problem is the most useful. -->

```sh
whetstone -web
```

1.
2.
3.

## Which front end

- [ ] Browser (`whetstone -web`)
- [ ] Terminal (`whetstone`)

## How often

- [ ] Every time
- [ ] Sometimes — roughly how often:
- [ ] Once, and I cannot reproduce it

## Environment

Paste the output of `whetstone -check` below. It reports the credential source,
endpoint and model, and never prints the key itself:

```text
(paste here)
```

| | |
|---|---|
| `whetstone -version` | |
| `go version` | |
| OS and version | |
| Browser and version (for `-web`) | |
| Terminal and `$TERM` (for the TUI) | |

## Browser console

<!-- For anything in the browser UI, open the developer console (Cmd+Option+J on
     Chrome, Cmd+Option+K on Firefox) and paste any red errors.

     This matters more than it sounds: several past bugs were a single
     exception early in one handler, which made every button in that area look
     dead while the rest of the page worked fine. -->

```text
(paste here, or write "none")
```

## Anything else

<!-- A screenshot for layout problems. If a workspace file is involved you can
     attach it — but read it first: it holds your notes and excerpts from your
     source documents. -->

## Before you post

- [ ] No API key appears anywhere in this issue, including in pasted output
- [ ] I am on the latest `main`, or I have said which version I am on

---
name: Feature request
about: Something you cannot do today
title: "[feature] "
labels: enhancement
---

## The problem

<!-- What can you not do? Describe the situation you are stuck in, not the
     feature you have in mind. "I keep losing track of which sections I have
     already argued from" is more useful than "add a progress bar". -->

## Proposal

<!-- What you would build. A rough sketch of the interaction is enough — where
     it lives in the UI, what the user does, what they see back. -->

## Does this keep the tool honest?

Whetstone's value comes from what it refuses to do. A feature that makes the
work faster by making the user think less is a regression here, however good it
looks in a demo. Please answer these:

- **Does it preserve material engagement?** Does the user still read, decide,
  and write the load-bearing parts themselves?
- **Does it add resistance or remove it?** Removing friction is not
  automatically good. Which friction, and was it doing work?
- **Does it open a free-text path to the model?** If so, read
  [`docs/adr/0001-no-chat-box.md`](../../docs/adr/0001-no-chat-box.md) and argue
  against it explicitly.
- **Would it move the authorship figure without moving the thinking?** A feature
  that games the engagement metric is worse than no feature.

## Alternatives considered

<!-- What else would solve the same problem, and why is this better? "Nothing"
     is a valid answer if the problem admits one obvious fix. -->

## Does it need a new model call?

- [ ] No — it works with what is already there
- [ ] Yes — and I have read
      [adding a model call](../../README.md#adding-a-model-call)

<!-- If yes: which `provider.Purpose`, what does the prompt constrain the model
     from doing, and what does the response schema look like? -->

## Prior art

<!-- Tools for Thought research, another tool that does this well, or a paper.
     Optional, but it is the fastest way to make a case. -->

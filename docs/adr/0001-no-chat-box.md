# ADR 0001: No chat box

**Status:** Accepted
**Date:** 2026-07-30

## Context

The obvious way to add a language model to a reading and writing tool is a chat
pane. It is cheap to build, users know how to use it, and it can do anything.

It is also the mechanism by which the tool would fail at its purpose.

A chat box makes the model a general-purpose oracle sitting next to your work. The
cost of asking it to do the next step is always lower than the cost of doing the
next step. That gradient runs one way, and over a session it moves the user from
*author* to *reviewer of a draft they did not write* — the failure mode described
in Advait Sarkar's TED talk as becoming "a professional validator of a robot's
opinions", and measured in the research as fewer ideas, less critical thinking,
and worse recall.

The relevant finding is not that AI output is bad. It is that **material
engagement is where the thinking happens**, and an open-ended prompt box is a
frictionless way to skip it.

## Decision

Whetstone has no chat interface. The model is reachable only through four fixed
affordances, each attached to material the user selected:

| Affordance | Input | Output shape | Cannot |
|---|---|---|---|
| Lens | one section | orientation + key points + relevance | draw conclusions |
| Section provocation | one passage | 1–2 objections, each ending in a question | rewrite, praise, agree |
| Outline provocation | one node of *your* argument | 1–2 objections | restructure the outline |
| Draft | one node's notes + citations | one paragraph | add claims not in the notes |

Each is a distinct `provider.Purpose` with its own system prompt, temperature,
and response schema. There is no free-text path to the model, and no way to ask
it a question of your own devising.

## Consequences

**Good**

- The user cannot delegate the task, because there is no interface for
  delegating the task. The floor on their engagement is structural.
- Every model call has a known shape, so responses can be schema-validated,
  cached, costed, and tested. `provider.Purpose` is a complete enumeration of
  the ways this program may talk to a model, which is only meaningful because
  the surface is closed.
- Prompts are reviewable artefacts in version control rather than whatever the
  user typed.
- The critic can be forbidden from agreeing. In a chat box that constraint is
  one "actually, just tell me the answer" away from being dissolved.

**Bad**

- Genuinely unanticipated needs have no escape hatch. When one recurs, the
  answer is a new affordance with its own prompt and schema, not a text box.
- Users arriving from ChatGPT-shaped tools will look for the chat pane and be
  briefly annoyed. The help screen says the omission is deliberate.
- More work per feature: four prompts and four parsers instead of one loop.

**Neutral**

- This constrains the product, not the model. A better model makes the lenses
  sharper and the objections harder to dismiss; it does not create pressure to
  open the surface up.

## Alternatives considered

**A chat box restricted to the current document.** Still open-ended, so the
gradient still runs toward delegation. Scoping the *input* does not constrain
the *ask*.

**A chat box that refuses to write prose.** Enforceable only by prompt, and
prompts are negotiable by the user on the other side of the box. The constraint
has to live in the interface, not in an instruction the interface can be talked
out of.

**Chat behind a flag, off by default.** Whichever way the flag defaults is the
product. A power-user escape hatch would become the main path within a week, and
every other constraint here would be load-bearing on nothing.

## References

- Advait Sarkar, *How to Stop AI from Killing Your Critical Thinking*, TED.
  https://www.youtube.com/watch?v=3lPnN8omdPA
- Microsoft Research Cambridge, Tools for Thought.

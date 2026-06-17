# Archiving agent conversations

Each canvas has a single **active** agent conversation per user. Over time a
conversation can drift, accumulate a failed attempt, or simply move on to a new
topic — and because the agent replays the conversation so far on every turn,
that history keeps influencing new requests.

**Archiving** lets you freeze the current conversation and start a fresh one
with a clean slate, while keeping the old conversation around to read later.

## Archive the current conversation

In the agent panel header, click the **Archive** button (top-right). The current
conversation is:

- given a short **title** — a one-line summary of what it was about (see
  [Titles](#titles) below);
- frozen and moved into the archive;
- replaced by a new, **empty** active conversation with no prior context.

Archiving is only available when the agent is idle — stop a running agent first.
It is **not** destructive: nothing is deleted, and the archived conversation
stays available in the drawer.

## Browse archived conversations

Click the **history** button (top-left) to open the **archived conversations**
drawer. It lists this canvas's archived conversations, newest first, as
`DD/MM/YY · title`, and is paginated when there are many.

Select any entry to open it **read-only**: you can scroll the full transcript,
but there's no composer — an archived conversation is a frozen record with no
live model context. Use **Back to current** in the banner to return to the
active conversation.

## Titles

The title is generated when you archive:

1. The agent provider is asked for a short (≤ 6-word) summary of the
   conversation.
2. If the provider can't summarize (or returns nothing), the title falls back to
   your **first message** in the conversation.
3. If there are no user messages, it's labelled `Untitled conversation`.

The date shown alongside the title is when the conversation started.

## Notes

- Scope is **per canvas, per user**: you only ever see your own conversations,
  and the archive is specific to the canvas you're on.
- There is always exactly **one** active conversation per canvas; archived ones
  never count against it.
- Resuming an archived conversation (continuing it with full context) is not
  supported yet — archived conversations are read-only.

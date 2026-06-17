# Custom / local AI provider for the agent

SuperPlane's in-canvas **agent** (the Ask / Build assistant) runs against a
pluggable backend. By default it uses **Anthropic's managed agents**; you can
instead point it at any **OpenAI-compatible Chat Completions** endpoint — a hosted
service or a **local** server such as vLLM, llama.cpp, Ollama, or LM Studio — to
keep agent traffic on infrastructure you control.

Provider selection is **installation-wide** (one backend per installation) and is
configured with environment variables. There is no per-organization or per-session
selection in this release.

## Configuration

The agent feature must be enabled for your installation (as for any backend):

```
AGENT_ENABLED=yes
```

Then select the OpenAI-compatible backend and point it at your endpoint:

| Variable | Required | Notes |
|---|---|---|
| `AGENT_PROVIDER` | yes | Set to `openai`. Unset (or `anthropic`) keeps the default managed-agents backend. |
| `AGENT_BASE_URL` | yes | Chat Completions base URL, e.g. `http://localhost:8000/v1`. The provider calls `<base>/chat/completions`. |
| `AGENT_MODEL` | yes | The model name to request, e.g. `Qwen/Qwen2.5-Coder-7B-Instruct` or `gpt-4o-mini`. |
| `AGENT_API_KEY` | no | Bearer token. **Optional** — omit it for unauthenticated local servers; when set it is sent as `Authorization: Bearer <key>`. |

If `AGENT_BASE_URL` or `AGENT_MODEL` is missing, the provider stays disabled (the
server logs `OpenAI-compatible agent provider disabled: set AGENT_BASE_URL and
AGENT_MODEL`) and the agent is unavailable.

### Example — a local vLLM / llama.cpp server

```
AGENT_ENABLED=yes
AGENT_PROVIDER=openai
AGENT_BASE_URL=http://10.0.0.5:8000/v1
AGENT_MODEL=Qwen/Qwen2.5-Coder-7B-Instruct
# AGENT_API_KEY left unset — the local server needs no auth
```

For an authenticated hosted endpoint, set `AGENT_BASE_URL` to its `/v1` and
`AGENT_API_KEY` to your key.

## How it works

An OpenAI-compatible endpoint is **stateless** — no server-side agent, session, or
tool loop (unlike Anthropic's managed agents) — so SuperPlane runs the agent loop
**client-side**:

- Each chat session keeps its own conversation history in the app process.
- A message streams a `/chat/completions` response; the assistant's reply is
  surfaced when the turn completes.
- In **Build** mode, the agent's tools are advertised to the model as function
  tools; when the model calls one, SuperPlane executes it and feeds the result back,
  continuing the turn until the model stops.

## Limitations (this release)

- **Installation-wide only** — one provider/model per installation; no per-org or
  per-session model selection yet.
- **A function-calling-capable model is required for Build mode** — tools rely on
  the endpoint's `tool_calls` support. Models without it still work for **Ask** mode
  but cannot edit the canvas.
- **No autonomous outcome loop** — the rubric-driven build/evaluate loop
  (`DefineOutcome`) is a managed-agent capability and is **not supported** on
  OpenAI-compatible providers (it returns an "unsupported" error; the related UI is
  inert).
- **In-memory sessions** — conversation history lives in the app process; after a
  restart a session is transparently recreated (a fresh conversation).
- **Minimal system prompt** — the assistant's instructions are briefer than the
  tuned managed-agent prompt; closer parity is a planned follow-up.

## Reverting to the default

Unset `AGENT_PROVIDER` (or set it to `anthropic`) and provide
`ANTHROPIC_API_KEY` / `ANTHROPIC_AGENT_ID` / `ANTHROPIC_ENVIRONMENT_ID` to return to
Anthropic's managed agents. The two backends are mutually exclusive per installation.

## See also

- [Agent internals](contributing/ai-agents.md) — how the provider seam and the
  agent service work (for contributors).

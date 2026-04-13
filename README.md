# Vikunja MCP

Give AI agents persistent memory and project tracking backed by a self-hosted [Vikunja](https://vikunja.io) instance.

Instead of stuffing yesterday's context back into every prompt, the agent stores facts once and queries only what it needs. Memories survive across sessions. Token costs drop. Multiple agents (local and remote) can share the same store.

> Go port of [acidvegas/vikunja-mcp](https://github.com/Crank-Git/vikunja-mcp). Original Python implementation and full design rationale live there.

## Requirements

- A running [Vikunja](https://vikunja.io) instance
- A Vikunja API token (Settings → API Tokens)
- Go 1.21+ **or** a pre-built binary

## Installation

### Option A — `go install` (permanent, recommended)

```
go install github.com/Crank-Git/vikunja-mcp@latest
```

Then add to Claude Code:

```
claude mcp add --transport stdio --scope user vikunja \
  --env VIKUNJA_URL=https://your-vikunja-host \
  --env VIKUNJA_TOKEN=tk_yourtoken \
  -- vikunja-mcp
```

### Option B — `go run` (no install step)

```
claude mcp add --transport stdio --scope user vikunja \
  --env VIKUNJA_URL=https://your-vikunja-host \
  --env VIKUNJA_TOKEN=tk_yourtoken \
  -- go run github.com/Crank-Git/vikunja-mcp@latest
```

### Option C — build from source

```
git clone https://github.com/Crank-Git/vikunja-mcp
cd vikunja-mcp
go build -o vikunja-mcp .
```

## Configuration

| Variable        | Required | Description                           |
| --------------- | -------- | ------------------------------------- |
| `VIKUNJA_URL`   | yes      | Base URL of your Vikunja server       |
| `VIKUNJA_TOKEN` | yes      | Personal API token (`tk_...`)         |

Both are read from environment variables. The MCP host (Claude Code, Claude Desktop, etc.) injects them at launch — no config file needed.

## Client Configuration

The server uses stdio transport. Any MCP client that can launch a subprocess works.

### Claude Code

```
claude mcp add --transport stdio --scope user vikunja \
  --env VIKUNJA_URL=https://your-vikunja-host \
  --env VIKUNJA_TOKEN=tk_yourtoken \
  -- vikunja-mcp
```

### Claude Desktop (`~/Library/Application Support/Claude/claude_desktop_config.json`)

```json
{
  "mcpServers": {
    "vikunja": {
      "command": "vikunja-mcp",
      "env": {
        "VIKUNJA_URL": "https://your-vikunja-host",
        "VIKUNJA_TOKEN": "tk_yourtoken"
      }
    }
  }
}
```

### Cursor (`.cursor/mcp.json`)

```json
{
  "mcpServers": {
    "vikunja": {
      "command": "vikunja-mcp",
      "env": {
        "VIKUNJA_URL": "https://your-vikunja-host",
        "VIKUNJA_TOKEN": "tk_yourtoken"
      }
    }
  }
}
```

## How It Works

```
  Vikunja (self-hosted)
         |
         | HTTP
         |
   vikunja-mcp  ←── MCP tools generated from Vikunja's OpenAPI spec
         |
        stdio
         |
   Claude Code / Claude Desktop / local LLMs
```

On startup the server fetches Vikunja's OpenAPI spec, filters it down to a curated set of safe endpoints, and registers each one as an MCP tool. The agent calls tools; the server forwards them to Vikunja's REST API and returns the result.

## How Information Is Stored

Vikunja's primitives map cleanly onto agent memory:

| Vikunja concept | How the agent uses it                                                     |
| --------------- | ------------------------------------------------------------------------- |
| **Project**     | Long-lived container. One per repo, one for general memory. Never deleted. |
| **Task**        | A single memory entry or work item. Title, description (markdown), labels, priority, due date, comments, attachments. |
| **Label**       | Namespaced tag for recall: `topic:postgres`, `kind:decision`, `lang:go`. Labels have numeric IDs used in filter expressions. |
| **View/Bucket** | Kanban columns for work-in-progress: Todo → In Progress → Review → Done. |

### The Memory project

One project titled **Memory** holds general long-term facts that don't belong to any specific repo. The agent creates it on first use and reuses it forever. Everything tagged aggressively so recall stays precise.

### Per-repository projects

For code work, one Vikunja project per repository (title = repo name). Standard kanban layout set up on first touch. "What was I working on?" at the start of a session becomes a single filtered query instead of a context dump.

## How Information Is Recalled

The agent filters tasks rather than loading everything:

| Intent | Filter |
| ------ | ------ |
| Open todos in a repo | `project = 42 && done = false` |
| High-priority bugs | `labels in 3,7 && done = false` |
| Everything about Alice | `labels = 5` |
| Decisions in the last month | `labels = 8 && created > "2026-03-12"` |
| Anything mentioning postgres | `title like "postgres" \|\| description like "postgres"` |

Five tasks of markdown is hundreds of tokens. A wall of pasted context is tens of thousands.

## The Instructions Payload

When an agent connects, the server sends a usage guide as part of the MCP `initialize` handshake. It defines:

- Label namespace conventions (`topic:`, `kind:`, `lang:`, `project:`, `area:`, `source:`, `status:`)
- Memory project and per-repo project conventions
- Filter syntax and examples
- Safety rules (confirm before destructive ops, never store credentials)

Every agent that connects reads the same rules and produces the same structure. Memories written by a local LLM on Monday are findable by Claude Code on Friday.

The guide lives in [`instructions.txt`](instructions.txt) and is embedded in the binary at build time. Edit it to add your own label namespaces or conventions, then rebuild.

## Token Savings

Three compounding effects:

1. **Filtered recall** — the agent pulls 2–5 relevant tasks rather than pasting a wall of history into every prompt
2. **Session continuity** — memory survives restarts; no re-explaining yesterday's work every morning
3. **Free labor** — routine memory writes and lookups run on a local model; frontier model sessions stay focused on reasoning

---

###### Mirrors: [SuperNETs](https://git.supernets.org/acidvegas/) · [GitHub](https://github.com/acidvegas/) · [GitLab](https://gitlab.com/acidvegas/) · [Codeberg](https://codeberg.org/acidvegas/)

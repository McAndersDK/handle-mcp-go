# Setup

Works on macOS, Linux and Windows. Needs [uv](https://docs.astral.sh/uv/getting-started/installation/).

**1. Logins.** Save each store's login in your OS keychain (macOS Keychain, Windows Credential Locker or Linux Secret Service). It asks for email and password, checks them against the store, and saves them encrypted:

```sh
uvx --from git+https://github.com/NJohansen/handle-mcp handle-mcp login nemlig
uvx --from git+https://github.com/NJohansen/handle-mcp handle-mcp login bilkatogo
```

`handle-mcp logout <store>` removes it again. From a clone, use `uv run handle-mcp login nemlig`.

Other ways, checked in this order:
1. Env vars `HANDLE_<STORE>_EMAIL` and `HANDLE_<STORE>_PASSWORD` (e.g. `HANDLE_NEMLIG_EMAIL`).
2. The keychain (above). Recommended.
3. A file `~/.config/handle-mcp/<store>.json` with `{"email": "...", "password": "..."}`. Plain text on disk, so the server refuses it unless only you can read it (`chmod 600`). Create it with `umask 077` and an editor, not `echo`, which leaves the password in your shell history.

**Headless / AI agent.** No terminal and no keychain (a Linux server, a container, an agent that sets itself up)? Put the login in env vars for one command, which checks it against the store and writes `~/.config/handle-mcp/<store>.json` with `chmod 600`:

```sh
HANDLE_NEMLIG_EMAIL=you@example.com HANDLE_NEMLIG_PASSWORD='...' \
  uvx --from git+https://github.com/NJohansen/handle-mcp handle-mcp login nemlig --from-env
```

The env vars are only needed for that one command; the MCP server reads the file afterwards. `handle-mcp logout <store>` removes the file. Nothing is written when the login fails.

Without any login, `list_offers` still works for nemlig; `search_products` needs a login (nemlig's search answers 500 without one). BilkaToGo needs a login for everything: the store, prices and offers follow your delivery address.

**2. BilkaToGo:** choose home delivery (not pickup) once on bilkatogo.dk. The server reads the store and fees from your cart.

**3. Add it to your MCP client.** Claude Code:

```sh
claude mcp add handle -- uvx --from git+https://github.com/NJohansen/handle-mcp handle-mcp
```

Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "handle": {
      "command": "uvx",
      "args": ["--from", "git+https://github.com/NJohansen/handle-mcp", "handle-mcp"]
    }
  }
}
```

Then ask: *"Compare this week's list in nemlig and Bilka, and fill the cheapest cart."* Open the store's website to check the cart, choose a time and pay.

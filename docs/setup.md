# Setup

Works on macOS, Linux and Windows. Needs [uv](https://docs.astral.sh/uv/getting-started/installation/).

**1. Logins.** Save each store's login in your OS keychain (macOS Keychain, Windows Credential Locker or Linux Secret Service). It asks for email and password, logs in, and saves only the session the store gives back. The password is never saved ([ADR 0003](adr/0003-save-the-session-not-the-password.md)). When the session expires (nemlig: about once a year), the tool asks you to log in again:

```sh
uvx --from git+https://github.com/NJohansen/handle-mcp@v1.2.0 handle-mcp login nemlig
uvx --from git+https://github.com/NJohansen/handle-mcp@v1.2.0 handle-mcp login bilkatogo
```

`handle-mcp logout <store>` removes it again. From a clone, use `uv run handle-mcp login nemlig`.

Other ways, checked in this order:
1. Env vars `HANDLE_<STORE>_EMAIL` and `HANDLE_<STORE>_PASSWORD` (e.g. `HANDLE_NEMLIG_EMAIL`).
2. The keychain (above). Recommended.
3. A file `~/.config/handle-mcp/<store>.json`, written by `--from-env` below. It holds the session, not the password, and the server refuses it unless only you can read it (`chmod 600`). Windows has no file modes, so there it relies on your user profile's permissions ([ADR 0004](adr/0004-windows-has-no-file-modes.md)).

**Headless / AI agent.** No terminal and no keychain (a Linux server, a container, an agent that sets itself up)? Put the login in env vars for one command, which logs in and writes the session to `~/.config/handle-mcp/<store>.json` with `chmod 600`:

```sh
HANDLE_NEMLIG_EMAIL=you@example.com HANDLE_NEMLIG_PASSWORD='...' \
  uvx --from git+https://github.com/NJohansen/handle-mcp@v1.2.0 handle-mcp login nemlig --from-env
```

The env vars are only needed for that one command; the MCP server reads the file afterwards. Remove them again: the file has no password. `handle-mcp logout <store>` removes the file. Nothing is written when the login fails.

Without any login, `list_offers` still works for nemlig; `search_products` needs a login (nemlig's search answers 500 without one). BilkaToGo needs a login for everything: the store, prices and offers follow your delivery address.

**2. BilkaToGo:** choose home delivery (not pickup) once on bilkatogo.dk. The server reads the store and fees from your cart.

**3. Add it to your MCP client.** Claude Code:

```sh
claude mcp add handle -- uvx --from git+https://github.com/NJohansen/handle-mcp@v1.2.0 handle-mcp
```

Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "handle": {
      "command": "uvx",
      "args": ["--from", "git+https://github.com/NJohansen/handle-mcp@v1.2.0", "handle-mcp"]
    }
  }
}
```

Then ask: *"Compare this week's list in nemlig and Bilka, and fill the cheapest cart."* Open the store's website to check the cart, choose a time and pay.

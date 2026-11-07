# Setup

Works on macOS, Linux and Windows, on x64 or ARM64. No Python or `uv` needed.

**0. Install.** Either the prebuilt binary, or Go:

- Prebuilt: download the archive for your system from [the v1.2.0 release](https://github.com/McAndersDK/handle-mcp-go/releases/tag/v1.2.0), unpack `handle-mcp` (`handle-mcp.exe` on Windows), and put it somewhere on PATH or remember its absolute path.
- With Go 1.27.3 or newer:

```sh
go install github.com/McAndersDK/handle-mcp-go/cmd/handle-mcp@v1.2.0
```

The binary goes to your Go bin folder (`go env GOBIN`, or `$(go env GOPATH)/bin` when GOBIN is not set).

The binaries are not signed. macOS Gatekeeper may block the first run: allow it under System Settings, Privacy & Security, Open Anyway, or run `xattr -d com.apple.quarantine handle-mcp`. Windows SmartScreen may warn: choose "More info", then "Run anyway". `go install` avoids both.

Below, `handle-mcp` means the binary. Use its absolute path if it is not on PATH.

**1. Logins.** Save each store's login in your OS keychain (macOS Keychain, Windows Credential Locker or Linux Secret Service). It asks for email and password, logs in, and saves only the session the store gives back. The password is never saved ([ADR 0003](adr/0003-save-the-session-not-the-password.md)). When the session expires (nemlig: about once a year), the tool asks you to log in again:

```sh
handle-mcp login nemlig
handle-mcp login bilkatogo
handle-mcp login rema1000
```

The password prompt needs a real terminal. On Windows, use PowerShell, not Git Bash.

`handle-mcp logout <store>` removes it again. From a clone, use `go run ./cmd/handle-mcp login nemlig`.

Logins saved by the old Python version are not read, so log in again. Its Windows keychain entries can be removed with `cmdkey /delete:handle-mcp` or `cmdkey /delete:<store>@handle-mcp`. Env vars and the config file still work.

Other ways, checked in this order:
1. Env vars `HANDLE_<STORE>_EMAIL` and `HANDLE_<STORE>_PASSWORD` (e.g. `HANDLE_NEMLIG_EMAIL`).
2. The keychain (above). Recommended.
3. A file `~/.config/handle-mcp/<store>.json`, written by `--from-env` below. It holds the session, not the password, and the server refuses it unless only you can read it (`chmod 600`). Windows has no file modes, so there it relies on your user profile's permissions ([ADR 0004](adr/0004-windows-has-no-file-modes.md)).

**Headless / AI agent.** No terminal and no keychain (a Linux server, a container, an agent that sets itself up)? Put the login in env vars for one command, which logs in and writes the session to `~/.config/handle-mcp/<store>.json` with `chmod 600`:

```sh
HANDLE_NEMLIG_EMAIL=you@example.com HANDLE_NEMLIG_PASSWORD='...' \
  handle-mcp login nemlig --from-env
```

The env vars are only needed for that one command; the MCP server reads the file afterwards. Remove them again: the file has no password. `handle-mcp logout <store>` removes the file. Nothing is written when the login fails.

Without any login, `list_offers` still works for nemlig; `search_products` needs a login (nemlig's search answers 500 without one). BilkaToGo needs a login for everything: the store, prices and offers follow your delivery address. REMA 1000 needs a login and at least one chosen store for everything.

**2. REMA 1000:** pickup only (Køb & Hent). `handle-mcp login rema1000` logs in, asks for a search text (a postcode or part of a store's name or city) and lists the matching REMA 1000 stores with postcode, city and whether Køb & Hent is active (`ja`/`nej`). Pick one or more by number, the one you prefer first, for example `2,1`. A store without Køb & Hent can be picked too: REMA 1000 switches on by itself the day one of your stores gets it (it is checked again once an hour), and uses the first active store in your order. REMA 1000 has no store search we know of, so handle-mcp reads the store list (up to 10 pages of 100) and filters it on your machine. The list is shown on your own terminal only. The stores are saved with the session (store ids, in your order), on your machine only; nothing is set at REMA. To change them, log in again. An older save with one store still works. `HANDLE_REMA1000_STORE_ID` overrides the saved stores with a comma-separated list of store ids in your order (`9100,9200`; the login list shows each id). With `--from-env` and no terminal, set `HANDLE_REMA1000_STORE_ID` too. The session lasts about an hour and renews itself while REMA accepts the saved session; when it does not, the tool asks you to log in again. The basket is the shopping list "handle-mcp YYYY-MM-DD" (made on first use, never as your primary list). Choose the pickup time and pay on rema1000.dk.

**3. BilkaToGo:** choose home delivery or pickup once on bilkatogo.dk. Without an address the server says so. The server reads the store, the fees and the mode (`delivery_mode`) from your cart. The time and the pickup store are yours to choose on the website.

**4. Add it to your MCP client.** Claude Code:

```sh
claude mcp add handle -- /absolute/path/to/handle-mcp
```

On Windows: `claude mcp add handle -- C:\path\to\handle-mcp.exe`

Claude Desktop (`claude_desktop_config.json`). Desktop may not see your PATH, so use the absolute path, with no args:

```json
{
  "mcpServers": {
    "handle": {
      "command": "/absolute/path/to/handle-mcp"
    }
  }
}
```

On Windows, escape the backslashes in JSON: `"command": "C:\\path\\to\\handle-mcp.exe"`.

Then ask: *"Compare this week's list in nemlig and Bilka, and fill the cheapest cart."* Open the store's website to check the cart, choose a time and pay.

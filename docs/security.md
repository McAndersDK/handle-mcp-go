# Security and privacy

- **The human always pays.** Only `set_cart_quantities` and `add_recipe_to_cart` write, and only cart quantities (both through the store's `set_quantities`). The stores' own "add recipe" and "clear cart" calls are forbidden in `tests/test_safety.py`.
- **Personal data stays on your machine.** Every tool answer goes through `privacy.scrub()`. Name, address, phone, email, door code and delivery notes are replaced with `[fjernet af handle-mcp]` before the model sees them.
- **Logins** live in your OS keychain, env vars or `~/.config/handle-mcp/` (chmod 600). Never in code, config in git or test fixtures. `handle-mcp login` never shows or logs the password.
- **stdout is JSON-RPC.** Logs go to stderr. httpx request logging is off, because some store URLs contain account ids.
- **Store text reaches the model.** Product names, offer texts and cart messages come from the stores and land in the model's context. A store (or a seller) could put instructions in them. The server cannot order or pay, so the damage is limited to a wrong cart. Check the cart before you pay.
- **Versions.** `git+https://...` installs the newest `main`. Pin a tag when one exists: `git+https://github.com/NJohansen/handle-mcp@v0.1.0`.
- Use one client at a time per store. Close the store's app or website before an agent changes the cart.
- Be kind to the stores: the server makes few calls, mostly one at a time per store, and retries only on timeouts, network errors, 429 and 5xx.

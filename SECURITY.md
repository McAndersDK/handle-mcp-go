# Security

handle-mcp handles store logins and reads your cart and orders. Report a security problem in private, not in a public issue.

**How:** open a private report on GitHub: *Security* → *Report a vulnerability* on this repo.

**What counts:** a way to order, pay or change the address through the server; personal data that leaves the server (answers, logs, errors, fixtures); logins written to disk in clear text; a store response that can run code.

**What does not:** the stores' private APIs changing. That is a bug, open an issue.

Only the newest `main` is supported.

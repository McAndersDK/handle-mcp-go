# Troubleshooting

Errors from a store come back to the agent as one short Danish line, e.g. `nemlig: ...`. One store failing never stops the others.

| Message | Fix |
|---|---|
| `Intet login til <store>` | Run `handle-mcp login <store>` in a terminal. |
| `afviser login. Tjek email og kodeord.` | Wrong email or password. Run `handle-mcp login <store>` again. |
| `nemlig.com har kø lige nu (queue-it)` | nemlig has a queue. Try again in a few minutes. |
| `Kurven er sat til afhentning` (BilkaToGo) | Choose home delivery (not pickup) once on bilkatogo.dk. |
| `uventet fejl (...)` | The store's API may have changed. The log on stderr has the details. Please open an issue. |

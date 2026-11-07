# Troubleshooting

Errors from a store come back to the agent as one short Danish line, e.g. `nemlig.com afviser login. ...`. One store failing never stops the others.

| Message | Fix |
|---|---|
| `Intet login til <store>` | Run `handle-mcp login <store>` in a terminal. |
| `<store> afviser login. Tjek email og kodeord.` | Wrong email or password. Run `handle-mcp login <store>` again. |
| `nemlig.com har kø lige nu (queue-it)` | nemlig has a queue. Try again in a few minutes. |
| `BilkaToGo viser ikke, om kurven er sat til hjemlevering eller afhentning.` | Choose home delivery or pickup once on bilkatogo.dk. |
| `Ingen leveringsadresse valgt i BilkaToGo.` | Choose home delivery or pickup on bilkatogo.dk, then try again. |
| `<store>: uventet fejl (...). Se serverens log.` | The store's API may have changed, or there is a bug. The log on stderr has the details. Please open an issue. |

# Troubleshooting

Errors from a store come back to the agent as one short Danish line, e.g. `nemlig.com afviser login. ...`. One store failing never stops the others.

| Message | Fix |
|---|---|
| `Intet login til <store>` | Run `handle-mcp login <store>` in a terminal. |
| `<store> afviser login. Tjek email og kodeord.` | Wrong email or password. Run `handle-mcp login <store>` again. |
| `nemlig.com har kø lige nu (queue-it)` | nemlig has a queue. Try again in a few minutes. |
| `BilkaToGo viser ikke, om kurven er sat til hjemlevering eller afhentning.` | Choose home delivery or pickup once on bilkatogo.dk. |
| `Ingen leveringsadresse valgt i BilkaToGo.` | Choose home delivery or pickup on bilkatogo.dk, then try again. |
| `REMA 1000: ingen butik valgt. Kør: handle-mcp login rema1000` | No store is saved. Run `handle-mcp login rema1000`, search and pick one or more stores. |
| `REMA 1000: ingen af dine valgte butikker har Køb & Hent endnu. REMA bruges, så snart en af dem får det.` | Nothing to fix. None of your stores has Køb & Hent yet. It is checked again once an hour, and REMA 1000 switches on by itself when one gets it. |
| `REMA 1000: dine valgte butikker findes ikke længere. Vælg igen: handle-mcp login rema1000` | The saved stores are gone from REMA's list. Run `handle-mcp login rema1000` again and pick new ones. |
| `REMA 1000 har kø eller et bot-tjek lige nu.` | REMA has a queue or a bot check. handle-mcp never works around it: wait and try again. |
| `<store>: uventet fejl (...). Se serverens log.` | The store's API may have changed, or there is a bug. The log on stderr has the details. Please open an issue. |

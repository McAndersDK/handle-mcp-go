# 0008: REMA 1000's basket is a dated shopping list

REMA 1000's webshop is pickup only (Køb & Hent). The webshop's own basket is a cart that is tied to a pickup store, a pickup time and a coupon, and it is paid through payment jobs. All of that is the human's: choosing a store or a time counts as changing the address (ADR 0006), and paying is ADR 0001. So handle-mcp never touches the cart. It uses what REMA's own app and webshop also have: shopping lists.

## The basket is a list called "handle-mcp YYYY-MM-DD"

- `get_cart` reads the list named exactly `handle-mcp <date>`, with the date in Copenhagen time, with `GET v3/users/{id}/shopping-lists` and then `GET v2/shoppinglists/{id}?with=items`. Never `with=users`: it holds names and emails. Lines with `bought: true` are left out. No list for today is an empty cart whose warning says which list it would use.
- `set_cart_quantities` creates today's list when it is missing, with `POST v2/shoppinglists` and `{name, primary: false, items: []}`, and reads it back. It adds a line with `POST .../items` (`store_id: 1` is fixed exactly as REMA's own SDK sends it; it is not a store choice), changes one with `PATCH .../items/{id}` (`{amount}`, and `bought: false` for a line that was bought in the app) and removes one with `PATCH v2/shoppinglists/{id}` and `{items: [{id, delete: true}]}`. The amounts are absolute, and every amount is read back, as in the other stores.
- A new list is never primary. If REMA made it primary anyway (REMA decides, and the account may have no other list), the result says `REMA gjorde handle-mcp-listen primær. Gør din egen liste primær igen i appen.` and the adapter does not PATCH it. A new date is a new list: yesterday's list stays in the app for the human to use or delete.

## Safe writes

- **Two POSTs are never retried.** `POST v2/shoppinglists` (create today's list) and `POST .../items` (add a line) are not absolute: a repeat after a lost answer could make a second list or a second line. The shared sender retries on timeouts, network errors, 429 and 5xx because every other write sets an absolute quantity; these two ask it not to (`NoRetry`). After a timeout, a network error, a 429 or a 5xx the adapter reads the state again (the list of lists, or today's list) and POSTs again only if the list or the line is still not there, at most three times. If the first POST did land, the adapter uses it and only sets the amount. The `PATCH` calls set absolute amounts and keep the sender's retries.
- **One write at a time.** `SetQuantities` holds a store-level lock from finding today's list to the read-back, so two calls on a new day create one list, not two.
- **Duplicates are never guessed.** If more than one list is named `handle-mcp <date>`, `get_cart` and `set_cart_quantities` answer `Der er flere lister med navnet ... Slet de ekstra i appen` and write nothing. If a product is on more than one line of today's list, its quantity is the sum of the lines (in `get_cart` too, as one line); setting it sets the first line and removes the extras.

## The pickup store gate

REMA only works with a known store that has Køb & Hent. `handle-mcp login rema1000` logs in, lists the stores with `is_click_and_collect_active` (paged) and asks the human to choose one by number. The store id is saved on this machine with the session (keychain, or the `--from-env` file); `HANDLE_REMA1000_STORE_ID` overrides it. This is a local choice: nothing is set at REMA, and no `PATCH` to the cart exists.

Before any method does any work, the adapter checks once per process that the store is still on the Køb & Hent list. If not, every method answers `REMA 1000: ingen afhentningsbutik med Køb & Hent er valgt. Kør: handle-mcp login rema1000`, or, when the store lost Køb & Hent, `REMA 1000: din afhentningsbutik har ikke Køb & Hent længere. Vælg en anden: handle-mcp login rema1000`. A failed check is not cached, so a fixed setting works without a restart.

`pickup_store` is the store's name only. A store's phone, address and opening hours are never decoded; the user's email, phone, name, birthday and photo are never decoded either.

## What is forbidden

The adapter never: touches another list, deletes a list, changes which list is primary, shares or invites to a list, imports a vendor's basket, calls the cart endpoints (any method), calls the payment jobs, or sets a store, a time or a coupon. `internal/safety` greps the non-test code for `v3/cart`, `v3/jobs`, `vendor-baskets`, `checkoutCallback`, a `primary` set to true (JSON or a Go literal) and a list delete (`MethodDelete` or any `"DELETE"` string). `/payments` is already covered by `/payment`. The read-only live tests refuse every write, and the one write test (`HANDLE_LIVE_WRITE=1`) refuses a write to anything but today's list, checks every request body (a list PATCH holds only `items` with `id` and `delete:true`, an item PATCH only `amount` and `bought:false`, an item POST only `amount`, `store_id` and `store_item_id`; `primary` and `name` only in the create body), probes with a product that is not on today's list at all, and checks that today's list is back to its exact before-state (bought flags too) and the human's other lists did not change.

## Why

- The list is the one place REMA lets a program write that the human can see and clear, and that cannot place an order, pick a time or take money.
- A date in the name keeps handle-mcp away from the human's own lists, and a stale basket from last week does not grow. It costs one extra list per day of use.
- The webshop's SDK does the same calls, so they are what REMA expects to see.
- Pickup is a mode (ADR 0006); the store is the one thing REMA cannot do without, so it is asked once, locally.

## Session

The login is the site's own OAuth login (one POST of email and password; the password is then forgotten, ADR 0003). The session saved is the two cookies the site callback sets, `access_token` (a JWT, about an hour) and `refresh_token`, and the chosen store id. The access token is renewed with `POST https://shop.rema1000.dk/api/auth/refresh`, the call the webshop makes; that is an auth call, not a write, like nemlig's token renewal. A rotated refresh cookie is saved again where the session was found. When REMA refuses the renewal, the tool says `Login til rema1000 er udløbet` and the human logs in again. How long the refresh cookie lives is set by REMA and is not known from the site's code.

## Consequences

- The cart in `get_cart` is not the webshop's cart. The human opens the list in the app or webshop, builds the order from it and chooses time and pays there. The cart answer says so.
- `get_orders` and `get_order` say "kan ikke det endnu": no order endpoint is known.
- `get_delivery_info` has no chosen time. Its note says the time is chosen and paid at REMA. The fee and the minimum order are `null`: REMA gives none in the calls we use.
- Recipes come from REMA's public recipe API (no login); the packs for a recipe follow `persons`.

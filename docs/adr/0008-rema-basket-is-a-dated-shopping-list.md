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

REMA only works while at least one store the human chose has Køb & Hent (`is_click_and_collect_active`). Køb & Hent exists in only a few places, so the human can choose the local stores anyway, and REMA switches on by itself the day one of them gets it.

- **Choosing.** `handle-mcp login rema1000` logs in and asks for a search text (a postcode or part of a name or city). REMA's `GET v3/stores` is the only call, and no search parameter is known for it, so it is paged (10 pages of 100 at most) and filtered on this machine. Each line shows number, name, postcode and city, and Køb & Hent `ja`/`nej`, on the human's own terminal only (never in a tool answer or a log). The human picks one or more numbers in order of preference (`2,1`); whether Køb & Hent is active does not matter. The store ids are saved in that order as `store_ids` (comma-separated) with the session (keychain, or the `--from-env` file). A session saved with one `store_id` is read as a list of one. `HANDLE_REMA1000_STORE_ID` is a comma-separated list that overrides the saved one. This is a local choice: nothing is set at REMA, and no `PATCH` to the cart exists.
- **Using.** The first chosen store, in the human's order, that exists and has Køb & Hent is the pickup store, for slots, `pickup_store` and everything else. A chosen id that no longer exists is skipped. `get_delivery_info` adds a line when a store ranked higher exists but has no Køb & Hent yet; it names only the active store.
- **The hourly check.** The store list is read at the first call and again when an hour has passed, whatever the last verdict was: so REMA switches on when a chosen store gets Køb & Hent and off when it loses it, without a restart. The paging stops as soon as every chosen id has been seen, so it is a few calls per hour at most. A call that fails (network, login) is not a verdict; it is tried again on the next call. A change of the chosen stores (the env var, a new login) is checked at once.
- **The messages** carry no store names: `REMA 1000: ingen butik valgt. Kør: handle-mcp login rema1000`; `REMA 1000: ingen af dine valgte butikker har Køb & Hent endnu. REMA bruges, så snart en af dem får det.`; `REMA 1000: dine valgte butikker findes ikke længere. Vælg igen: handle-mcp login rema1000`.

`pickup_store` is the active store's name only. A store's phone, address and opening hours are never decoded; the postcode and city are read only for the login list on the human's terminal. The user's email, phone, name, birthday and photo are never decoded either.

## What is forbidden

The adapter never: touches another list, deletes a list, changes which list is primary, shares or invites to a list, imports a vendor's basket, calls the cart endpoints (any method), calls the payment jobs, or sets a store, a time or a coupon. `internal/safety` greps the non-test code for `v3/cart`, `v3/jobs`, `vendor-baskets`, `checkoutCallback`, a `primary` set to true (JSON or a Go literal) and a list delete (`MethodDelete` or any `"DELETE"` string). `/payments` is already covered by `/payment`. The read-only live tests refuse every write, and the one write test (`HANDLE_LIVE_WRITE=1`) refuses a write to anything but today's list, checks every request body (a list PATCH holds only `items` with `id` and `delete:true`, an item PATCH only `amount` and `bought:false`, an item POST only `amount`, `store_id` and `store_item_id`; `primary` and `name` only in the create body), probes with a product that is not on today's list at all, and checks that today's list is back to its exact before-state (bought flags too) and the human's other lists did not change.

## Why

- The list is the one place REMA lets a program write that the human can see and clear, and that cannot place an order, pick a time or take money.
- A date in the name keeps handle-mcp away from the human's own lists, and a stale basket from last week does not grow. It costs one extra list per day of use.
- The webshop's SDK does the same calls, so they are what REMA expects to see.
- Pickup is a mode (ADR 0006); a store with Køb & Hent is the one thing REMA cannot do without, so the stores are asked once, locally, and checked every hour.

## Session

The login is the site's own OAuth login (one POST of email and password; the password is then forgotten, ADR 0003). The session saved is the two cookies the site callback sets, `access_token` (a JWT, about an hour) and `refresh_token`, and the chosen store ids (`store_ids`). The access token is renewed with `POST https://shop.rema1000.dk/api/auth/refresh`, the call the webshop makes; that is an auth call, not a write, like nemlig's token renewal. A rotated refresh cookie is saved again where the session was found. When REMA refuses the renewal, the tool says `Login til rema1000 er udløbet` and the human logs in again. How long the refresh cookie lives is set by REMA and is not known from the site's code.

## Consequences

- The cart in `get_cart` is not the webshop's cart. The human opens the list in the app or webshop, builds the order from it and chooses time and pays there. The cart answer says so.
- `get_orders` and `get_order` say "kan ikke det endnu": no order endpoint is known.
- `get_delivery_info` has no chosen time. Its note says the time is chosen and paid at REMA. The fee and the minimum order are `null`: REMA gives none in the calls we use.
- Recipes come from REMA's public recipe API (no login); the packs for a recipe follow `persons`.

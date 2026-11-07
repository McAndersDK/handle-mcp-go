# 0006: Pickup is a delivery mode

BilkaToGo carts can be set to pickup in a store. handle-mcp reads pickup carts and lists their pickup times. It never chooses a store, a time or a mode. The scope is no longer home delivery only.

## Modes

`delivery_mode` is `"home"`, `"pickup"` or `null`. `pickup_store` is the store's name or `null`. Both keys are always in the answer of `get_cart`, in both parts of `get_delivery_info` (`delivery` and `terms`) and in each store of `compare_basket`; `pickup_store` is also on each delivery slot. A missing fact is `null`, never omitted and never guessed.

BilkaToGo decides from the cart's `deliveryAddress`, in one place (`deliveryMode` in `stores/bilkatogo/delivery.go`):

- **home:** `dawa_uid` or `recipient_name` is present.
- **pickup:** `sid` is present, `dawa_uid` and `recipient_name` are empty, and `name` starts with `Bilka`.
- **unknown:** anything else. This fails closed.

The top-level `hd_available` does not tell the modes apart: a pickup cart has it `true` there and `false` inside `deliveryAddress` (docs/research.md, confirmed live 2026-11-05). Python treated a missing address as pickup. That was wrong, and Go does not.

On a home cart `deliveryAddress.name` is the customer's name. So the adapter reads `name` only when nothing says home, and `pickup_store` is set only in the pickup case. A home cart with its markers blanked is `unknown`, and the name is dropped. Only the address fields we need are decoded.

nemlig is home delivery only: `delivery_mode` is `"home"` and `pickup_store` is `null`. `home_delivery` is unchanged.

## Rule 1 still holds

Choosing a pickup store counts as changing the address (ADR 0001). So does choosing a pickup time or switching between home and pickup. No code does any of it. When the mode is unknown, the answer says `BilkaToGo viser ikke, om kurven er sat til hjemlevering eller afhentning. Vælg det på bilkatogo.dk.` and the human decides on the store's own site.

## Pickup answers

- No "choose home delivery" warning in the cart.
- `get_delivery_info` shows the chosen pickup time (hidden once it has ended) and `pickup_store`.
- `list_delivery_slots` lists pickup times with `kind: "Afhentning"`, `pickup_store` and the fee per slot. A `delivery_price` that is present and 0 is `"0.00"`; an absent one stays `null` with the note "Gebyret er ukendt". Pakkeservice stays in the cart's fees and totals.
- The terms (in `get_delivery_info`) show the same known fee: `delivery_fee` is `"0.00"` for a pickup cart whose `delivery_price` is 0, and `null` when the key is absent. `home_delivery` is `false` for pickup.
- `compare_basket` adds `Afhentning i <butik>: du henter selv varerne.` (or `Afhentning: butikken er ikke oplyst.`). When one `home` and one `pickup` store both priced the list, the summary ends with the cheapest store for each mode and `Turen til butikken er ikke regnet med.` `cheapest` stays money only. There is no sentence when all stores share a mode or any mode is `null`.

## Goldens

The goldens are the recorded Python answers and are not edited. `goldentest.DropAdded` removes exactly `delivery_mode` and `pickup_store` from the actual output, so the golden still proves everything else. Ten cases change because of pickup and are named exceptions with hand-written expectations (`golden_pickup_test.go`), compared with the new keys:

`cartpickup`, `termspickup`, `delivery_infopickup`, `slotspickup`, `cart_no_address`, `cart_minimal`, `delivery_info_no_address`, `search_no_address`, `search_address_without_sid`, `slots_not_home`.

If another case changes, the test fails: stop and look.

## Proof

The account used to test proves pickup live. The home path is proven by goldens and fixtures only, and a run on this account logs that. A run on a home account is needed to prove it live.

## Porting upstream fixes

Check these by hand when a fix touches them: `_is_home`, `NOT_HOME` and `delivery_slots` in `src/handle_mcp/stores/bilkatogo.py`, and the notes in the quote in `src/handle_mcp/compare.py`. Python still maps every non-home cart to `NOT_HOME`.

## Consequences

- New keys on four answers. Old skills that ignore unknown keys are unaffected (minor version).
- Tool descriptions for `get_cart`, `get_delivery_info`, `list_delivery_slots` and `compare_basket` change at the server phase.
- A new store with pickup uses the same two keys.

# 0002: Money has two decimals

Every Decimal in a tool answer is a string in kroner with exactly two decimals: `"39.00"`, `"0.00"`, `"12.95"`. `privacy.jsonable()` formats it, so no store file has to.

## Why

The stores send different exponents: nemlig `"0.0"` and `"32.0"`, BilkaToGo `"0"` and `"39.00"`. An agent comparing the two saw two formats for the same thing. Decimal strings stay exact; a float would not.

## Consequences

- A value with more than two decimals (a unit price such as `0.125`) is rounded half up to `"0.13"`. Øre is the smallest unit a store charges, so nothing is lost that a person could pay.
- A value Decimal cannot quantize (NaN, absurdly large) is shown as it is, never dropped.
- Money stays a string. Changing it to a number would be a major version.

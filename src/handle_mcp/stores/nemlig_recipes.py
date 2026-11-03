"""nemlig.com recipes: the products behind "Se varer og læg i kurv" and "Ugens udvalgte".

From nemlig's new web API (productbff), found 2026-11-03 (tests/fixtures/nemlig/README.md).
Read only: nemlig's own "add recipe to basket" is never called. The caller puts the
products in the cart with set_cart_quantities, after the human says yes.
"""

from __future__ import annotations

import asyncio
import re
from decimal import Decimal
from typing import Any

from handle_mcp.models import Recipe, RecipeDetails, RecipeItem, RecipeProduct
from handle_mcp.stores import StoreError

SITE = "https://www.nemlig.com"
BFF = "https://webapi.prod.knl.nemlig.it/productbff/api/web"
SLUG = re.compile(r"^[a-z0-9-]+$")


def recipe_slug(recipe: str) -> str:
    """'https://www.nemlig.com/maaltider/opskrift/kebabfad-98001303?x=1', the old
    '/opskrifter/kebabfad-98001303' or just 'kebabfad-98001303' → 'kebabfad-98001303'."""
    slug = recipe.strip().split("?")[0].split("#")[0].rstrip("/").rsplit("/", 1)[-1].lower()
    if not SLUG.match(slug):
        raise StoreError(f"Kender ikke opskriften {recipe!r}. Brug linket fra nemlig.com.")
    return slug


def category_path(category: str) -> str:
    """'Mad til flere dage' or 'måltidskasse' → nemlig's path: 'mad-til-flere-dage'."""
    path = category.strip().lower().replace(" ", "-")
    for a, b in (("æ", "ae"), ("ø", "oe"), ("å", "aa")):
        path = path.replace(a, b)
    if not SLUG.match(path):
        raise StoreError(f"Kender ikke kategorien {category!r}. Brug fx aftensmad eller frokost.")
    return path


def _product(item: dict) -> RecipeProduct:
    p = item.get("product") or {}
    campaign = next((c.get("text") for c in p.get("campaignLines") or [] if c.get("text")), None)
    return RecipeProduct(
        id=str(p["id"]),
        name=p.get("title", ""),
        price=Decimal(str(p.get("price") or 0)) / 100,  # øre
        quantity=int(item.get("quantity") or 1),
        size=p.get("description") or None,
        available=(p.get("availability") or {}).get("type", "Available") == "Available",
        offer_text=campaign,
    )


def _items(rows: list[dict], have_at_home: bool) -> list[RecipeItem]:
    return [
        RecipeItem(
            ingredient=row.get("ingredientTitle") or row.get("ingredientId", ""),
            product=_product(row["item"]),
            have_at_home=have_at_home,
            alternatives=[_product(a) for a in row.get("alternatives") or [] if a.get("product")],
        )
        for row in rows
        if (row.get("item") or {}).get("product", {}).get("id")
    ]


class RecipesMixin:
    """Recipe methods for NemligStore. Uses its logged-in `_api`."""

    name: str
    _api: Any
    _tags: dict[str, str]
    _tag_names: list[str]

    async def recipe(self, recipe_id: str, persons: int | None = None) -> RecipeDetails:
        slug = recipe_slug(recipe_id)
        params: dict[str, Any] = {"recipeId": slug, "maxProductsPerIngredient": 10}
        if persons:
            params["servings"] = persons
        details, dialog = await asyncio.gather(
            self._api("GET", f"{BFF}/meals/recipeDetails/{slug}"),
            self._api("GET", f"{BFF}/meals/recipeProductDialog", params=params),
        )
        details, dialog = details or {}, dialog or {}
        if "includedProducts" not in dialog:
            raise StoreError("nemlig-opskriften har ændret format.")
        items = _items(dialog["includedProducts"] or [], False)
        items += _items(dialog.get("selfSuppliedProducts") or [], True)
        limits = dialog.get("servingLimits") or details.get("servingLimits") or {}
        minutes = (details.get("durationTotal") or {}).get("from")
        return RecipeDetails(
            store=self.name,
            id=slug,
            name=dialog.get("title") or details.get("title", ""),
            url=f"{SITE}/maaltider/opskrift/{slug}",
            persons=int(dialog.get("servings") or details.get("servings") or persons or 0),
            min_persons=limits.get("minServings"),
            max_persons=limits.get("maxServings"),
            time=f"{minutes} min" if minutes else None,
            ingredients=[
                i["text"]
                for group in details.get("ingredients") or []
                for i in group.get("ingredients") or []
                if i.get("text")
            ],
            items=items,
            total=sum(
                (i.product.price * i.product.quantity for i in items if not i.have_at_home),
                Decimal(0),
            ),
        )

    async def featured_recipes(self, limit: int = 20) -> list[Recipe]:
        """nemlig's "Ugens udvalgte" (/maaltider), grouped by theme."""
        page = await self._api("GET", f"{BFF}/page", params={"path": "/maaltider"}) or {}
        blocks = [b for b in page.get("pageContent") or [] if "recipes" in b]
        if not blocks:
            raise StoreError("Fandt ingen opskrifter i nemligs 'Ugens udvalgte'.")
        return [
            Recipe(
                store=self.name,
                id=r["id"],
                name=r.get("title", ""),
                url=f"{SITE}/maaltider/opskrift/{r['id']}",
                group=(b.get("header") or {}).get("title"),
            )
            for b in blocks
            for r in b["recipes"] or []
            if r.get("id")
        ][:limit]

    async def recipes_by_tag(
        self, tags: list[str], category: str | None = None, limit: int = 20
    ) -> list[Recipe]:
        """All of nemlig's recipes with every tag, e.g. ["Familievenlig", "Under 30 min"].
        Tags are matched on nemlig's own list, by title or path. Category defaults to
        nemlig's "aftensmad"."""
        known = await self._recipe_tags()
        paths = []
        for tag in tags:
            path = known.get(tag.strip().lower())
            if not path:
                names = ", ".join(self._tag_names)
                raise StoreError(f"nemlig kender ikke tagget {tag!r}. Brug et af: {names}.")
            paths.append(path)
        params: dict[str, Any] = {"skip": 0, "take": limit, "filters": paths}
        if category:
            params["categoryPath"] = category_path(category)
        data = await self._api("GET", f"{BFF}/meals/allRecipes", params=params) or {}
        found = data.get("requestedCategory") or {}
        if not isinstance(found.get("recipes"), list):
            raise StoreError("nemlig-opskrifter har ændret format.")
        return [
            Recipe(
                store=self.name,
                id=r["id"],
                name=r.get("title", ""),
                url=f"{SITE}/maaltider/opskrift/{r['id']}",
                group=found.get("title"),
            )
            for r in found["recipes"]
            if r.get("id")
        ][:limit]

    async def _recipe_tags(self) -> dict[str, str]:
        """nemlig's tags, by lower-case title and by path → path. Cached: they rarely change."""
        if not getattr(self, "_tags", None):
            data = await self._api("GET", f"{BFF}/meals/allRecipes", params={"skip": 0, "take": 1})
            found = [
                f
                for group in (data or {}).get("filters") or []
                for f in group.get("filters") or []
                if f.get("path")
            ]
            if not found:
                raise StoreError("nemlig-opskrifter har ændret format: ingen tags.")
            self._tag_names = [str(f.get("title") or f["path"]).strip() for f in found]
            self._tags = {
                key.strip().lower(): f["path"]
                for f in found
                for key in (str(f.get("title") or ""), f["path"])
                if key.strip()
            }
        return self._tags

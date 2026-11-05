"""nemlig store cases, part 2: recipes."""

from __future__ import annotations

from scripts.golden_fakes import resp
from scripts.golden_hooks import body, put, respond, scripted
from scripts.golden_nemlig_a import ALL, DETAILS, DIALOG, KEBAB, NO_SLOT, PAGE, ROW
from scripts.golden_stores import Case
from tests.test_nemlig import fixture

C = Case


def _recipe_odd(fake, _store) -> None:
    fake.respond[DIALOG] = [resp(200, {"includedProducts": [ROW], "servings": "4-6"})]
    fake.respond[DETAILS] = [
        resp(
            200,
            {
                "servings": "4-6",
                "images": [{"source": "http://x/plain.jpg"}, {"source": None}],
                "instructions": [{"title": None, "steps": [{"description": "  "}]}],
            },
        )
    ]


def _recipe_texts(fake, _store) -> None:
    fake.respond[DIALOG] = [resp(200, {"includedProducts": [ROW], "servings": 6})]
    fake.respond[DETAILS] = [
        resp(
            200,
            {
                "servings": 4,
                "ingredients": [{"ingredients": [{"text": "600 g laksefilet"}, {"text": ""}]}],
                "images": [
                    {"source": "http://insecure.example/x.jpg"},
                    {"source": " https://ok.example/a.jpg "},
                ],
                "instructions": [
                    {"title": "Tom", "steps": [{"description": "  "}, {}]},
                    {"title": None, "steps": [{"description": " Rør rundt "}]},
                ],
                "tip": "  Server med brød  ",
                "description": " Intro ",
                "durationTotal": {"from": 25},
                "servingLimits": {"minServings": 2, "maxServings": 8},
            },
        )
    ]


def _recipe_no_price(fake, _store) -> None:
    fake.respond[DIALOG] = [resp(200, {"includedProducts": [ROW, {"item": {"product": {}}}, {}]})]
    fake.respond[DETAILS] = [resp(200, {})]


RECIPE_CASES = [
    C("recipes_search", "recipes", ("kylling karry", 3)),
    C("recipes_search_default_limit", "recipes", ("kylling karry",)),
    C("recipes_search_no_timeslot", "recipes", ("kylling karry", 3), setup=NO_SLOT),
    C(
        "recipes_search_format_changed",
        "recipes",
        ("karry",),
        setup=body("/productbff/api/web/search/recipes", {"searchResult": {}}),
    ),
    C(
        "recipes_search_item_without_id",
        "recipes",
        ("karry", 5),
        setup=body(
            "/productbff/api/web/search/recipes",
            {"searchResult": {"items": [{"title": "x"}, {"id": "a-1"}]}},
        ),
    ),
    C("recipe_kebab_url", "recipe", (KEBAB, 4)),
    C("recipe_kebab_old_path", "recipe", ("/opskrifter/kebabfad-med-fladbrod-98001303", 6)),
    C("recipe_kebab_no_persons", "recipe", ("kebabfad-med-fladbrod-98001303",)),
    C("recipe_kebab_persons_0", "recipe", ("kebabfad-med-fladbrod-98001303", 0)),
    C(
        "recipe_slug_query_fragment",
        "recipe",
        ("https://www.nemlig.com/maaltider/opskrift/Kebabfad-Med-Fladbrod-98001303/?x=1#top", 4),
    ),
    C("recipe_session", "recipe", (KEBAB, 4), login="session"),
    C("recipe_bad_slug_percent", "recipe", ("kebab%2f..",)),
    C("recipe_bad_slug_space", "recipe", ("kebab fad",)),
    C("recipe_bad_slug_empty", "recipe", ("",)),
    C("recipe_bad_slug_unicode", "recipe", ("kebabfåd",)),
    C("recipe_format_changed", "recipe", (KEBAB,), setup=body(DIALOG, {})),
    C("recipe_dialog_empty_body", "recipe", (KEBAB,), setup=respond(DIALOG, resp())),
    C("recipe_dialog_500", "recipe", (KEBAB,), setup=scripted(DIALOG, 500, 500, 500)),
    C("recipe_details_404", "recipe", (KEBAB,), setup=scripted(DETAILS, 404)),
    C("recipe_odd_servings_and_images", "recipe", (KEBAB, 4), setup=_recipe_odd),
    C("recipe_texts_and_limits", "recipe", (KEBAB, 6), setup=_recipe_texts),
    C("recipe_without_price_or_servings", "recipe", (KEBAB, 6), setup=_recipe_no_price),
    C("featured_default", "featured_recipes", (20,)),
    C("featured_default_limit", "featured_recipes"),
    C("featured_3", "featured_recipes", (3,)),
    C("featured_queue_it_api", "featured_recipes", (20,), setup=put("api_queue", True)),
    C("featured_no_blocks", "featured_recipes", (20,), setup=body(PAGE, {"pageContent": []})),
    C(
        "featured_items_without_id",
        "featured_recipes",
        (20,),
        setup=body(
            PAGE,
            {
                "pageContent": [
                    {
                        "header": {"title": "T"},
                        "recipes": [{"title": "x"}, {"id": "a-1", "title": "A"}],
                    },
                    {"recipes": None},
                    {"recipes": [{"id": "b-2"}]},
                ]
            },
        ),
    ),
    C("by_tag_family_budget", "recipes_by_tag", (["Familievenlig", "budgetvenlig"], None, 5)),
    C("by_tag_default_limit", "recipes_by_tag", (["Familievenlig"],)),
    C("by_tag_under_30_madpakke", "recipes_by_tag", (["Under 30 min"], "Madpakke", 20)),
    C(
        "by_tag_category_with_spaces",
        "recipes_by_tag",
        (["Familievenlig"], "Mad til flere dage", 3),
    ),
    C("by_tag_category_danish_letters", "recipes_by_tag", (["Familievenlig"], "Måltidskasse", 3)),
    C("by_tag_bad_category", "recipes_by_tag", (["Familievenlig"], "aftens/mad", 3)),
    C("by_tag_unknown", "recipes_by_tag", (["Pizza"],)),
    C("by_tag_no_tags", "recipes_by_tag", ([], None, 20)),
    C(
        "by_tag_cached",
        "recipes_by_tag",
        (["Familievenlig"],),
        then=[("recipes_by_tag", (["Kylling"],), None)],
    ),
    C(
        "by_tag_no_filters",
        "recipes_by_tag",
        (["Familievenlig"],),
        setup=body(ALL, {"filters": []}),
    ),
    C(
        "by_tag_search_format_changed",
        "recipes_by_tag",
        (["Familievenlig"],),
        setup=respond(ALL, resp(200, fixture("recipes_by_tag")), resp(200, {})),
    ),
]

CASES = RECIPE_CASES

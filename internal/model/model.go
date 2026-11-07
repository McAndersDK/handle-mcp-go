package model

import "encoding/json"

// List marshals as [] when empty or nil, never null: Python's empty default is [].
type List[T any] []T

// MarshalJSON writes [] for a nil list.
func (l List[T]) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]T(l))
}

// Dict marshals as {} when empty or nil, never null.
type Dict[T any] map[string]T

// MarshalJSON writes {} for a nil map.
func (d Dict[T]) MarshalJSON() ([]byte, error) {
	if d == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]T(d))
}

// JSON names are Python's field names. A missing optional value is a nil pointer and
// marshals as null (never omitted). A field that Python defaults to True (Available)
// is true only when a constructor or the adapter says so.

// Product is one thing to buy.
type Product struct {
	Store       string  `json:"store"`
	ID          string  `json:"id"` // the store's own product id, used in the cart
	Name        string  `json:"name"`
	Price       Money   `json:"price"`
	Brand       *string `json:"brand"`
	Size        *string `json:"size"`       // as the store writes it, e.g. "1 l"
	UnitPrice   *Money  `json:"unit_price"` // price per unit below
	Unit        *string `json:"unit"`       // "kg", "l" or "stk"
	EAN         *string `json:"ean"`
	OnOffer     bool    `json:"on_offer"`
	OfferText   *string `json:"offer_text"`
	BeforePrice *Money  `json:"before_price"` // the price without the offer
	OfferUntil  *Date   `json:"offer_until"`  // last day of the offer
	Available   bool    `json:"available"`
	Category    *string `json:"category"`      // the store's category path
	Campaign    *string `json:"campaign"`      // the store's offer list, e.g. "Priskup"
	MaxPerOrder *int    `json:"max_per_order"` // offer limit, e.g. 1 for a free gift
}

// NewProduct returns a Product that is available, like Python's default.
func NewProduct(store, id, name string, price Money) Product {
	return Product{Store: store, ID: id, Name: name, Price: price, Available: true}
}

// Recipe is a store recipe in a list.
type Recipe struct {
	Store   string  `json:"store"`
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	URL     string  `json:"url"`
	Time    *string `json:"time"` // as the store writes it, e.g. "30 min"
	Persons *int    `json:"persons"`
	Group   *string `json:"group"` // the store's theme, e.g. "Mad i fad"
}

// RecipeProduct is a product picked for a recipe ingredient.
type RecipeProduct struct {
	ID        string  `json:"id"` // the store's product id, for set_cart_quantities
	Name      string  `json:"name"`
	Price     Money   `json:"price"`    // per pack
	Quantity  int     `json:"quantity"` // packs to buy for the persons asked for
	Size      *string `json:"size"`
	Available bool    `json:"available"`
	OfferText *string `json:"offer_text"`
}

// NewRecipeProduct returns a RecipeProduct that is available, like Python's default.
func NewRecipeProduct(id, name string, price Money, quantity int) RecipeProduct {
	return RecipeProduct{ID: id, Name: name, Price: price, Quantity: quantity, Available: true}
}

// RecipeItem is one ingredient and the product the store picks for it.
type RecipeItem struct {
	Ingredient   string              `json:"ingredient"`
	Product      RecipeProduct       `json:"product"`
	HaveAtHome   bool                `json:"have_at_home"` // left out of the total
	Alternatives List[RecipeProduct] `json:"alternatives"`
}

// RecipeSection is one part of the method, e.g. "Salat", with its steps in order.
type RecipeSection struct {
	Title *string      `json:"title"`
	Steps List[string] `json:"steps"`
}

// RecipeDetails is a recipe with its method and the products to buy.
type RecipeDetails struct {
	Store              string              `json:"store"`
	ID                 string              `json:"id"`
	Name               string              `json:"name"`
	URL                string              `json:"url"`
	Persons            *int                `json:"persons"` // nil when the store writes the recipe for pieces, not persons
	MinPersons         *int                `json:"min_persons"`
	MaxPersons         *int                `json:"max_persons"`
	Time               *string             `json:"time"`
	Description        *string             `json:"description"`
	Image              *string             `json:"image"`
	Ingredients        List[string]        `json:"ingredients"`
	IngredientsPersons *int                `json:"ingredients_persons"` // the persons that text is written for
	Method             List[RecipeSection] `json:"method"`
	Tip                *string             `json:"tip"`
	Items              List[RecipeItem]    `json:"items"`
	Total              Money               `json:"total"`   // items to buy, without have_at_home
	Missing            List[string]        `json:"missing"` // product ids the store does not sell here
}

// CartLine is one product line in a cart or an order.
type CartLine struct {
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	LineTotal Money  `json:"line_total"`
}

// How the groceries arrive (docs/adr/0006). A store that cannot tell says nil, never a guess.
const (
	DeliveryHome   = "home"
	DeliveryPickup = "pickup"
)

// Cart is what is in the store's cart now. DeliveryMode is "home", "pickup" or nil; PickupStore is
// the store's name and is set only when the mode is "pickup".
type Cart struct {
	Store         string         `json:"store"`
	Lines         List[CartLine] `json:"lines"`
	ProductsTotal Money          `json:"products_total"`
	Fees          Dict[Money]    `json:"fees"` // e.g. {"levering": 39}
	Total         Money          `json:"total"`
	MinimumOrder  *Money         `json:"minimum_order"`
	MinimumMet    *bool          `json:"minimum_met"`
	Delivery      *string        `json:"delivery"` // chosen delivery time as text
	Warnings      List[string]   `json:"warnings"`
	DeliveryMode  *string        `json:"delivery_mode"`
	PickupStore   *string        `json:"pickup_store"`
}

// QuantityResult is the outcome of one absolute cart quantity.
type QuantityResult struct {
	ProductID string  `json:"product_id"`
	Requested int     `json:"requested"`
	Applied   *int    `json:"applied"` // what the store set; nil if unknown
	OK        bool    `json:"ok"`
	Message   *string `json:"message"`
}

// StoreTerms are the store's delivery fees and minimum order.
type StoreTerms struct {
	Store        string      `json:"store"`
	HomeDelivery bool        `json:"home_delivery"`
	DeliveryFee  *Money      `json:"delivery_fee"`
	OtherFees    Dict[Money] `json:"other_fees"`
	MinimumOrder *Money      `json:"minimum_order"`
	Source       string      `json:"source"` // "api" or "config"
	DeliveryMode *string     `json:"delivery_mode"`
	PickupStore  *string     `json:"pickup_store"`
}

// NewStoreTerms returns terms with Python's default source, "api".
func NewStoreTerms(store string, homeDelivery bool) StoreTerms {
	return StoreTerms{Store: store, HomeDelivery: homeDelivery, Source: "api"}
}

// DeliveryInfo is the delivery time chosen in the cart.
type DeliveryInfo struct {
	Store         string    `json:"store"`
	ChosenStart   *DateTime `json:"chosen_start"`
	ChosenEnd     *DateTime `json:"chosen_end"`
	EditDeadline  *DateTime `json:"edit_deadline"`
	NextAvailable *Date     `json:"next_available"`
	Note          *string   `json:"note"`
	DeliveryMode  *string   `json:"delivery_mode"`
	PickupStore   *string   `json:"pickup_store"`
}

// DeliverySlot is a delivery time the store offers. Read only: handle-mcp never chooses one.
type DeliverySlot struct {
	Store     string   `json:"store"`
	Start     DateTime `json:"start"`
	End       DateTime `json:"end"`
	Price     *Money   `json:"price"` // delivery fee for this slot; nil when the store does not say
	Available bool     `json:"available"`
	Kind      *string  `json:"kind"` // the store's type of delivery, e.g. "Fleksibel" or "Afhentning"
	Note      *string  `json:"note"`
	// PickupStore is where a pickup slot is collected; nil for a delivery slot.
	PickupStore *string `json:"pickup_store"`
}

// NewDeliverySlot returns a slot that is available, like Python's default.
func NewDeliverySlot(store string, start, end DateTime, price *Money) DeliverySlot {
	return DeliverySlot{Store: store, Start: start, End: end, Price: price, Available: true}
}

// Order is a past or open order.
type Order struct {
	Store         string         `json:"store"`
	ID            string         `json:"id"`
	Status        string         `json:"status"`
	Total         Money          `json:"total"`
	PlacedAt      *DateTime      `json:"placed_at"`
	DeliveryStart *DateTime      `json:"delivery_start"`
	DeliveryEnd   *DateTime      `json:"delivery_end"`
	Lines         List[CartLine] `json:"lines"` // empty in order lists
}

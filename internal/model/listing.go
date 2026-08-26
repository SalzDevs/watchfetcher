package model

// Listing is the canonical output of every source adapter. One struct, one
// JSON shape — the contract between fetchers and the storage layer.

type Listing struct {
	Source    string  `json:"source"`              // chrono24 | bobswatches | the1916company | watchfinder
	NativeID  string  `json:"native_id"`           // stable per-listing ID at the source
	URL       string  `json:"url"`
	Title     string  `json:"title"`
	Brand     string  `json:"brand,omitempty"`
	Model     string  `json:"model,omitempty"`
	Ref       string  `json:"ref,omitempty"`       // reference number as stated
	Dial      string  `json:"dial,omitempty"`      // black|blue|green|white|silver|grey|panda|pepsi|batman...
	Material  string  `json:"material,omitempty"`  // steel|gold|two_tone|platinum
	Scope     string  `json:"scope,omitempty"`     // full_set|box_only|papers_only|naked
	Condition string  `json:"condition,omitempty"` // unworn|excellent|very_good|good|fair
	Year      int     `json:"year,omitempty"`
	Dealer    string  `json:"dealer,omitempty"`
	ImageURL  string  `json:"image_url,omitempty"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
}

// Key is the stable identity used to track a listing across nightly runs.
func (l Listing) Key() string {
	return l.Source + ":" + l.NativeID
}

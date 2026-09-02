// Package money: the only place money types live (G7). Decimal everywhere;
// float never crosses a money boundary.
package money

import (
	"github.com/shopspring/decimal"
)

type Decimal = decimal.Decimal

// MustDecimal parses a decimal string; panics on garbage — used only for
// compile-time-known literals in rules/config, never user input.
func MustDecimal(s string) Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic("money: bad constant " + s + ": " + err.Error())
	}
	return d
}

// FromString parses user/config input into a decimal, error preserved.
func FromString(s string) (Decimal, error) {
	return decimal.NewFromString(s)
}

// Zero is the zero value.
func Zero() Decimal { return decimal.Zero }

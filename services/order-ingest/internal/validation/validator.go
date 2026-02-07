package validation

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/shopspring/decimal"
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationErrors []FieldError

func (v ValidationErrors) Error() string {
	return "invalid request"
}

var symbolPattern = regexp.MustCompile(`^[A-Z0-9]+-[A-Z0-9]+$`)

func ValidateOrderRequest(symbol, side, orderType, timeInForce, quantity, price string) ValidationErrors {
	var errs ValidationErrors

	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		errs = append(errs, FieldError{Field: "symbol", Message: "symbol is required"})
	} else if !symbolPattern.MatchString(strings.ToUpper(symbol)) {
		errs = append(errs, FieldError{Field: "symbol", Message: "symbol must match BASE-QUOTE"})
	}

	side = strings.ToLower(strings.TrimSpace(side))
	if side != "buy" && side != "sell" {
		errs = append(errs, FieldError{Field: "side", Message: "side must be buy or sell"})
	}

	orderType = strings.ToLower(strings.TrimSpace(orderType))
	if orderType != "limit" && orderType != "market" {
		errs = append(errs, FieldError{Field: "type", Message: "type must be limit or market"})
	}

	tif := strings.ToUpper(strings.TrimSpace(timeInForce))
	if tif == "" {
		tif = "GTC"
	}
	if tif != "GTC" && tif != "IOC" && tif != "FOK" {
		errs = append(errs, FieldError{Field: "time_in_force", Message: "time_in_force must be GTC, IOC, or FOK"})
	}

	if _, err := parsePositiveDecimal(quantity); err != nil {
		errs = append(errs, FieldError{Field: "quantity", Message: err.Error()})
	}

	trimmedPrice := strings.TrimSpace(price)
	if orderType == "limit" {
		if trimmedPrice == "" {
			errs = append(errs, FieldError{Field: "price", Message: "price is required for limit orders"})
		} else if _, err := parsePositivePrice(trimmedPrice); err != nil {
			errs = append(errs, FieldError{Field: "price", Message: err.Error()})
		}
	}
	if orderType == "market" && trimmedPrice != "" {
		if _, err := parsePositivePrice(trimmedPrice); err != nil {
			errs = append(errs, FieldError{Field: "price", Message: err.Error()})
		}
	}

	return errs
}

func parsePositiveDecimal(raw string) (decimal.Decimal, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return decimal.Zero, fmt.Errorf("quantity is required")
	}
	val, err := decimal.NewFromString(trimmed)
	if err != nil {
		return decimal.Zero, fmt.Errorf("quantity must be a decimal")
	}
	if val.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, fmt.Errorf("quantity must be positive")
	}
	return val, nil
}

func parsePositivePrice(raw string) (decimal.Decimal, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return decimal.Zero, fmt.Errorf("price is required")
	}
	val, err := decimal.NewFromString(trimmed)
	if err != nil {
		return decimal.Zero, fmt.Errorf("price must be a decimal")
	}
	if val.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, fmt.Errorf("price must be positive")
	}
	return val, nil
}

func NormalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

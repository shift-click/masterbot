package providers

import (
	"errors"
	"strings"
)

const unsupportedReferencePrefix = "UNSUPPORTED:"

var (
	ErrCoinQuoteUnavailable       = errors.New("coin quote unavailable from configured providers")
	ErrWorldStockQuoteUnavailable = errors.New("world stock quote unavailable from configured providers")
	ErrWorldStockChartUnavailable = errors.New("world stock chart unavailable from configured providers")
)

func makeUnsupportedReferenceID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return unsupportedReferencePrefix
	}
	return unsupportedReferencePrefix + value
}

func isUnsupportedReferenceID(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), unsupportedReferencePrefix)
}

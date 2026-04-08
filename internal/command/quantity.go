package command

import (
	"regexp"
	"strconv"
	"strings"
)

// quantityPattern matches "{query} * {number}" with optional spaces.
// Supports operators: *, x, X, ×
var quantityPattern = regexp.MustCompile(`^(.+?)\s*[*xX×]\s*(\d+(?:\.\d+)?)$`)

// parseQuantifiedQuery extracts a query and quantity multiplier from input.
// Returns (query, qty, true) for "솔 * 2" → ("솔", 2, true).
// Returns (input, 1, false) if no quantity pattern is found.
func parseQuantifiedQuery(input string) (query string, qty float64, ok bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", 1, false
	}

	m := quantityPattern.FindStringSubmatch(input)
	if m == nil {
		return input, 1, false
	}

	q := strings.TrimSpace(m[1])
	if q == "" {
		return input, 1, false
	}

	n, err := strconv.ParseFloat(m[2], 64)
	if err != nil || n <= 0 {
		return input, 1, false
	}

	return q, n, true
}

package config

import "strconv"

func itoa(v int) string {
	return strconv.Itoa(v)
}

func quote(v string) string {
	return strconv.Quote(v)
}

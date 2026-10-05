// Package env reads configuration from environment variables.
package env

import (
	"fmt"
	"os"
	"strconv"
)

// String returns the variable, or def when it is unset or empty.
func String(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// PositiveInt returns the variable as a positive integer, or def when unset.
func PositiveInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return n, nil
}

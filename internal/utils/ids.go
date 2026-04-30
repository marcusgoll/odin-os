package utils

import "strings"

func NormalizeID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

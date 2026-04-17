package registry

import (
	"fmt"
	"regexp"
	"strings"
)

const SkillHandlerRoot = "scripts/skills"

var validKeyPattern = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)
var validCapabilityKeyPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

func ValidateKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("registry key is required")
	}
	if key != strings.TrimSpace(key) {
		return fmt.Errorf("registry key %q must not include leading or trailing whitespace", key)
	}
	if !validKeyPattern.MatchString(key) {
		return fmt.Errorf("registry key %q must use lowercase letters, digits, hyphen, or underscore", key)
	}
	return nil
}

func ValidateCapabilityKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("registry key is required")
	}
	if key != strings.TrimSpace(key) {
		return fmt.Errorf("registry key %q must not include leading or trailing whitespace", key)
	}
	if !validCapabilityKeyPattern.MatchString(key) {
		return fmt.Errorf("registry key %q must use lowercase letters, digits, dot, hyphen, or underscore", key)
	}
	return nil
}

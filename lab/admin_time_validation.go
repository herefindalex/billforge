package lab

import (
	"regexp"
	"time"
)

var adminUTCInput = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$`)

// canonicalAdminUTC accepts only timestamps that can be stored exactly by
// the commerce tables' int64 Unix-nanosecond representation.
func canonicalAdminUTC(value string) (string, error) {
	if !adminUTCInput.MatchString(value) {
		return "", ErrAdminInvalidCommand
	}
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || at.IsZero() || at.Before(minUnixNanoTime) || at.After(maxUnixNanoTime) {
		return "", ErrAdminInvalidCommand
	}
	return at.UTC().Format(time.RFC3339Nano), nil
}

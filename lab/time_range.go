package lab

import "time"

var (
	minUnixNanoTime = time.Unix(0, -1<<63).UTC()
	maxUnixNanoTime = time.Unix(0, 1<<63-1).UTC()
)

// quoteExpiryAt checks both timestamps before either is converted to the
// int64 Unix-nanosecond representation used by the database and fingerprint.
func quoteExpiryAt(at time.Time) (time.Time, error) {
	at = at.UTC()
	if at.Before(minUnixNanoTime) || at.After(maxUnixNanoTime) {
		return time.Time{}, ErrConflict
	}
	expires := at.Add(15 * time.Minute)
	if expires.After(maxUnixNanoTime) {
		return time.Time{}, ErrConflict
	}
	return expires, nil
}

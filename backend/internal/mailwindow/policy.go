package mailwindow

import "errors"

const (
	Default = 50
	Max     = 200
)

// Resolve applies the default for an omitted count and rejects unsupported windows.
func Resolve(limit int) (int, error) {
	if limit == 0 {
		return Default, nil
	}
	if limit < 0 || limit > Max {
		return 0, errors.New("limit must be between 1 and 200, or 0 for the default")
	}
	return limit, nil
}

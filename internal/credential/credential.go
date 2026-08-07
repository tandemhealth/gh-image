// Package credential stores the one GitHub session used by gh-image.
//
// The service name is intentionally specific to this program. gh-image never
// asks the operating system for a browser encryption key or scans a browser
// cookie database.
package credential

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const (
	service = "tandemhealth-gh-image"
	account = "github.com-user_session"
)

// Get returns the GitHub session saved specifically for gh-image.
func Get() (string, error) {
	value, err := keyring.Get(service, account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", fmt.Errorf("no gh-image credential found; run 'gh image auth-store' in a trusted terminal")
		}
		return "", fmt.Errorf("reading gh-image credential: %w", err)
	}
	return value, nil
}

// Set saves one GitHub session in gh-image's dedicated OS credential entry.
func Set(value string) error {
	if err := keyring.Set(service, account, value); err != nil {
		return fmt.Errorf("storing gh-image credential: %w", err)
	}
	return nil
}

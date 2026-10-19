package access

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/hydrogencms/hydrogen/core/store"
)

// First-run setup: while no user exists, the server prints a link carrying a setup token, and whoever opens it
// creates the first administrator in the admin app. The token is random per process (stable in dev, see
// AdminOptions.SetupSeed) and stops working once any user exists.

var (
	// ErrSetupDone: a user exists, so the first administrator can no longer be created through setup.
	ErrSetupDone = errors.New("access: setup is complete")
	// ErrSetupToken: the setup token is missing or not this process's.
	ErrSetupToken = errors.New("access: the setup link is not valid")
)

type setupState struct {
	mu    sync.Mutex
	token string
}

// NeedsSetup reports whether no user exists yet.
func (a *Admin) NeedsSetup(ctx context.Context) (bool, error) {
	var n int
	err := a.view(ctx, func(s store.AuthStore) error {
		var err error
		_, n, err = s.ListUsers(ctx, store.Page{Limit: 1}, "")
		return err
	})
	return n == 0, err
}

// SetupToken returns this process's setup token, made on first use, or "" once a user exists.
func (a *Admin) SetupToken(ctx context.Context) (string, error) {
	need, err := a.NeedsSetup(ctx)
	if err != nil || !need {
		return "", err
	}
	a.setup.mu.Lock()
	defer a.setup.mu.Unlock()
	if a.setup.token == "" && a.seed != "" {
		sum := sha256.Sum256([]byte("hydrogen-setup:" + a.seed))
		a.setup.token = base64.RawURLEncoding.EncodeToString(sum[:24])
	}
	if a.setup.token == "" {
		b := make([]byte, 24)
		if _, err := io.ReadFull(a.rand, b); err != nil {
			return "", fmt.Errorf("access: setup token: %w", err)
		}
		a.setup.token = base64.RawURLEncoding.EncodeToString(b)
	}
	return a.setup.token, nil
}

// CompleteSetup creates the first administrator with the setup token. It fails with ErrSetupToken for a wrong
// token and with ErrSetupDone once any user exists; on success the token stops working.
func (a *Admin) CompleteSetup(ctx context.Context, token, login, password string) (User, error) {
	a.setup.mu.Lock()
	defer a.setup.mu.Unlock()
	if a.setup.token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(a.setup.token)) != 1 {
		return User{}, ErrSetupToken
	}
	u, err := a.ensureAdmin(ctx, login, password, true)
	if err == nil || errors.Is(err, ErrSetupDone) {
		a.setup.token = ""
	}
	return u, err
}

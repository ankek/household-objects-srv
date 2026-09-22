package middleware

import (
	"errors"
	"net/http"
)

func Chain(authenticators ...Authenticator) Authenticator {
	if len(authenticators) < 2 {
		panic("middleware: Chain needs at least two Authenticators; a single one should be passed to TenantScope directly")
	}
	chained := make([]Authenticator, len(authenticators))
	copy(chained, authenticators)

	return AuthenticatorFunc(func(r *http.Request) (Identity, error) {
		var last error
		for _, a := range chained {
			identity, err := a.Authenticate(r)
			if err == nil {
				return identity, nil
			}
			if !errors.Is(err, ErrUnauthenticated) {
				return Identity{}, err
			}
			last = err
		}
		return Identity{}, last
	})
}

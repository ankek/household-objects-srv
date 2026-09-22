package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func declining(called *bool) Authenticator {
	return AuthenticatorFunc(func(*http.Request) (Identity, error) {
		if called != nil {
			*called = true
		}
		return Identity{}, fmt.Errorf("declining: no credential: %w", ErrUnauthenticated)
	})
}

func accepting(id Identity, called *bool) Authenticator {
	return AuthenticatorFunc(func(*http.Request) (Identity, error) {
		if called != nil {
			*called = true
		}
		return id, nil
	})
}

var errBoom = errors.New("boom: authentication could not be performed")

func erroring(called *bool) Authenticator {
	return AuthenticatorFunc(func(*http.Request) (Identity, error) {
		if called != nil {
			*called = true
		}
		return Identity{}, errBoom
	})
}

func req() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
}

func TestChainPanicsWithFewerThanTwoAuthenticators(t *testing.T) {
	cases := [][]Authenticator{
		nil,
		{},
		{declining(nil)},
	}
	for _, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Chain(%d authenticators) did not panic", len(c))
				}
			}()
			Chain(c...)
		}()
	}
}

func TestChainReturnsTheFirstSuccess(t *testing.T) {
	want := Identity{Group: "g1", UserID: "u1", Role: "owner"}
	var firstCalled, secondCalled bool
	chain := Chain(accepting(want, &firstCalled), accepting(Identity{Group: "g2"}, &secondCalled))

	got, err := chain.Authenticate(req())
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got != want {
		t.Errorf("Authenticate = %+v, want %+v", got, want)
	}
	if !firstCalled {
		t.Error("the first authenticator was never called")
	}
	if secondCalled {
		t.Error("the second authenticator was called even though the first succeeded")
	}
}

func TestChainFallsThroughToTheSecondOnADecline(t *testing.T) {
	want := Identity{Group: "g2", UserID: "u2", Role: "member"}
	var firstCalled, secondCalled bool
	chain := Chain(declining(&firstCalled), accepting(want, &secondCalled))

	got, err := chain.Authenticate(req())
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got != want {
		t.Errorf("Authenticate = %+v, want %+v", got, want)
	}
	if !firstCalled {
		t.Error("the first authenticator was never called")
	}
	if !secondCalled {
		t.Error("the second authenticator was never called after the first declined")
	}
}

func TestChainDeclinesWhenEveryAuthenticatorDeclines(t *testing.T) {
	chain := Chain(declining(nil), declining(nil))
	if _, err := chain.Authenticate(req()); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate = %v, want ErrUnauthenticated", err)
	}
}

func TestChainStopsImmediatelyOnAHardFailure(t *testing.T) {
	var secondCalled bool
	chain := Chain(erroring(nil), declining(&secondCalled))

	_, err := chain.Authenticate(req())
	if !errors.Is(err, errBoom) {
		t.Fatalf("Authenticate error = %v, want it to wrap errBoom (the hard failure), unchanged", err)
	}
	if errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate error = %v, wrongly classified as ErrUnauthenticated -- a hard failure must never look like a declined credential", err)
	}
	if secondCalled {
		t.Fatal("the second authenticator was called after the first returned a hard failure; the failure was silently retried instead of surfaced")
	}
}

func TestChainHardFailureFromTheSecondAuthenticatorAlsoSurfaces(t *testing.T) {
	chain := Chain(declining(nil), erroring(nil))
	_, err := chain.Authenticate(req())
	if !errors.Is(err, errBoom) {
		t.Fatalf("Authenticate error = %v, want it to wrap errBoom", err)
	}
}

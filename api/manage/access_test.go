package manage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/audit"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
)

// fakeAuth maps bearer values to principals: "user" signs in with a session, "agent" is a non-super agent.
type fakeAuth struct{ csrfErr error }

func (f fakeAuth) Authenticate(r *http.Request) (*access.Authn, error) {
	switch r.Header.Get("Authorization") {
	case "Bearer super":
		return &access.Authn{Principal: &access.Principal{Kind: access.KindToken, Name: "config", Super: true}}, nil
	case "Bearer agent":
		return &access.Authn{Principal: &access.Principal{Kind: access.KindAgent, ID: ids.UUID{9}, Name: "bot",
			Detail: map[string]string{"model": "m1"}}}, nil
	case "Bearer user":
		return &access.Authn{Principal: &access.Principal{Kind: access.KindUser, ID: ids.UUID{8}, Name: "ann"}}, nil
	}
	return nil, access.ErrUnauthenticated
}

func (f fakeAuth) CheckCSRF(r *http.Request, an *access.Authn) error {
	if r.Header.Get("Authorization") == "Bearer user" && r.Method != "GET" {
		return f.csrfErr
	}
	return nil
}

func (fakeAuth) Login(http.ResponseWriter, *http.Request, string, string) (*access.LoginResult, error) {
	return nil, &access.ThrottledError{RetryAfter: 90 * time.Second}
}

func (fakeAuth) Logout(http.ResponseWriter, *http.Request) error { return nil }

// holds grants exactly the listed policies.
type holds []string

func (h holds) Holds(_ context.Context, policy string) error {
	for _, p := range h {
		if p == policy {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", access.ErrForbidden, policy)
}

func (h holds) Grants(context.Context, *access.Principal) ([]access.Grant, error) {
	return []access.Grant{{Policy: "content.read", Role: "viewer"}}, nil
}

func accessAPI(t *testing.T, f *fake, g Gate, a Authenticator) http.Handler {
	t.Helper()
	return newAPI(t, f, func(o *Options) { o.Token, o.Auth, o.Gate = "", a, g })
}

func doAs(h http.Handler, who, method, target, body string) *httptest.ResponseRecorder {
	return do(h, method, target, body, "Authorization", "Bearer "+who)
}

func TestRoutePolicyGate(t *testing.T) {
	f := newFake(t)
	h := accessAPI(t, f, holds{"content.read"}, fakeAuth{})
	id := testID.String()
	if w := doAs(h, "agent", "GET", "/items/"+id, ""); w.Code != 200 {
		t.Fatalf("read: %d %s", w.Code, w.Body)
	}
	w := doAs(h, "agent", "PATCH", "/items/"+id, `{"fields":{}}`)
	body := errCode(t, w)
	if w.Code != 403 || body["code"] != "forbidden" || body["message"] != "content.edit" {
		t.Fatalf("edit without the policy: %d %v", w.Code, body)
	}
	for _, c := range f.calls {
		if c.name == "Update" {
			t.Fatal("the service ran for a refused route")
		}
	}
	// Authenticated-only routes need no policy; unknown credentials are 401.
	if w := doAs(h, "agent", "GET", "/types", ""); w.Code != 200 {
		t.Errorf("types: %d", w.Code)
	}
	if w := doAs(h, "nobody", "GET", "/types", ""); w.Code != 401 || w.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("unknown credentials: %d %v", w.Code, w.Header())
	}
	// Super passes every gate, even without a gate configured.
	h = accessAPI(t, f, nil, fakeAuth{})
	if w := doAs(h, "super", "PATCH", "/items/"+id, `{"fields":{}}`); w.Code != 200 {
		t.Errorf("super: %d %s", w.Code, w.Body)
	}
	if w := doAs(h, "agent", "GET", "/items/"+id, ""); w.Code != 403 {
		t.Errorf("no gate must refuse non-super principals: %d", w.Code)
	}
}

func TestPrincipalAndActorReachTheService(t *testing.T) {
	f := newFake(t)
	var p *access.Principal
	var a audit.Actor
	cf := &ctxFake{fake: f, on: func(got audit.Actor) { a = got }}
	h := newAPI(t, f, func(o *Options) {
		o.Token, o.Auth, o.Gate, o.Content = "", fakeAuth{}, holds{"content.read"}, &principalFake{ctxFake: cf, on: func(got *access.Principal) { p = got }}
	})
	if w := doAs(h, "agent", "GET", "/items/"+testID.String(), ""); w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
	if p == nil || p.Kind != access.KindAgent || a.Kind != "agent" || a.Detail["model"] != "m1" {
		t.Errorf("principal %+v actor %+v", p, a)
	}
}

type principalFake struct {
	*ctxFake
	on func(*access.Principal)
}

func (p *principalFake) Get(ctx context.Context, id ids.UUID, o content.GetOptions) (*content.View, error) {
	p.on(access.PrincipalFrom(ctx))
	return p.ctxFake.Get(ctx, id, o)
}

func TestCSRFRefusal(t *testing.T) {
	h := accessAPI(t, newFake(t), holds{"content.edit"}, fakeAuth{csrfErr: access.ErrCSRF})
	w := doAs(h, "user", "PATCH", "/items/"+testID.String(), `{"fields":{}}`)
	if w.Code != 403 || errCode(t, w)["code"] != "csrf" {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

func TestAccessErrorsMap(t *testing.T) {
	f := newFake(t)
	h := accessAPI(t, f, holds{"content.read", "content.edit", "content.delete"}, fakeAuth{})
	id := testID.String()
	for _, tc := range []struct {
		err    error
		status int
		code   string
		check  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{&access.ThrottledError{RetryAfter: 1500 * time.Millisecond}, 429, "throttled", func(t *testing.T, w *httptest.ResponseRecorder) {
			if w.Header().Get("Retry-After") != "2" {
				t.Errorf("Retry-After %q", w.Header().Get("Retry-After"))
			}
		}},
		{&access.InvalidError{Fields: map[string]string{"name": "required", "email": "bad"}}, 400, "bad_request", func(t *testing.T, w *httptest.ResponseRecorder) {
			if b := w.Body.String(); strings.Index(b, `"email"`) > strings.Index(b, `"name"`) || !strings.Contains(b, `"fields"`) {
				t.Errorf("fields not sorted: %s", b)
			}
		}},
		{&content.ReferencedError{Hidden: 3}, 409, "referenced", func(t *testing.T, w *httptest.ResponseRecorder) {
			if !strings.Contains(w.Body.String(), `"hidden":3`) {
				t.Errorf("hidden: %s", w.Body)
			}
		}},
	} {
		f.err = tc.err
		w := doAs(h, "agent", "DELETE", "/items/"+id, "")
		if w.Code != tc.status || errCode(t, w)["code"] != tc.code {
			t.Errorf("%T: %d %s", tc.err, w.Code, w.Body)
		}
		if tc.check != nil {
			tc.check(t, w)
		}
	}
	// A wrapped forbidden error shows the policy and nothing around it.
	f.err = errors.New("content: update 123: " + access.ErrForbidden.Error() + ": content.edit")
	if got := forbiddenPolicy(f.err); got != "content.edit" {
		t.Errorf("policy %q", got)
	}
	f.err = nil
}

func TestLoginIsPublicAndThrottled(t *testing.T) {
	h := accessAPI(t, newFake(t), nil, fakeAuth{})
	w := do(h, "POST", "/auth/login", `{"login":"a","password":"b"}`, "Authorization", "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "90" {
		t.Errorf("%d %v %s", w.Code, w.Header(), w.Body)
	}
	if w := do(h, "POST", "/auth/login", `{"login":"a","password":"b","extra":1}`, "Authorization", ""); w.Code != 400 {
		t.Errorf("unknown field: %d", w.Code)
	}
}

func TestAccessRoutesNeedAdmin(t *testing.T) {
	h := accessAPI(t, newFake(t), holds{"user.manage", "audit.read"}, fakeAuth{})
	for _, p := range []string{"/users", "/audit", "/tokens"} {
		if w := doAs(h, "agent", "GET", p, ""); w.Code != 404 && w.Code != 403 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	if w := doAs(h, "agent", "GET", "/users", ""); w.Code != 404 {
		t.Errorf("users without an admin service: %d", w.Code)
	}
	if w := doAs(h, "agent", "GET", "/tokens", ""); w.Code != 403 {
		t.Errorf("tokens without token.manage: %d", w.Code)
	}
}

func TestSessionRoute(t *testing.T) {
	h := accessAPI(t, newFake(t), holds{"content.read"}, fakeAuth{})
	w := doAs(h, "agent", "GET", "/auth/session", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"kind":"agent"`) || !strings.Contains(w.Body.String(), `"policy":"content.read"`) {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	w = doAs(h, "super", "GET", "/auth/session", "")
	if !strings.Contains(w.Body.String(), `"policy":"*"`) {
		t.Errorf("super: %s", w.Body)
	}
}

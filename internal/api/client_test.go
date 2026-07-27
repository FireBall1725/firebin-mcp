// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 FireBall1725

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetDecodesBareJSON(t *testing.T) {
	// FireBin returns the object directly, with no `data` envelope.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer fbin_pat_test" {
			t.Errorf("Authorization = %q, want the configured bearer", got)
		}
		w.Write([]byte(`{"parts_count":42}`))
	}))
	defer srv.Close()

	out, err := Get[struct {
		PartsCount int `json:"parts_count"`
	}](context.Background(), New(srv.URL, "fbin_pat_test"), "/stats")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if out.PartsCount != 42 {
		t.Errorf("parts_count = %d, want 42", out.PartsCount)
	}
}

func TestErrorEnvelopeIsParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"part has no MPN to look up","code":"part.no_mpn"}`))
	}))
	defer srv.Close()

	_, err := Get[map[string]any](context.Background(), New(srv.URL, "t"), "/parts")
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *Error
	if !statusIs(err, http.StatusBadRequest) {
		t.Fatalf("expected a 400, got %v", err)
	}
	if ok := asErrorForTest(err, &apiErr); !ok {
		t.Fatal("error is not an *Error")
	}
	if apiErr.Message != "part has no MPN to look up" || apiErr.Code != "part.no_mpn" {
		t.Errorf("envelope not parsed: %+v", apiErr)
	}
	if want := "firebin api 400: part has no MPN to look up"; apiErr.Error() != want {
		t.Errorf("Error() = %q, want %q", apiErr.Error(), want)
	}
}

// A non-JSON body (an ingress error page, say) must still produce a usable
// error rather than a decode failure.
func TestNonJSONErrorBodyIsPreserved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer srv.Close()

	_, err := Get[map[string]any](context.Background(), New(srv.URL, "t"), "/parts")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !statusIs(err, http.StatusBadGateway) {
		t.Errorf("expected a 502, got %v", err)
	}
}

func TestNotFoundAndForbidden(t *testing.T) {
	cases := []struct {
		status    int
		notFound  bool
		forbidden bool
	}{
		{http.StatusNotFound, true, false},
		{http.StatusForbidden, false, true},
		{http.StatusInternalServerError, false, false},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(`{"error":"nope"}`))
		}))
		_, err := Get[map[string]any](context.Background(), New(srv.URL, "t"), "/x")
		if NotFound(err) != c.notFound {
			t.Errorf("status %d: NotFound() = %v, want %v", c.status, NotFound(err), c.notFound)
		}
		if Forbidden(err) != c.forbidden {
			t.Errorf("status %d: Forbidden() = %v, want %v", c.status, Forbidden(err), c.forbidden)
		}
		srv.Close()
	}
}

// An empty 200 body must leave the zero value rather than failing to decode:
// several write endpoints answer with no content.
func TestPostToleratesEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	out, err := Post[map[string]string, map[string]any](
		context.Background(), New(srv.URL, "t"), "/stock/move", map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected the zero value, got %v", out)
	}
}

// asErrorForTest keeps the errors.As dance out of the assertions above.
func asErrorForTest(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

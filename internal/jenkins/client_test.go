package jenkins

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWhoAmI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "token123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if r.URL.Path != "/me/api/json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Jenkins", "2.479.1")
		w.Write([]byte(`{"id":"admin","fullName":"Admin jmodel.User"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "admin", "token123", false)
	user, err := client.WhoAmI(context.Background())
	if err != nil {
		t.Fatalf("WhoAmI() error: %v", err)
	}

	if user.ID != "admin" {
		t.Errorf("user.ID = %q, want %q", user.ID, "admin")
	}
	if user.FullName != "Admin jmodel.User" {
		t.Errorf("user.FullName = %q, want %q", user.FullName, "Admin jmodel.User")
	}
	if user.JenkinsVersion != "2.479.1" {
		t.Errorf("user.JenkinsVersion = %q, want %q", user.JenkinsVersion, "2.479.1")
	}
}

func TestWhoAmIAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "admin", "wrong", false)
	_, err := client.WhoAmI(context.Background())
	if err == nil {
		t.Fatal("WhoAmI() expected error for unauthorized, got nil")
	}
}

func TestWhoAmILoginRequired(t *testing.T) {
	// Mimic a Jenkins with an SSO realm: unauthenticated requests bounce to
	// securityRealm/commenceLogin via a relative redirect, which otherwise
	// loops until the redirect limit.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "securityRealm/commenceLogin?from=%2F", http.StatusFound)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "admin", "token123", false)
	_, err := client.WhoAmI(context.Background())
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("WhoAmI() error = %v, want ErrLoginRequired", err)
	}
}

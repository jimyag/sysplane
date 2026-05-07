package tokenauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewCatalogRejectsDuplicateAcrossDomains(t *testing.T) {
	_, err := NewCatalog([]string{"dup"}, nil, []string{"dup"}, nil)
	if err == nil {
		t.Fatal("expected duplicate token error")
	}
}

func TestAuthenticateHTTP(t *testing.T) {
	catalog, err := NewCatalog([]string{"client-token"}, []string{"admin-token"}, []string{"agent-token"}, []string{"proxy-token"})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}

	identity, err := catalog.AuthenticateHTTP("Bearer admin-token", DomainAdmin)
	if err != nil {
		t.Fatalf("AuthenticateHTTP: %v", err)
	}
	if identity.Domain != DomainAdmin {
		t.Fatalf("expected admin identity, got %s", identity.Domain)
	}

	if _, err := catalog.AuthenticateHTTP("Bearer client-token", DomainAdmin); err == nil {
		t.Fatal("expected forbidden for client token on admin-only route")
	}
}

func TestAuthenticateRegistration(t *testing.T) {
	catalog, err := NewCatalog(nil, nil, []string{"agent-token"}, []string{"proxy-token"})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}

	if _, err := catalog.AuthenticateRegistration(DomainAgent, "agent-token"); err != nil {
		t.Fatalf("AuthenticateRegistration agent: %v", err)
	}
	if _, err := catalog.AuthenticateRegistration(DomainProxy, "agent-token"); err == nil {
		t.Fatal("expected proxy registration with agent token to fail")
	}
}

func TestRequireHTTPStoresIdentityInContext(t *testing.T) {
	catalog, err := NewCatalog([]string{"client-token"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}

	handler := catalog.RequireHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := IdentityFromContext(r.Context())
		if !ok {
			t.Fatal("expected identity in context")
		}
		if identity.Domain != DomainClient {
			t.Fatalf("expected client identity, got %s", identity.Domain)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer client-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

package tokenauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

type Domain string

const (
	DomainClient Domain = "client"
	DomainAdmin  Domain = "admin"
	DomainAgent  Domain = "agent"
	DomainProxy  Domain = "proxy"
)

type Identity struct {
	Domain      Domain
	SubjectID   string
	Fingerprint string
}

type Catalog struct {
	httpTokens         map[string]Identity
	registrationTokens map[Domain]map[string]Identity
}

type contextKey struct{}

func NewCatalog(clientTokens, adminTokens, agentTokens, proxyTokens []string) (*Catalog, error) {
	c := &Catalog{
		httpTokens: map[string]Identity{},
		registrationTokens: map[Domain]map[string]Identity{
			DomainAgent: {},
			DomainProxy: {},
		},
	}

	seen := make(map[string]Domain)
	if err := c.addDomainTokens(seen, DomainClient, clientTokens, c.httpTokens); err != nil {
		return nil, err
	}
	if err := c.addDomainTokens(seen, DomainAdmin, adminTokens, c.httpTokens); err != nil {
		return nil, err
	}
	if err := c.addDomainTokens(seen, DomainAgent, agentTokens, c.registrationTokens[DomainAgent]); err != nil {
		return nil, err
	}
	if err := c.addDomainTokens(seen, DomainProxy, proxyTokens, c.registrationTokens[DomainProxy]); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Catalog) addDomainTokens(seen map[string]Domain, domain Domain, tokens []string, dst map[string]Identity) error {
	for idx, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			return fmt.Errorf("%s token at index %d must not be empty", domain, idx)
		}
		fp := tokenFingerprint(token)
		if prev, ok := seen[fp]; ok {
			return fmt.Errorf("token %q is duplicated across %s and %s domains", token, prev, domain)
		}
		seen[fp] = domain
		dst[fp] = Identity{
			Domain:      domain,
			SubjectID:   fmt.Sprintf("%s_%02d", domain, idx+1),
			Fingerprint: tokenFingerprint(token),
		}
	}
	return nil
}

func (c *Catalog) AuthenticateHTTP(header string, allowed ...Domain) (Identity, error) {
	token, err := bearerToken(header)
	if err != nil {
		return Identity{}, err
	}
	identity, ok := c.httpTokens[tokenFingerprint(token)]
	if !ok {
		return Identity{}, fmt.Errorf("unauthorized")
	}
	if len(allowed) == 0 {
		return identity, nil
	}
	if slices.Contains(allowed, identity.Domain) {
		return identity, nil
	}
	return Identity{}, fmt.Errorf("forbidden")
}

func (c *Catalog) AuthenticateRegistration(domain Domain, token string) (Identity, error) {
	token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	if token == "" {
		return Identity{}, fmt.Errorf("unauthorized")
	}
	domainTokens, ok := c.registrationTokens[domain]
	if !ok {
		return Identity{}, fmt.Errorf("unsupported registration domain %q", domain)
	}
	identity, ok := domainTokens[tokenFingerprint(token)]
	if !ok {
		return Identity{}, fmt.Errorf("unauthorized")
	}
	return identity, nil
}

func (c *Catalog) RequireHTTP(next http.Handler, allowed ...Domain) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := c.AuthenticateHTTP(r.Header.Get("Authorization"), allowed...)
		if err != nil {
			switch err.Error() {
			case "forbidden":
				http.Error(w, "Forbidden", http.StatusForbidden)
			default:
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
			}
			return
		}
		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
	})
}

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, identity)
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(contextKey{}).(Identity)
	return identity, ok
}

func bearerToken(header string) (string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", fmt.Errorf("missing bearer token")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", fmt.Errorf("invalid bearer token")
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", fmt.Errorf("invalid bearer token")
	}
	return token, nil
}

func tokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

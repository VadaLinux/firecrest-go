package firecrest

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// ClientCredentials configures Keycloak's OAuth2 client-credentials flow.
type ClientCredentials struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

// NewClientCredentialsHTTPClient returns an HTTP client that obtains and safely
// reuses OAuth2 tokens. oauth2's TokenSource serializes refreshes for callers.
func NewClientCredentialsHTTPClient(ctx context.Context, credentials ClientCredentials) *http.Client {
	config := clientcredentials.Config{TokenURL: credentials.TokenURL, ClientID: credentials.ClientID, ClientSecret: credentials.ClientSecret, Scopes: credentials.Scopes}
	return config.Client(ctx)
}

// NewStaticTokenHTTPClient is useful for tests and externally managed tokens.
func NewStaticTokenHTTPClient(ctx context.Context, token string) *http.Client {
	return oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token, TokenType: "Bearer"}))
}

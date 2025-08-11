package auth

import (
	"context"
	"net/http"

	"go.uber.org/zap"

	"mcp-gateway/internal/common/config"
	"mcp-gateway/internal/common/errorx"
)

// Auth defines the authentication oauth interface
type Auth interface {
	OAuth2
	IsOAuth2Enabled() bool
	GetOAuth2CORS() *config.CORSConfig
}

type OAuth2 interface {
	// ServerMetadata returns the server metadata
	ServerMetadata(r *http.Request) map[string]interface{}

	// Authorize handles the authorization request
	Authorize(ctx context.Context, r *http.Request) (*AuthorizationResponse, error)

	// Token handles the token request
	Token(ctx context.Context, r *http.Request) (*TokenResponse, error)

	// Register handles client registration
	Register(ctx context.Context, r *http.Request) (*ClientRegistrationResponse, error)

	// Revoke handles token revocation
	Revoke(ctx context.Context, r *http.Request) error

	// ValidateToken validates an access token
	ValidateToken(ctx context.Context, token string) error
}

// AuthorizationResponse represents the response from the authorization endpoint
type AuthorizationResponse struct {
	Code  string
	State string
}

// TokenResponse represents the response from the token endpoint
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// ClientRegistrationResponse represents the response from the client registration endpoint
type ClientRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

type auth struct {
	OAuth2
	cfg config.AuthConfig
}

// NewAuth creates a new auth oauth based on the configuration
func NewAuth(logger *zap.Logger, cfg config.AuthConfig) (Auth, error) {
	a := &auth{
		cfg: cfg,
	}
	if cfg.OAuth2 != nil {
		oauth2, err := newOAuth(logger, *cfg.OAuth2)
		if err != nil {
			return nil, err
		}
		a.OAuth2 = oauth2
	}
	return a, nil
}

// IsOAuth2Enabled returns true if OAuth2 is enabled
func (a *auth) IsOAuth2Enabled() bool {
	return a.cfg.OAuth2 != nil
}

// GetOAuth2CORS returns the OAuth2 CORS configuration
func (a *auth) GetOAuth2CORS() *config.CORSConfig {
	return a.cfg.CORS
}

// ValidateToken validates an access token
func (a *auth) ValidateToken(ctx context.Context, token string) error {
	if a.OAuth2 == nil {
		return errorx.ErrOAuth2NotEnabled
	}

	return a.OAuth2.ValidateToken(ctx, token)
}

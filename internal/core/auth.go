package core

import (
	"net/http"
	"net/url"
	"strings"

	"mcp-gateway/internal/common/errorx"

	"github.com/gin-gonic/gin"
)

// handleOAuthServerMetadata handles the OAuth server metadata endpoint
func (s *Server) handleOAuthServerMetadata(c *gin.Context) {
	metadata := s.auth.ServerMetadata(c.Request)
	c.JSON(http.StatusOK, metadata)
}

// renderAuthorizationPage renders the OAuth authorization page
func (s *Server) renderAuthorizationPage(c *gin.Context, clientName string, redirectURI string, state string) {
	c.HTML(http.StatusOK, "authorize.html", gin.H{
		"clientName":  clientName,
		"redirectURI": redirectURI,
		"state":       state,
	})
}

// handleOAuthAuthorize handles the OAuth authorization endpoint
func (s *Server) handleOAuthAuthorize(c *gin.Context) {
	// Get client info from request
	clientID := c.Query("client_id")
	redirectURI := c.Query("redirect_uri")
	state := c.Query("state")
	responseType := c.Query("response_type")
	scope := c.Query("scope")

	// If it's a POST request, process the authorization
	if c.Request.Method == http.MethodPost {
		// Get parameters from form
		clientID = c.PostForm("client_id")
		redirectURI = c.PostForm("redirect_uri")
		state = c.PostForm("state")
		responseType = c.PostForm("response_type")
		scope = c.PostForm("scope")

		// Set query parameters for the OAuth service
		q := c.Request.URL.Query()
		q.Set("client_id", clientID)
		q.Set("redirect_uri", redirectURI)
		q.Set("state", state)
		q.Set("response_type", responseType)
		q.Set("scope", scope)
		c.Request.URL.RawQuery = q.Encode()

		resp, err := s.auth.Authorize(c.Request.Context(), c.Request)
		if err != nil {
			s.sendOAuthError(c, err)
			return
		}

		// Redirect to the client's redirect URI with the authorization code
		u, err := url.Parse(redirectURI)
		if err != nil {
			s.sendOAuthError(c, errorx.ErrInvalidRedirectURI)
			return
		}

		q = u.Query()
		q.Set("code", resp.Code)
		if resp.State != "" {
			q.Set("state", resp.State)
		}
		u.RawQuery = q.Encode()

		c.Redirect(http.StatusFound, u.String())
		return
	}

	// For GET requests, render the authorization page
	s.renderAuthorizationPage(c, clientID, redirectURI, state)
}

// handleOAuthToken handles the OAuth token endpoint
func (s *Server) handleOAuthToken(c *gin.Context) {
	resp, err := s.auth.Token(c.Request.Context(), c.Request)
	if err != nil {
		s.sendOAuthError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// handleOAuthRegister handles the OAuth client registration endpoint
func (s *Server) handleOAuthRegister(c *gin.Context) {
	resp, err := s.auth.Register(c.Request.Context(), c.Request)
	if err != nil {
		s.sendOAuthError(c, err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// handleOAuthRevoke handles the OAuth token revocation endpoint
func (s *Server) handleOAuthRevoke(c *gin.Context) {
	if err := s.auth.Revoke(c.Request.Context(), c.Request); err != nil {
		s.sendOAuthError(c, err)
		return
	}

	c.Status(http.StatusOK)
}

// sendOAuthError sends an OAuth error response
func (s *Server) sendOAuthError(c *gin.Context, err error) {
	oauthErr := errorx.ConvertToOAuth2Error(err)
	c.JSON(oauthErr.HTTPStatus, gin.H{
		"error":             oauthErr.ErrorType,
		"error_description": oauthErr.ErrorDescription,
		"error_uri":         oauthErr.ErrorURI,
		"error_code":        oauthErr.ErrorCode,
	})
}

// isValidAccessToken checks if the request has a valid Bearer token
func (s *Server) isValidAccessToken(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return false
	}
	parts := strings.Split(auth, " ")
	if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
		return false
	}

	// Validate token using auth service
	err := s.auth.ValidateToken(r.Context(), parts[1])
	return err == nil
}

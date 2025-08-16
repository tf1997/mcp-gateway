package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"mcp-gateway/internal/auth"
	"mcp-gateway/internal/common/cnst"
	"mcp-gateway/internal/common/config"
	"mcp-gateway/internal/core/mcpproxy"
	"mcp-gateway/internal/core/state"
	"mcp-gateway/internal/mcp/session"
	"mcp-gateway/internal/mcp/storage"
	"mcp-gateway/pkg/mcp"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type (
	// Server represents the MCP server
	Server struct {
		logger *zap.Logger
		port   int
		router *gin.Engine
		// state contains all the read-only shared state
		state *state.State
		// store is the storage service for MCP configs
		store storage.Store
		// sessions manages all active sessions
		sessions session.Store
		// shutdownCh is used to signal shutdown to all SSE connections
		shutdownCh chan struct{}
		// toolRespHandler is a chain of response handlers
		toolRespHandler ResponseHandler
		lastUpdateTime  time.Time
		auth            auth.Auth
	}
)

// NewServer creates a new MCP server
func NewServer(logger *zap.Logger, port int, store storage.Store, sessionStore session.Store, a auth.Auth) (*Server, error) {
	s := &Server{
		logger:          logger,
		port:            port,
		router:          gin.Default(),
		state:           state.NewState(),
		store:           store,
		sessions:        sessionStore,
		shutdownCh:      make(chan struct{}),
		toolRespHandler: CreateResponseHandlerChain(),
		auth:            a,
	}

	// Load HTML templates
	s.router.LoadHTMLGlob("assets/templates/*")
	// Serve static files
	s.router.Static("/static", "assets/static")

	s.router.Use(s.loggerMiddleware())
	s.router.Use(s.recoveryMiddleware())
	return s, nil
}

// RegisterRoutes registers routes with the given router for MCP servers
func (s *Server) RegisterRoutes(ctx context.Context) error {
	s.router.GET("/health_check", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"message": "Health check passed.",
		})
	})
	s.router.POST("/api/v1/configs", func(c *gin.Context) {
		s.UpdateConfigFromHTTP(ctx, c)
		if !c.IsAborted() {
			c.JSON(http.StatusOK, gin.H{
				"status":  "ok",
				"message": "Configuration updated successfully",
			})
		}
	})

	s.router.DELETE("/api/v1/configs", func(c *gin.Context) {
		s.DeleteConfigFromHTTP(ctx, c)
		if !c.IsAborted() {
			c.JSON(http.StatusOK, gin.H{
				"status":  "ok",
				"message": "Configuration deleted successfully",
			})
		}
	})

	s.router.GET("/api/v1/configs", s.GetRouteState)

	s.router.POST("/api/v1/consumer_tokens/update", s.handleUpdateConsumerTokens)

	// Only register OAuth routes if OAuth2 is configured
	if s.auth.IsOAuth2Enabled() {
		// Create OAuth group with optional CORS middleware
		oauthGroup := s.router.Group("")
		if cors := s.auth.GetOAuth2CORS(); cors != nil {
			oauthCorsMiddleware := s.corsMiddleware(cors)
			s.router.OPTIONS("/*path", oauthCorsMiddleware)
			oauthGroup.Use(oauthCorsMiddleware)
		}

		// Register OAuth routes
		oauthGroup.GET("/.well-known/oauth-authorization-server", s.handleOAuthServerMetadata)
		// oauthGroup.GET("/.well-known/oauth-protected-resource", s.handleOAuthServerMetadata)
		oauthGroup.GET("/authorize", s.handleOAuthAuthorize)
		oauthGroup.POST("/authorize", s.handleOAuthAuthorize)
		oauthGroup.POST("/token", s.handleOAuthToken)
		oauthGroup.POST("/register", s.handleOAuthRegister)
		oauthGroup.POST("/revoke", s.handleOAuthRevoke)
	}

	loadedState, err := s.store.LoadState(ctx)
	if err != nil {
		s.logger.Error("Failed to load state", zap.Error(err))
	} else {
		s.logger.Info("State loaded successfully", zap.Any("state", loadedState))
		s.state = loadedState
	}
	s.logger.Debug("registering root handler")
	s.router.NoRoute(s.handleRoot)

	return nil
}

// handleRoot handles all unmatched routes
func (s *Server) handleRoot(c *gin.Context) {
	// Get the path
	path := c.Request.URL.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		s.logger.Debug("invalid path format",
			zap.String("path", path),
			zap.String("remote_addr", c.Request.RemoteAddr))
		s.sendProtocolError(c, nil, "Invalid path", http.StatusBadRequest, mcp.ErrorCodeInvalidRequest)
		return
	}
	endpoint := parts[len(parts)-1]
	prefix := "/" + strings.Join(parts[:len(parts)-1], "/")

	s.logger.Debug("routing request",
		zap.String("path", path),
		zap.String("prefix", prefix),
		zap.String("endpoint", endpoint),
		zap.String("remote_addr", c.Request.RemoteAddr))

	// Get runtime unit for the prefix
	runtimeUnit := s.state.GetRuntime(prefix)
	if runtimeUnit == nil {
		s.logger.Warn("invalid prefix, runtime unit not found",
			zap.String("prefix", prefix),
			zap.String("remote_addr", c.Request.RemoteAddr))
		s.sendProtocolError(c, nil, "Invalid prefix", http.StatusNotFound, mcp.ErrorCodeInvalidRequest)
		return
	}

	// Consumer Token Validation
	if runtimeUnit.Router != nil { // Check if ConsumerTokens field exists
		if len(runtimeUnit.Router.ConsumerTokens) == 0 {
			// If the list is empty, it means no consumer is allowed
			s.logger.Warn("consumer tokens list is empty, denying access",
				zap.String("prefix", prefix),
				zap.String("remote_addr", c.Request.RemoteAddr))
			s.sendProtocolError(c, nil, "No consumer tokens allowed for this route", http.StatusForbidden, mcp.ErrorCodeUnauthorized)
			return
		}

		consumerToken := c.GetHeader("X-Consumer-Token")
		if consumerToken == "" {
			s.logger.Warn("consumer token missing",
				zap.String("prefix", prefix),
				zap.String("remote_addr", c.Request.RemoteAddr))
			s.sendProtocolError(c, nil, "Consumer token missing", http.StatusForbidden, mcp.ErrorCodeUnauthorized)
			return
		}

		found := slices.Contains(runtimeUnit.Router.ConsumerTokens, consumerToken)

		if !found {
			s.logger.Warn("invalid consumer token",
				zap.String("prefix", prefix),
				zap.String("consumer_token", consumerToken),
				zap.String("remote_addr", c.Request.RemoteAddr))
			s.sendProtocolError(c, nil, "Invalid consumer token", http.StatusForbidden, mcp.ErrorCodeUnauthorized)
			return
		}
	}

	// Check auth configuration
	auth := s.state.GetAuth(prefix)
	if auth != nil && auth.Mode == cnst.AuthModeOAuth2 {
		// Validate access token
		if !s.isValidAccessToken(c.Request) {
			c.Header("WWW-Authenticate", `Bearer realm="OAuth", error="invalid_token", error_description="Missing or invalid access token"`)
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":             "invalid_token",
				"error_description": "Missing or invalid access token",
			})
			return
		}
	}

	// Dynamically set CORS
	if cors := s.state.GetCORS(prefix); cors != nil {
		s.logger.Debug("applying CORS middleware",
			zap.String("prefix", prefix))
		s.corsMiddleware(cors)(c)
		if c.IsAborted() {
			s.logger.Debug("request aborted by CORS middleware",
				zap.String("prefix", prefix),
				zap.String("remote_addr", c.Request.RemoteAddr))
			return
		}
	}

	protoType := s.state.GetProtoType(prefix)
	if protoType == "" {
		s.logger.Warn("invalid prefix",
			zap.String("prefix", prefix),
			zap.String("remote_addr", c.Request.RemoteAddr))
		s.sendProtocolError(c, nil, "Invalid prefix", http.StatusNotFound, mcp.ErrorCodeInvalidRequest)
		return
	}

	c.Status(http.StatusOK)
	switch endpoint {
	case "sse":
		s.logger.Debug("handling SSE endpoint",
			zap.String("prefix", prefix))
		s.handleSSE(c)
	case "message":
		s.logger.Debug("handling message endpoint",
			zap.String("prefix", prefix))
		s.handleMessage(c)
	case "mcp":
		s.logger.Debug("handling MCP endpoint",
			zap.String("prefix", prefix))
		s.handleMCP(c)
	default:
		s.logger.Warn("invalid endpoint",
			zap.String("endpoint", endpoint),
			zap.String("prefix", prefix),
			zap.String("remote_addr", c.Request.RemoteAddr))
		s.sendProtocolError(c, nil, "Invalid endpoint", http.StatusNotFound, mcp.ErrorCodeInvalidRequest)
	}
}

func (s *Server) Start() {
	go func() {
		if err := s.router.Run(fmt.Sprintf(":%d", s.port)); err != nil {
			s.logger.Error("failed to start server", zap.Error(err))
		}
	}()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(_ context.Context) error {
	s.logger.Info("shutting down server")
	close(s.shutdownCh)

	var wg sync.WaitGroup
	for prefix, transport := range s.state.GetTransports() {
		if transport.IsRunning() {
			wg.Add(1)
			go func(p string, t mcpproxy.Transport) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := t.Stop(ctx); err != nil {
					if err.Error() == "signal: interrupt" {
						s.logger.Info("transport stopped", zap.String("prefix", p))
						return
					}
					s.logger.Error("failed to stop transport",
						zap.String("prefix", p),
						zap.Error(err))
				}
			}(prefix, transport)
		}
	}
	wg.Wait()

	return nil
}

func (s *Server) UpdateConfigFromHTTP(ctx context.Context, c *gin.Context) {
	var configs []*config.MCPConfig
	if err := c.BindJSON(&configs); err != nil {
		s.logger.Error("failed to parse request body",
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body: " + err.Error(),
		})
		return
	}
	s.logger.Info("Updating MCP configuration")

	for _, cfg := range configs {
		if err := config.ValidateMCPConfig(cfg); err != nil {
			var validationErr *config.ValidationError
			if errors.As(err, &validationErr) {
				s.logger.Error("Configuration validation failed",
					zap.String("name", cfg.Name),
					zap.String("error", validationErr.Error()))
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "Configuration validation failed: " + validationErr.Error(),
				})
				return
			}
			s.logger.Error("failed to validate configuration",
				zap.String("name", cfg.Name),
				zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to validate configuration",
			})
			return
		}
	}

	// Get current state
	currentState := s.state
	if currentState == nil {
		// HTTP config，directly initialize the state
		s.logger.Warn("current state is nil, triggering reload")
		updatedState, err := state.BuildStateFromConfig(ctx, configs, currentState, s.logger)
		if err != nil {
			s.logger.Error("failed to build state from updated configs",
				zap.Error(err))
			return
		}
		s.state = updatedState
		return
	}


	// Build new state from updated configs
	updatedState, err := state.UpdateStateFromConfig(ctx, configs, currentState, s.logger)
	if err != nil {
		s.logger.Error("failed to build state from updated configs",
			zap.Error(err))
		return
	}

	// Log the changes
	s.logger.Info("Configuration updated",
		zap.Int("server_count", updatedState.GetServerCount()),
		zap.Int("tool_count", updatedState.GetToolCount()),
		zap.Int("router_count", updatedState.GetRouterCount()))

	// Atomically replace the state
	s.state = updatedState

}

func (s *Server) DeleteConfigFromHTTP(ctx context.Context, c *gin.Context) {
	var prefixes []string
	if err := c.BindJSON(&prefixes); err != nil {
		s.logger.Error("failed to parse request body",
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body: " + err.Error(),
		})
		return
	}

	if len(prefixes) == 0 {
		s.logger.Warn("empty prefixes list, nothing to delete")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Prefixes list cannot be empty",
		})
		return
	}

	s.logger.Info("Deleting MCP configuration")

	currentState := s.state
	if currentState == nil {
		// HTTP config，directly initialize the state
		s.logger.Warn("current state is nil, nothing to delete")
		return
	}
	// Remove configurations with the specified prefixes
	currentState.DeleteRuntimeByPrefixes(ctx, prefixes, s.logger)

	// Log the changes
	s.logger.Info("Configuration deleted",
		zap.Int("server_count", currentState.GetServerCount()),
		zap.Int("tool_count", currentState.GetToolCount()),
		zap.Int("router_count", currentState.GetRouterCount()))
}

func (s *Server) GetRouteState(c *gin.Context) {
	if s.state == nil {
		c.JSON(http.StatusOK, gin.H{
			"error":  "Server state is not initialized",
			"routes": map[string]interface{}{},
		})
		return
	}

	routesMap := make(map[string]interface{})
	for k, v := range s.state.GetRouteStateMap() {
		routesMap[string(k)] = v
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"routes": routesMap,
	})
}

func (s *Server) SaveState(ctx context.Context) {
	err := s.store.SaveState(ctx, s.state)
	if err != nil {
		s.logger.Error("Failed to save state", zap.Error(err))
	} else {
		s.logger.Info("State saved successfully!")
	}
}

// rebuildState rebuilds the server's state from the given configurations
func (s *Server) rebuildState(ctx context.Context, configs []*config.MCPConfig) error {
	updatedState, err := state.BuildStateFromConfig(ctx, configs, s.state, s.logger)
	if err != nil {
		s.logger.Error("failed to build state from updated configs during rebuild",
			zap.Error(err))
		return fmt.Errorf("failed to rebuild state: %w", err)
	}
	s.state = updatedState
	s.logger.Info("Server state rebuilt successfully")
	return nil
}

func (s *Server) handleUpdateConsumerTokens(c *gin.Context) {
	var req struct {
		OldToken string   `json:"oldToken"`
		NewToken string   `json:"newToken"`
		Prefixes []string `json:"prefixes"` // Add prefixes field
	}
	if err := c.BindJSON(&req); err != nil {
		s.logger.Error("failed to parse request body for consumer token update",
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body: " + err.Error(),
		})
		return
	}

	if req.OldToken == "" || req.NewToken == "" {
		s.logger.Warn("oldToken or newToken is empty for consumer token update")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "oldToken and newToken cannot be empty",
		})
		return
	}

	// Call the updated method in state
	changed := s.state.UpdateConsumerToken(req.OldToken, req.NewToken, req.Prefixes)
 
	if changed {
		// Trigger a state update to apply changes using the new rebuildState method
		if err := s.rebuildState(c.Request.Context(), s.state.GetRawConfigs()); err != nil {
			s.logger.Error("failed to rebuild state after consumer token update",
				zap.Error(err))
			s.sendProtocolError(c, err, "Failed to rebuild state after token update", http.StatusInternalServerError, mcp.ErrorCodeInternalError)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"message": "Consumer token updated and configuration reloaded successfully",
		})
	} else {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"message": "No matching consumer token found to update in specified prefixes",
		})
	}
}

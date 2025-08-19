package core

import (
	"bytes"   // Add bytes import
	"context" // Add context import
	"io"      // Add io import
	"net/http"
	"runtime"
	"strings"
	"time"

	"mcp-gateway/internal/common/config"
	"mcp-gateway/internal/common/cnst"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// responseBodyWriter is a custom Gin response writer to capture the response body
type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write writes the response body to the buffer
func (w responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// WriteString writes the response body string to the buffer
func (w responseBodyWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

// loggerMiddleware logs incoming requests and outgoing responses
func (s *Server) loggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Record basic information at request start time using Info level
		startTime := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		// Extract MCP service identifier
		serviceIdentifier := "unknown"
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) >= 2 {
			serviceIdentifier = "/" + strings.Join(parts[:len(parts)-1], "/")
		}

		// Record basic information for all requests
		logger := s.logger.With(
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("remote_addr", c.Request.RemoteAddr),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.String("service_identifier", serviceIdentifier), // Add service identifier
		)

		// Use Debug level to record more detailed request information
		headers := make(map[string]string)
		for k, v := range c.Request.Header {
			// Filter out sensitive header information
			if k != "Authorization" && k != "Cookie" {
				headers[k] = strings.Join(v, ", ")
			}
		}

		// Read request body
		var requestBody []byte
		if c.Request.Body != nil {
			requestBody, _ = c.GetRawData()
			// Restore the body for subsequent handlers
			c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
		}

		if s.logger.Core().Enabled(zap.DebugLevel) {
			logger.Debug("request details",
				zap.String("query", query),
				zap.Any("headers", headers),
				zap.ByteString("body", requestBody), // Log body in debug
			)
		}

		// Record request start
		logger.Info("request started")

		// Save logger and request body in context for later use
		c.Set("logger", logger)
		c.Set("request_body", requestBody) // Store raw body in context

		// Wrap the response writer to capture the response body
		w := &responseBodyWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
		c.Writer = w

		c.Next()

		// Calculate request processing time
		latency := time.Since(startTime)
		statusCode := c.Writer.Status()
		clientIP := c.ClientIP()
		responseSize := c.Writer.Size()
		responseBody := w.body.String() // Get the captured response body

		// Determine if the request was successful
		success := statusCode >= 200 && statusCode < 400

		// Extract session ID if present (for SSE connections)
		sessionID := c.Query("sessionId")
		if sessionID == "" && strings.HasSuffix(path, "/sse") {
			// For initial SSE connection, session ID is generated later.
			// We can't get it here reliably for the initial request.
			// It will be added to the SSE event logs.
		}

		// Extract consumer token using the generic method
		consumerToken := s.getConsumerToken(c)

		// Prepare log entry for Kafka
		logEntry := map[string]any{
			"log_type":           "http_request", // Distinguish from SSE event logs
			"timestamp":          time.Now().Format(time.RFC3339),
			"method":             c.Request.Method,
			"path":               path,
			"query":              query,
			"remote_addr":        c.Request.RemoteAddr,
			"user_agent":         c.Request.UserAgent(),
			"service_identifier": serviceIdentifier,
			"status_code":        statusCode,
			"latency_ms":         latency.Milliseconds(),
			"client_ip":          clientIP,
			"response_size":      responseSize,
			"headers":            headers, // Include headers in Kafka log
			"request_body":       string(requestBody), // Include request body in Kafka log
			"response_body":      responseBody,        // Include response body in Kafka log
			"success":            success, // Add success status
			"node_ip":            s.nodeIP,  // Add node IP
		}

		// Add session_id and consumer_token if available
		if sessionID != "" {
			logEntry["session_id"] = sessionID
		}
		if consumerToken != "" {
			logEntry["consumer_token"] = consumerToken
		}

		// Send log to Kafka if producer is initialized
		if s.kafkaProducer != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				err := s.kafkaProducer.Produce(ctx, cnst.KafkaTopicHttpRequest, serviceIdentifier, logEntry) // Use constant for HTTP topic
				if err != nil {
					s.logger.Error("failed to send log to Kafka", zap.Error(err), zap.String("service_identifier", serviceIdentifier))
				}
			}()
		}

		// Choose log level based on status code for local logging
		if statusCode >= 500 {
			// Use Error level for server errors
			logger.Error("request completed with server error",
				zap.Int("status", statusCode),
				zap.Int("size", responseSize),
				zap.Duration("latency", latency),
				zap.String("client_ip", clientIP),
			)
		} else if statusCode >= 400 {
			// Use Warn level for client errors
			logger.Warn("request completed with client error",
				zap.Int("status", statusCode),
				zap.Int("size", responseSize),
				zap.Duration("latency", latency),
				zap.String("client_ip", clientIP),
			)
		} else {
			// Use Info level for normal status
			logger.Info("request completed successfully",
				zap.Int("status", statusCode),
				zap.Int("size", responseSize),
				zap.Duration("latency", latency),
				zap.String("client_ip", clientIP),
			)
		}
	}
}

// recoveryMiddleware recovers from panics and returns 500 error
func (s *Server) recoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Get stack information
				stack := make([]byte, 4096)
				length := runtime.Stack(stack, false)

				// Get request related information
				httpRequest := c.Request
				headers := make(map[string]string)
				for k, v := range httpRequest.Header {
					if k != "Authorization" && k != "Cookie" {
						headers[k] = strings.Join(v, ", ")
					}
				}

				// Record panic information with Error level
				s.logger.Error("panic recovered",
					zap.Any("error", err),
					zap.String("path", c.Request.URL.Path),
					zap.String("method", c.Request.Method),
					zap.String("remote_addr", c.Request.RemoteAddr),
					zap.String("client_ip", c.ClientIP()),
					zap.Any("request_headers", headers),
					zap.ByteString("stack", stack[:length]),
				)

				// Return 500 error
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "internal server error",
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}

// corsMiddleware handles CORS configuration
func (s *Server) corsMiddleware(cors *config.CORSConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin == "" {
			c.Next()
			return
		}

		allowed := false
		for _, allowedOrigin := range cors.AllowOrigins {
			if allowedOrigin == "*" || origin == allowedOrigin {
				allowed = true
				c.Header("Access-Control-Allow-Origin", origin)
				break
			}
		}

		if !allowed {
			c.Next()
			return
		}

		if len(cors.AllowMethods) > 0 {
			c.Header("Access-Control-Allow-Methods", strings.Join(cors.AllowMethods, ", "))
		}

		if len(cors.AllowHeaders) > 0 {
			c.Header("Access-Control-Allow-Headers", strings.Join(cors.AllowHeaders, ", "))
		}

		if len(cors.ExposeHeaders) > 0 {
			c.Header("Access-Control-Expose-Headers", strings.Join(cors.ExposeHeaders, ", "))
		}

		if cors.AllowCredentials {
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

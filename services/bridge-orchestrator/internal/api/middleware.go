package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// LoggerMiddleware provides structured logging for HTTP requests
func LoggerMiddleware(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Calculate latency
		latency := time.Since(start)

		// Get response details
		status := c.Writer.Status()
		method := c.Request.Method
		userAgent := c.Request.UserAgent()
		clientIP := c.ClientIP()

		// Build full path
		if raw != "" {
			path = path + "?" + raw
		}

		// Log fields
		fields := []zap.Field{
			zap.Int("status", status),
			zap.String("method", method),
			zap.String("path", path),
			zap.String("ip", clientIP),
			zap.Duration("latency", latency),
			zap.String("user_agent", userAgent),
		}

		// Add request ID if available
		if requestID := c.GetHeader("X-Request-ID"); requestID != "" {
			fields = append(fields, zap.String("request_id", requestID))
		}

		// Add user address if available (from auth middleware)
		if userAddr, exists := c.Get("user_address"); exists {
			fields = append(fields, zap.String("user_address", userAddr.(string)))
		}

		// Log based on status code
		switch {
		case status >= 500:
			logger.Error("Server error", fields...)
		case status >= 400:
			logger.Warn("Client error", fields...)
		case status >= 300:
			logger.Info("Redirect", fields...)
		default:
			logger.Info("Request completed", fields...)
		}
	}
}

// CORSMiddleware handles Cross-Origin Resource Sharing
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		
		// In production, you should validate origins against a whitelist
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
		} else {
			c.Header("Access-Control-Allow-Origin", "*")
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, X-User-Address")
		c.Header("Access-Control-Expose-Headers", "X-Request-ID, X-Rate-Limit-Remaining")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Max-Age", "86400") // 24 hours

		// Handle preflight requests
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// SecurityMiddleware adds security headers
func SecurityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Security headers
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", "default-src 'self'")
		
		// Remove sensitive headers
		c.Header("Server", "")
		c.Header("X-Powered-By", "")

		c.Next()
	}
}

// RequestIDMiddleware adds a unique request ID to each request
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
		}

		c.Header("X-Request-ID", requestID)
		c.Set("request_id", requestID)

		c.Next()
	}
}

// RateLimitMiddleware implements basic rate limiting
func RateLimitMiddleware(maxRequests int, window time.Duration) gin.HandlerFunc {
	// Simple in-memory rate limiter (use Redis in production)
	type client struct {
		requests    int
		resetTime   time.Time
	}

	clients := make(map[string]*client)
	
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		now := time.Now()

		// Get or create client
		cl, exists := clients[clientIP]
		if !exists || now.After(cl.resetTime) {
			clients[clientIP] = &client{
				requests:  1,
				resetTime: now.Add(window),
			}
			c.Header("X-Rate-Limit-Remaining", fmt.Sprintf("%d", maxRequests-1))
			c.Next()
			return
		}

		// Check rate limit
		if cl.requests >= maxRequests {
			c.Header("X-Rate-Limit-Remaining", "0")
			c.Header("Retry-After", fmt.Sprintf("%.0f", time.Until(cl.resetTime).Seconds()))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
				"retry_after": cl.resetTime.Unix(),
			})
			c.Abort()
			return
		}

		// Increment requests
		cl.requests++
		c.Header("X-Rate-Limit-Remaining", fmt.Sprintf("%d", maxRequests-cl.requests))
		c.Next()
	}
}

// AuthMiddleware handles authentication (simplified for hackathon)
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// In production, validate JWT tokens here
		// For hackathon, we'll use a simple header-based auth
		
		userAddress := c.GetHeader("X-User-Address")
		if userAddress != "" {
			// Basic validation of Ethereum address format
			if len(userAddress) == 42 && userAddress[:2] == "0x" {
				c.Set("user_address", userAddress)
				c.Set("authenticated", true)
			}
		}

		c.Next()
	}
}

// RequireAuthMiddleware ensures the user is authenticated
func RequireAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authenticated, exists := c.Get("authenticated")
		if !exists || !authenticated.(bool) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Authentication required",
				"code":  ErrCodeUnauthorized,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// ValidationMiddleware handles request validation errors
func ValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// Check for validation errors in the response
		if len(c.Errors) > 0 {
			err := c.Errors.Last()
			c.JSON(http.StatusBadRequest, gin.H{
				"error":     "Validation failed",
				"details":   err.Error(),
				"code":      ErrCodeValidation,
				"timestamp": time.Now().UTC(),
			})
		}
	}
}

// ErrorHandlerMiddleware handles panics and errors
func ErrorHandlerMiddleware(logger *zap.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, err interface{}) {
		logger.Error("Panic recovered",
			zap.Any("error", err),
			zap.String("path", c.Request.URL.Path),
			zap.String("method", c.Request.Method),
			zap.String("ip", c.ClientIP()),
		)

		c.JSON(http.StatusInternalServerError, gin.H{
			"error":     "Internal server error",
			"code":      ErrCodeInternalError,
			"timestamp": time.Now().UTC(),
		})
	})
}

// HealthCheckSkipMiddleware skips certain middleware for health check endpoints
func HealthCheckSkipMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		
		// Skip heavy middleware for health checks
		if path == "/health" || path == "/ready" {
			c.Set("skip_auth", true)
			c.Set("skip_rate_limit", true)
		}

		c.Next()
	}
}

// MetricsMiddleware collects request metrics
func MetricsMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        
        c.Next()
        
        duration := time.Since(start)
        status := c.Writer.Status()
        method := c.Request.Method
        path := c.FullPath()

        // Get logger from context
        if loggerInterface, exists := c.Get("logger"); exists {
            if logger, ok := loggerInterface.(*zap.Logger); ok {
                logger.Debug("Request metrics",
                    zap.String("method", method),
                    zap.String("path", path),
                    zap.Int("status", status),
                    zap.Duration("duration", duration),
                )
            }
        }
    }
}


// CompressionMiddleware handles response compression
func CompressionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Simple gzip compression (use gin-contrib/gzip in production)
		acceptEncoding := c.GetHeader("Accept-Encoding")
		if acceptEncoding != "" {
			// For simplicity, we'll just set the header
			// In production, implement actual compression
			c.Header("Content-Encoding", "identity")
		}

		c.Next()
	}
}



// isHealthCheckPath checks if the path is a health check endpoint
func isHealthCheckPath(path string) bool {
	healthPaths := []string{"/health", "/ready", "/metrics"}
	for _, hp := range healthPaths {
		if path == hp {
			return true
		}
	}
	return false
}

// SetupMiddleware configures all middleware in the correct order
func SetupMiddleware(router *gin.Engine, logger *zap.Logger) {
	// Order matters! These should be applied in this sequence:
	
	// 1. Error handling and recovery (first to catch everything)
	router.Use(ErrorHandlerMiddleware(logger))
	
	// 2. Request ID (early for tracking)
	router.Use(RequestIDMiddleware())
	
	// 3. Security headers
	router.Use(SecurityMiddleware())
	
	// 4. CORS (before other processing)
	router.Use(CORSMiddleware())
	
	// 5. Health check skip logic
	router.Use(HealthCheckSkipMiddleware())
	
	// 6. Logging (after request ID, before business logic)
	router.Use(LoggerMiddleware(logger))
	
	// 7. Metrics collection
	router.Use(MetricsMiddleware())
	
	// 8. Rate limiting (before auth)
	router.Use(RateLimitMiddleware(100, time.Minute)) // 100 requests per minute
	
	// 9. Authentication (after rate limiting)
	router.Use(AuthMiddleware())
	
	// 10. Validation handling
	router.Use(ValidationMiddleware())
	
	// 11. Compression (last processing middleware)
	router.Use(CompressionMiddleware())
}

// Middleware configuration constants
const (
	DefaultRateLimit     = 100
	DefaultRateWindow    = time.Minute
	DefaultRequestTimeout = 30 * time.Second
	MaxRequestSize       = 10 << 20 
)

// Production middleware configurations
type MiddlewareConfig struct {
	EnableAuth        bool
	EnableRateLimit   bool
	EnableCompression bool
	EnableMetrics     bool
	RateLimit         int
	RateWindow        time.Duration
	JWTSecret         string
	AllowedOrigins    []string
}

// NewProductionMiddleware creates middleware configuration for production
func NewProductionMiddleware() *MiddlewareConfig {
	return &MiddlewareConfig{
		EnableAuth:        true,
		EnableRateLimit:   true,
		EnableCompression: true,
		EnableMetrics:     true,
		RateLimit:         1000,
		RateWindow:        time.Hour,
		AllowedOrigins:    []string{"https://app.flowfusion.io"},
	}
}

// NewDevelopmentMiddleware creates middleware configuration for development
func NewDevelopmentMiddleware() *MiddlewareConfig {
	return &MiddlewareConfig{
		EnableAuth:        false,
		EnableRateLimit:   false,
		EnableCompression: false,
		EnableMetrics:     true,
		RateLimit:         10000,
		RateWindow:        time.Minute,
		AllowedOrigins:    []string{"*"},
	}
}
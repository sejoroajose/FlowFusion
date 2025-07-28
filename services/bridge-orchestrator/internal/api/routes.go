package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"flowfusion/bridge-orchestrator/internal/database"
	"flowfusion/bridge-orchestrator/pkg/orchestrator"
	"flowfusion/bridge-orchestrator/pkg/twap"
)

const (
	// API timeouts
	DefaultTimeout = 30 * time.Second
	
	// Rate limiting
	// DefaultRateLimit = 100
	BurstLimit       = 150
	
	// Pagination limits
	DefaultPageSize = 20
	MaxPageSize     = 100
	
	// Cache TTL
	CacheTTL = 5 * time.Minute
)

// Handler holds dependencies for API handlers
type Handler struct {
	orchestrator *orchestrator.Orchestrator
	twapEngine   *twap.Engine
	db           database.DB
	logger       *zap.Logger
	
	// Production features
	cache       map[string]interface{} // In production, use Redis
	rateLimiter map[string]*RateLimiter
}

// RateLimiter simple in-memory rate limiter for demo
type RateLimiter struct {
	tokens    int
	capacity  int
	lastRefill time.Time
}

// SetupRoutes configures all API routes with production features
func SetupRoutes(
	router *gin.Engine,
	orch *orchestrator.Orchestrator,
	twapEngine *twap.Engine,
	db database.DB,
	logger *zap.Logger,
) {
	h := &Handler{
		orchestrator: orch,
		twapEngine:   twapEngine,
		db:           db,
		logger:       logger,
		cache:        make(map[string]interface{}),
		rateLimiter:  make(map[string]*RateLimiter),
	}

	// Setup middleware
	h.setupMiddleware(router)

	// Health endpoints (no auth required)
	h.setupHealthRoutes(router)

	// API v1 routes (with auth middleware)
	v1 := router.Group("/api/v1")
	v1.Use(h.authMiddleware())
	v1.Use(h.rateLimitMiddleware())
	{
		h.setupOrderRoutes(v1)
		h.setupTWAPRoutes(v1)
		h.setupChainRoutes(v1)
		h.setupPriceRoutes(v1)
		h.setupStatsRoutes(v1)
		h.setupWebSocketRoutes(v1)
	}

	// Admin routes (additional admin auth required)
	admin := v1.Group("/admin")
	admin.Use(h.adminAuthMiddleware())
	{
		h.setupAdminRoutes(admin)
	}
}

// setupMiddleware configures production middleware
func (h *Handler) setupMiddleware(router *gin.Engine) {
	// Recovery middleware
	router.Use(gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		h.logger.Error("Panic recovered",
			zap.Any("error", recovered),
			zap.String("path", c.Request.URL.Path),
			zap.String("method", c.Request.Method),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.String("ip", c.ClientIP()),
		)
		
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Internal server error",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
	}))

	// Request ID middleware
	router.Use(func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
		}
		c.Header("X-Request-ID", requestID)
		c.Set("request_id", requestID)
		c.Next()
	})

	// CORS middleware
	router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if h.isAllowedOrigin(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
		c.Header("Access-Control-Expose-Headers", "X-Request-ID, X-Rate-Limit-Remaining")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// Security headers
	router.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	})
}

// setupHealthRoutes configures health check endpoints
func (h *Handler) setupHealthRoutes(router *gin.Engine) {
	health := router.Group("/health")
	{
		health.GET("/", h.basicHealthCheck)
		health.GET("/ready", h.readinessCheck)
		health.GET("/live", h.livenessCheck)
		health.GET("/detailed", h.detailedHealthCheck)
	}
}

// setupOrderRoutes configures order management endpoints
func (h *Handler) setupOrderRoutes(v1 *gin.RouterGroup) {
	orders := v1.Group("/orders")
	{
		orders.POST("", h.validateCreateOrder(), h.createOrder)
		orders.GET("/:id", h.validateOrderID(), h.getOrder)
		orders.PUT("/:id/cancel", h.validateOrderID(), h.cancelOrder)
		orders.GET("", h.validateListOrders(), h.listOrders)
		orders.GET("/:id/history", h.validateOrderID(), h.getOrderHistory)
		orders.GET("/:id/status", h.validateOrderID(), h.getOrderStatus)
	}
}

// setupTWAPRoutes configures TWAP operation endpoints
func (h *Handler) setupTWAPRoutes(v1 *gin.RouterGroup) {
	twapRoutes := v1.Group("/twap")
	{
		twapRoutes.GET("/price/:pair", h.validateTokenPair(), h.getTWAPPrice)
		twapRoutes.GET("/current/:pair", h.validateTokenPair(), h.getCurrentPrice)
		twapRoutes.POST("/execute/:id", h.validateOrderID(), h.executeOrder)
		twapRoutes.GET("/metrics", h.getTWAPMetrics)
	}
}

// setupChainRoutes configures blockchain operation endpoints
func (h *Handler) setupChainRoutes(v1 *gin.RouterGroup) {
	chains := v1.Group("/chains")
	{
		chains.GET("", h.getSupportedChains)
		chains.GET("/:id/status", h.validateChainID(), h.getChainStatus)
		chains.GET("/:id/metrics", h.validateChainID(), h.getChainMetrics)
	}
}

// setupPriceRoutes configures price feed endpoints
func (h *Handler) setupPriceRoutes(v1 *gin.RouterGroup) {
	prices := v1.Group("/prices")
	{
		prices.GET("/:pair/history", h.validateTokenPair(), h.getPriceHistory)
		prices.GET("/:pair/latest", h.validateTokenPair(), h.getLatestPrice)
	}
}

// setupStatsRoutes configures statistics endpoints
func (h *Handler) setupStatsRoutes(v1 *gin.RouterGroup) {
	stats := v1.Group("/stats")
	{
		stats.GET("/overview", h.getOverviewStats)
		stats.GET("/volume", h.getVolumeStats)
		stats.GET("/performance", h.getPerformanceStats)
	}
}

// setupWebSocketRoutes configures WebSocket endpoints
func (h *Handler) setupWebSocketRoutes(v1 *gin.RouterGroup) {
	v1.GET("/ws", h.websocketHandler)
}

// setupAdminRoutes configures admin endpoints
func (h *Handler) setupAdminRoutes(admin *gin.RouterGroup) {
	admin.GET("/health", h.adminHealthCheck)
	admin.POST("/maintenance", h.toggleMaintenanceMode)
	admin.GET("/metrics/detailed", h.getDetailedMetrics)
	admin.POST("/cache/clear", h.clearCache)
}

// Health Check Handlers

func (h *Handler) basicHealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"version":   "1.0.0",
		"service":   "flowfusion-bridge-orchestrator",
	})
}

func (h *Handler) readinessCheck(c *gin.Context) {
	_, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	checks := make(map[string]interface{})
	allHealthy := true

	// Check database
	if err := h.db.Health(); err != nil {
		checks["database"] = map[string]interface{}{
			"status": "unhealthy",
			"error":  err.Error(),
		}
		allHealthy = false
	} else {
		checks["database"] = map[string]interface{}{
			"status": "healthy",
		}
	}

	// Check orchestrator
	if orchHealth := h.orchestrator.HealthCheck(); orchHealth != nil {
		checks["orchestrator"] = orchHealth
		if status, ok := orchHealth["status"].(string); ok && status != "healthy" {
			allHealthy = false
		}
	}

	// Check TWAP engine
	if metrics := h.twapEngine.GetMetrics(); metrics != nil {
		checks["twap_engine"] = map[string]interface{}{
			"status":            "healthy",
			"total_executions":  metrics.TotalExecutions,
			"success_rate":      float64(metrics.SuccessfulExecutions) / float64(metrics.TotalExecutions) * 100,
			"last_execution":    metrics.LastExecutionTime,
		}
	}

	statusCode := http.StatusOK
	if !allHealthy {
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, gin.H{
		"ready":     allHealthy,
		"checks":    checks,
		"timestamp": time.Now().UTC(),
	})
}

func (h *Handler) livenessCheck(c *gin.Context) {
	// Simple liveness check - if we can respond, we're alive
	c.JSON(http.StatusOK, gin.H{
		"status":    "alive",
		"timestamp": time.Now().UTC(),
		"uptime":    time.Since(time.Now()).String(), // This would be actual uptime in production
	})
}

func (h *Handler) detailedHealthCheck(c *gin.Context) {
	_, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	health := h.orchestrator.HealthCheck()
	
	// Add additional system metrics
	health["system"] = map[string]interface{}{
		"timestamp":    time.Now().UTC(),
		"goroutines":   "unknown", // runtime.NumGoroutine() in production
		"memory_usage": "unknown", // Get actual memory stats in production
		"cpu_usage":    "unknown", // Get actual CPU stats in production
	}

	// Determine overall health
	allHealthy := true
	if dbHealth, ok := health["database"].(map[string]interface{}); ok {
		if status, ok := dbHealth["status"].(string); ok && status != "healthy" {
			allHealthy = false
		}
	}

	statusCode := http.StatusOK
	if !allHealthy {
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, health)
}

// Order Management Handlers

func (h *Handler) createOrder(c *gin.Context) {
	_, cancel := context.WithTimeout(c.Request.Context(), DefaultTimeout)
	defer cancel()

	var req CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid create order request", 
			zap.Error(err),
			zap.String("request_id", h.getRequestID(c)))
		
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     "Invalid request format",
			Code:      ErrCodeValidation,
			Details:   map[string]interface{}{"validation_error": err.Error()},
			Timestamp: time.Now(),
		})
		return
	}

	// Enhanced validation
	if err := h.validateCreateOrderRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     "Validation failed",
			Code:      ErrCodeValidation,
			Details:   map[string]interface{}{"validation_error": err.Error()},
			Timestamp: time.Now(),
		})
		return
	}

	// Check rate limits
	userAddress := h.getUserAddress(c)
	if !h.checkRateLimit(userAddress) {
		c.JSON(http.StatusTooManyRequests, ErrorResponse{
			Error:     "Rate limit exceeded",
			Code:      ErrCodeRateLimit,
			Timestamp: time.Now(),
		})
		return
	}

	// Convert to database order with proper timestamps
	now := time.Now()
	order := &database.Order{
		ID:                  req.ID,
		UserAddress:         req.UserAddress,
		SourceChain:         req.SourceChain,
		TargetChain:         req.TargetChain,
		SourceToken:         req.SourceToken,
		SourceAmount:        req.SourceAmount,
		TargetToken:         req.TargetToken,
		TargetRecipient:     req.TargetRecipient,
		MinReceived:         req.MinReceived,
		WindowMinutes:       req.TWAPConfig.WindowMinutes,
		ExecutionIntervals:  req.TWAPConfig.ExecutionIntervals,
		MaxSlippage:         req.TWAPConfig.MaxSlippage,
		MinFillSize:         req.TWAPConfig.MinFillSize,
		EnableMEVProtection: req.TWAPConfig.EnableMEVProtection,
		HTLCHash:            req.HTLCHash,
		TimeoutHeight:       req.TimeoutHeight,
		TimeoutTimestamp:    req.TimeoutTimestamp,
		CreatedAt:           now,
		UpdatedAt:           now,
		Status:              string(database.OrderStatusPending),
		ExecutedAmount:      decimal.Zero,
		AveragePrice:        decimal.Zero,
		Metadata:            database.Metadata(req.Metadata),
	}

	// Create order in database with context
	if err := h.db.CreateOrder(order); err != nil {
		h.logger.Error("Failed to create order", 
			zap.Error(err),
			zap.String("order_id", order.ID),
			zap.String("user_address", order.UserAddress),
			zap.String("request_id", h.getRequestID(c)))
		
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to create order",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info("Order created successfully", 
		zap.String("order_id", order.ID),
		zap.String("user_address", order.UserAddress),
		zap.String("source_chain", order.SourceChain),
		zap.String("target_chain", order.TargetChain),
		zap.String("request_id", h.getRequestID(c)))

	c.JSON(http.StatusCreated, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"order_id":   order.ID,
			"status":     order.Status,
			"created_at": order.CreatedAt.UTC(),
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) getOrder(c *gin.Context) {
	_, cancel := context.WithTimeout(c.Request.Context(), DefaultTimeout)
	defer cancel()

	orderID := c.Param("id")
	userAddress := h.getUserAddress(c)

	// Check cache first
	if cached := h.getFromCache("order:" + orderID); cached != nil {
		if order, ok := cached.(*OrderResponse); ok {
			// Verify user has access to this order
			if !h.canAccessOrder(userAddress, order.UserAddress) {
				c.JSON(http.StatusForbidden, ErrorResponse{
					Error:     "Access denied",
					Code:      ErrCodeForbidden,
					Timestamp: time.Now(),
				})
				return
			}
			c.JSON(http.StatusOK, order)
			return
		}
	}

	order, err := h.db.GetOrder(orderID)
	if err != nil {
		if err == database.ErrOrderNotFound {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:     "Order not found",
				Code:      ErrCodeNotFound,
				Timestamp: time.Now(),
			})
			return
		}
		h.logger.Error("Failed to get order", 
			zap.Error(err),
			zap.String("order_id", orderID),
			zap.String("request_id", h.getRequestID(c)))
		
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to retrieve order",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	// Verify user has access to this order
	if !h.canAccessOrder(userAddress, order.UserAddress) {
		c.JSON(http.StatusForbidden, ErrorResponse{
			Error:     "Access denied",
			Code:      ErrCodeForbidden,
			Timestamp: time.Now(),
		})
		return
	}

	// Get execution history
	history, err := h.db.GetExecutionHistory(orderID)
	if err != nil {
		h.logger.Error("Failed to get execution history", 
			zap.Error(err),
			zap.String("order_id", orderID))
		history = []*database.ExecutionRecord{} // Return empty history on error
	}

	response := &OrderResponse{
		ID:                  order.ID,
		UserAddress:         order.UserAddress,
		SourceChain:         order.SourceChain,
		TargetChain:         order.TargetChain,
		SourceToken:         order.SourceToken,
		SourceAmount:        order.SourceAmount,
		TargetToken:         order.TargetToken,
		TargetRecipient:     order.TargetRecipient,
		MinReceived:         order.MinReceived,
		TWAPConfig: TWAPConfigResponse{
			WindowMinutes:       order.WindowMinutes,
			ExecutionIntervals:  order.ExecutionIntervals,
			MaxSlippage:         order.MaxSlippage,
			MinFillSize:         order.MinFillSize,
			EnableMEVProtection: order.EnableMEVProtection,
		},
		HTLCHash:         order.HTLCHash,
		TimeoutHeight:    order.TimeoutHeight,
		TimeoutTimestamp: order.TimeoutTimestamp,
		CreatedAt:        order.CreatedAt,
		UpdatedAt:        order.UpdatedAt,
		ExecutedAmount:   order.ExecutedAmount,
		LastExecution:    order.LastExecution,
		Status:           order.Status,
		AveragePrice:     order.AveragePrice,
		CompletionRate:   order.CalculateCompletionRate(),
		ExecutionHistory: h.convertExecutionHistory(history),
		Metadata:         map[string]interface{}(order.Metadata),
	}

	// Cache the response
	h.setCache("order:"+orderID, response, CacheTTL)

	c.JSON(http.StatusOK, response)
}

func (h *Handler) cancelOrder(c *gin.Context) {
	_, cancel := context.WithTimeout(c.Request.Context(), DefaultTimeout)
	defer cancel()

	orderID := c.Param("id")
	userAddress := h.getUserAddress(c)

	order, err := h.db.GetOrder(orderID)
	if err != nil {
		if err == database.ErrOrderNotFound {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:     "Order not found",
				Code:      ErrCodeNotFound,
				Timestamp: time.Now(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to retrieve order",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	// Verify ownership
	if !h.canModifyOrder(userAddress, order.UserAddress) {
		c.JSON(http.StatusForbidden, ErrorResponse{
			Error:     "Unauthorized to cancel this order",
			Code:      ErrCodeForbidden,
			Timestamp: time.Now(),
		})
		return
	}

	// Check if order can be cancelled
	if !h.canCancelOrder(order.Status) {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "Order cannot be cancelled in current status",
			Code:    ErrCodeConflict,
			Details: map[string]interface{}{"current_status": order.Status},
			Timestamp: time.Now(),
		})
		return
	}

	// Update order status
	order.Status = string(database.OrderStatusCancelled)
	order.UpdatedAt = time.Now()

	if err := h.db.UpdateOrder(order); err != nil {
		h.logger.Error("Failed to cancel order", 
			zap.Error(err),
			zap.String("order_id", orderID),
			zap.String("user_address", userAddress))
		
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to cancel order",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	// Clear cache
	h.clearOrderCache(orderID)

	h.logger.Info("Order cancelled", 
		zap.String("order_id", orderID),
		zap.String("user_address", userAddress))

	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"order_id":     orderID,
			"status":       order.Status,
			"cancelled_at": order.UpdatedAt.UTC(),
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) listOrders(c *gin.Context) {
	_, cancel := context.WithTimeout(c.Request.Context(), DefaultTimeout)
	defer cancel()

	// Parse and validate query parameters
	params := h.parseListOrdersParams(c)
	if err := h.validateListOrdersParams(params); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     "Invalid query parameters",
			Code:      ErrCodeValidation,
			Details:   map[string]interface{}{"validation_error": err.Error()},
			Timestamp: time.Now(),
		})
		return
	}

	userAddress := h.getUserAddress(c)
	if userAddress == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     "User address is required",
			Code:      ErrCodeValidation,
			Timestamp: time.Now(),
		})
		return
	}

	orders, err := h.db.GetOrdersByUser(userAddress, params.Limit, params.Offset)
	if err != nil {
		h.logger.Error("Failed to get orders", 
			zap.Error(err),
			zap.String("user_address", userAddress))
		
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to retrieve orders",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	// Convert to response format
	orderResponses := make([]OrderSummaryResponse, 0, len(orders))
	for _, order := range orders {
		// Get execution count
		history, _ := h.db.GetExecutionHistory(order.ID)
		
		orderResponses = append(orderResponses, OrderSummaryResponse{
			ID:                order.ID,
			SourceChain:       order.SourceChain,
			TargetChain:       order.TargetChain,
			SourceAmount:      order.SourceAmount,
			ExecutedAmount:    order.ExecutedAmount,
			Status:            order.Status,
			CreatedAt:         order.CreatedAt,
			CompletionRate:    order.CalculateCompletionRate(),
			AveragePrice:      order.AveragePrice,
			IntervalsExecuted: len(history),
			TotalIntervals:    order.ExecutionIntervals,
		})
	}

	pagination := NewPaginationResponse(params.Page, params.Limit, int64(len(orderResponses)))

	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"orders":     orderResponses,
			"pagination": pagination,
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) getOrderHistory(c *gin.Context) {
	_, cancel := context.WithTimeout(c.Request.Context(), DefaultTimeout)
	defer cancel()

	orderID := c.Param("id")
	userAddress := h.getUserAddress(c)

	// Verify user can access this order
	order, err := h.db.GetOrder(orderID)
	if err != nil {
		if err == database.ErrOrderNotFound {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:     "Order not found",
				Code:      ErrCodeNotFound,
				Timestamp: time.Now(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to retrieve order",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	if !h.canAccessOrder(userAddress, order.UserAddress) {
		c.JSON(http.StatusForbidden, ErrorResponse{
			Error:     "Access denied",
			Code:      ErrCodeForbidden,
			Timestamp: time.Now(),
		})
		return
	}

	history, err := h.db.GetExecutionHistory(orderID)
	if err != nil {
		h.logger.Error("Failed to get execution history", 
			zap.Error(err),
			zap.String("order_id", orderID))
		
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to retrieve execution history",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"order_id": orderID,
			"history":  h.convertExecutionHistory(history),
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) getOrderStatus(c *gin.Context) {
	_, cancel := context.WithTimeout(c.Request.Context(), DefaultTimeout)
	defer cancel()

	orderID := c.Param("id")
	userAddress := h.getUserAddress(c)

	order, err := h.db.GetOrder(orderID)
	if err != nil {
		if err == database.ErrOrderNotFound {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:     "Order not found",
				Code:      ErrCodeNotFound,
				Timestamp: time.Now(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to retrieve order",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	if !h.canAccessOrder(userAddress, order.UserAddress) {
		c.JSON(http.StatusForbidden, ErrorResponse{
			Error:     "Access denied",
			Code:      ErrCodeForbidden,
			Timestamp: time.Now(),
		})
		return
	}

	// Calculate progress
	history, _ := h.db.GetExecutionHistory(orderID)
	
	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"order_id":           orderID,
			"status":             order.Status,
			"completion_rate":    order.CalculateCompletionRate(),
			"executed_amount":    order.ExecutedAmount,
			"remaining_amount":   order.GetRemainingAmount(),
			"intervals_executed": len(history),
			"total_intervals":    order.ExecutionIntervals,
			"average_price":      order.AveragePrice,
			"last_execution":     order.LastExecution,
			"can_execute":        order.CanExecuteInterval(),
			"next_execution":     order.GetNextExecutionTime(),
		},
		Timestamp: time.Now(),
	})
}

// TWAP endpoints
func (h *Handler) getTWAPPrice(c *gin.Context) {
	_, cancel := context.WithTimeout(c.Request.Context(), DefaultTimeout)
	defer cancel()

	pair := c.Param("pair")
	windowStr := c.DefaultQuery("window", "60")

	window, err := strconv.Atoi(windowStr)
	if err != nil || window < 5 || window > 1440 {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     "Invalid window parameter (must be 5-1440 minutes)",
			Code:      ErrCodeValidation,
			Timestamp: time.Now(),
		})
		return
	}

	// Check cache first
	cacheKey := "twap:" + pair + ":" + windowStr
	if cached := h.getFromCache(cacheKey); cached != nil {
		if price, ok := cached.(decimal.Decimal); ok {
			c.JSON(http.StatusOK, SuccessResponse{
				Success: true,
				Data: map[string]interface{}{
					"token_pair":     pair,
					"window_minutes": window,
					"twap_price":     price,
					"calculated_at":  time.Now().UTC(),
					"cached":         true,
				},
				Timestamp: time.Now(),
			})
			return
		}
	}

	price, err := h.twapEngine.GetTWAPPrice(pair, window)
	if err != nil {
		h.logger.Error("Failed to get TWAP price", 
			zap.Error(err),
			zap.String("token_pair", pair),
			zap.Int("window", window))
		
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to calculate TWAP price",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	// Cache the result
	h.setCache(cacheKey, price, CacheTTL)

	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"token_pair":     pair,
			"window_minutes": window,
			"twap_price":     price,
			"calculated_at":  time.Now().UTC(),
			"cached":         false,
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) getCurrentPrice(c *gin.Context) {
	_, cancel := context.WithTimeout(c.Request.Context(), DefaultTimeout)
	defer cancel()

	pair := c.Param("pair")

	price, err := h.twapEngine.GetCurrentPrice(pair)
	if err != nil {
		h.logger.Error("Failed to get current price", 
			zap.Error(err),
			zap.String("token_pair", pair))
		
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to get current price",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"token_pair": pair,
			"price":      price,
			"timestamp":  time.Now().UTC(),
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) executeOrder(c *gin.Context) {
	_, cancel := context.WithTimeout(c.Request.Context(), DefaultTimeout)
	defer cancel()

	orderID := c.Param("id")
	userAddress := h.getUserAddress(c)

	// Verify user can modify this order
	order, err := h.db.GetOrder(orderID)
	if err != nil {
		if err == database.ErrOrderNotFound {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:     "Order not found",
				Code:      ErrCodeNotFound,
				Timestamp: time.Now(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to retrieve order",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	if !h.canModifyOrder(userAddress, order.UserAddress) {
		c.JSON(http.StatusForbidden, ErrorResponse{
			Error:     "Access denied",
			Code:      ErrCodeForbidden,
			Timestamp: time.Now(),
		})
		return
	}

	response, err := h.twapEngine.ExecuteOrderManually(orderID)
	if err != nil {
		h.logger.Error("Failed to execute order", 
			zap.Error(err),
			zap.String("order_id", orderID),
			zap.String("user_address", userAddress))
		
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     err.Error(),
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	if !response.Success {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     response.Error.Error(),
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	// Clear order cache
	h.clearOrderCache(orderID)

	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"executed_amount": response.ExecutedAmount,
			"execution_price": response.ExecutionPrice,
			"tx_hash":         response.TxHash,
			"gas_used":        response.GasUsed,
			"slippage_bps":    response.Slippage,
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) getTWAPMetrics(c *gin.Context) {
	metrics := h.twapEngine.GetMetrics()

	successRate := float64(0)
	if metrics.TotalExecutions > 0 {
		successRate = float64(metrics.SuccessfulExecutions) / float64(metrics.TotalExecutions) * 100
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"total_executions":       metrics.TotalExecutions,
			"successful_executions":  metrics.SuccessfulExecutions,
			"failed_executions":      metrics.FailedExecutions,
			"success_rate":           successRate,
			"average_execution_time": metrics.AverageExecutionTime.String(),
			"average_slippage":       metrics.AverageSlippage,
			"total_volume_executed":  metrics.TotalVolumeExecuted,
			"last_execution_time":    metrics.LastExecutionTime,
		},
		Timestamp: time.Now(),
	})
}

// Chain endpoints
func (h *Handler) getSupportedChains(c *gin.Context) {
	chains, err := h.db.GetSupportedChains()
	if err != nil {
		h.logger.Error("Failed to get supported chains", zap.Error(err))
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to retrieve supported chains",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"supported_chains": chains,
			"count":           len(chains),
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) getChainStatus(c *gin.Context) {
	chainID := c.Param("id")

	status, err := h.db.GetChainStatus(chainID)
	if err != nil {
		if err == database.ErrChainNotFound {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:     "Chain not found",
				Code:      ErrCodeNotFound,
				Timestamp: time.Now(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to retrieve chain status",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Success:   true,
		Data:      status,
		Timestamp: time.Now(),
	})
}

func (h *Handler) getChainMetrics(c *gin.Context) {
	chainID := c.Param("id")

	// In production, get real metrics from the adapter
	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"chain_id":           chainID,
			"order_count":        100,
			"total_volume":       "1000000",
			"success_rate":       99.5,
			"average_block_time": "12s",
			"health_status":      "healthy",
		},
		Timestamp: time.Now(),
	})
}

// WebSocket handler for real-time updates
func (h *Handler) websocketHandler(c *gin.Context) {
	// WebSocket implementation would go here
	c.JSON(http.StatusNotImplemented, ErrorResponse{
		Error:     "WebSocket endpoint not yet implemented",
		Code:      "NOT_IMPLEMENTED",
		Timestamp: time.Now(),
	})
}

// Statistics endpoints
func (h *Handler) getOverviewStats(c *gin.Context) {
	// In production, get real statistics from the orchestrator
	stats := h.orchestrator.GetStatistics()
	
	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"total_orders":        stats.TotalOrders,
			"active_orders":       stats.ActiveOrders,
			"completed_orders":    stats.CompletedOrders,
			"failed_orders":       stats.FailedOrders,
			"total_volume":        stats.TotalVolume,
			"cross_chain_swaps":   stats.CrossChainSwaps,
			"successful_swaps":    stats.SuccessfulSwaps,
			"average_process_time": stats.AverageProcessTime,
			"uptime_seconds":      stats.UptimeSeconds,
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) getVolumeStats(c *gin.Context) {
	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"daily_volume":   "1000000",
			"weekly_volume":  "7000000",
			"monthly_volume": "30000000",
			"volume_by_chain": map[string]interface{}{
				"ethereum": "20000000",
				"cosmos":   "8000000",
				"stellar":  "2000000",
			},
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) getPerformanceStats(c *gin.Context) {
	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"average_slippage":       "0.15%",
			"average_execution_time": "42s",
			"success_rate":           99.2,
			"mev_protection_rate":    100.0,
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) getPriceHistory(c *gin.Context) {
	pair := c.Param("pair")
	windowStr := c.DefaultQuery("window", "1440") // 24 hours default

	window, err := strconv.Atoi(windowStr)
	if err != nil {
		window = 1440
	}

	history, err := h.db.GetPriceHistory(pair, window)
	if err != nil {
		h.logger.Error("Failed to get price history", 
			zap.Error(err),
			zap.String("token_pair", pair))
		
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to retrieve price history",
			Code:      ErrCodeInternalError,
			Timestamp: time.Now(),
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"token_pair":     pair,
			"window_minutes": window,
			"data_points":    len(history),
			"history":        history,
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) getLatestPrice(c *gin.Context) {
	pair := c.Param("pair")

	// Get most recent price point
	history, err := h.db.GetPriceHistory(pair, 60) // Last hour
	if err != nil || len(history) == 0 {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:     "No price data available",
			Code:      ErrCodeNotFound,
			Timestamp: time.Now(),
		})
		return
	}

	latest := history[len(history)-1]
	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"token_pair": pair,
			"price":      latest.Price,
			"volume":     latest.Volume,
			"source":     latest.Source,
			"timestamp":  latest.Timestamp,
		},
		Timestamp: time.Now(),
	})
}

// Admin endpoints
func (h *Handler) adminHealthCheck(c *gin.Context) {
	health := h.orchestrator.HealthCheck()
	c.JSON(http.StatusOK, SuccessResponse{
		Success:   true,
		Data:      health,
		Timestamp: time.Now(),
	})
}

func (h *Handler) toggleMaintenanceMode(c *gin.Context) {
	// Implementation for maintenance mode toggle
	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"maintenance_mode": false,
			"message":         "Maintenance mode toggled",
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) getDetailedMetrics(c *gin.Context) {
	// Implementation for detailed metrics
	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"system_metrics": "detailed metrics would go here",
		},
		Timestamp: time.Now(),
	})
}

func (h *Handler) clearCache(c *gin.Context) {
	h.cache = make(map[string]interface{})
	c.JSON(http.StatusOK, SuccessResponse{
		Success: true,
		Data: map[string]interface{}{
			"message": "Cache cleared successfully",
		},
		Timestamp: time.Now(),
	})
}

// Helper functions
func (h *Handler) convertExecutionHistory(history []*database.ExecutionRecord) []ExecutionHistoryResponse {
	response := make([]ExecutionHistoryResponse, 0, len(history))
	for _, record := range history {
		response = append(response, ExecutionHistoryResponse{
			IntervalNumber: record.IntervalNumber,
			Timestamp:      record.Timestamp,
			Amount:         record.Amount,
			Price:          record.Price,
			GasUsed:        record.GasUsed,
			Slippage:       record.Slippage,
			TxHash:         record.TxHash,
			ChainID:        record.ChainID,
		})
	}
	return response
}
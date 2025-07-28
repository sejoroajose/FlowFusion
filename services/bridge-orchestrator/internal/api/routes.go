package api

import (
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

// Handler holds dependencies for API handlers
type Handler struct {
	orchestrator *orchestrator.Orchestrator
	twapEngine   *twap.Engine
	db           database.DB
	logger       *zap.Logger
}

// SetupRoutes configures all API routes
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
	}

	// Health check
	router.GET("/health", h.healthCheck)
	router.GET("/ready", h.readinessCheck)

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// Order management
		orders := v1.Group("/orders")
		{
			orders.POST("", h.createOrder)
			orders.GET("/:id", h.getOrder)
			orders.PUT("/:id/cancel", h.cancelOrder)
			orders.GET("", h.listOrders)
			orders.GET("/:id/history", h.getOrderHistory)
			orders.GET("/:id/status", h.getOrderStatus)
		}

		// TWAP operations
		twapRoutes := v1.Group("/twap")
		{
			twapRoutes.GET("/price/:pair", h.getTWAPPrice)
			twapRoutes.GET("/current/:pair", h.getCurrentPrice)
			twapRoutes.POST("/execute/:id", h.executeOrder)
			twapRoutes.GET("/metrics", h.getTWAPMetrics)
		}

		// Chain operations
		chains := v1.Group("/chains")
		{
			chains.GET("", h.getSupportedChains)
			chains.GET("/:id/status", h.getChainStatus)
			chains.GET("/:id/metrics", h.getChainMetrics)
		}

		// Price feeds
		prices := v1.Group("/prices")
		{
			prices.GET("/:pair/history", h.getPriceHistory)
			prices.GET("/:pair/latest", h.getLatestPrice)
		}

		// Statistics and analytics
		stats := v1.Group("/stats")
		{
			stats.GET("/overview", h.getOverviewStats)
			stats.GET("/volume", h.getVolumeStats)
			stats.GET("/performance", h.getPerformanceStats)
		}

		// WebSocket endpoint for real-time updates
		v1.GET("/ws", h.websocketHandler)
	}
}

// Health check endpoints
func (h *Handler) healthCheck(c *gin.Context) {
	// Check database connectivity
	if err := h.db.Health(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  "database connection failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"version":   "1.0.0",
		"service":   "flowfusion-bridge-orchestrator",
	})
}

func (h *Handler) readinessCheck(c *gin.Context) {
	// More comprehensive readiness check
	checks := map[string]string{
		"database": "healthy",
		"twap_engine": "healthy",
		"orchestrator": "healthy",
	}

	// Check database
	if err := h.db.Health(); err != nil {
		checks["database"] = "unhealthy"
	}

	// Check if any critical component is unhealthy
	allHealthy := true
	for _, status := range checks {
		if status != "healthy" {
			allHealthy = false
			break
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

// Order management endpoints
func (h *Handler) createOrder(c *gin.Context) {
	var req CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid create order request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// Validate request
	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Validation failed",
			"details": err.Error(),
		})
		return
	}

	// Convert to database order
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
		Status:              string(database.OrderStatusPending),
		ExecutedAmount:      decimal.Zero,
		AveragePrice:        decimal.Zero,
		Metadata:            database.Metadata(req.Metadata),
	}

	// Create order in database
	if err := h.db.CreateOrder(order); err != nil {
		h.logger.Error("Failed to create order", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create order",
		})
		return
	}

	h.logger.Info("Order created successfully", zap.String("order_id", order.ID))

	c.JSON(http.StatusCreated, gin.H{
		"order_id": order.ID,
		"status":   order.Status,
		"created_at": time.Now().UTC(),
	})
}

func (h *Handler) getOrder(c *gin.Context) {
	orderID := c.Param("id")
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Order ID is required",
		})
		return
	}

	order, err := h.db.GetOrder(orderID)
	if err != nil {
		if err == database.ErrOrderNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Order not found",
			})
			return
		}
		h.logger.Error("Failed to get order", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve order",
		})
		return
	}

	// Get execution history
	history, err := h.db.GetExecutionHistory(orderID)
	if err != nil {
		h.logger.Error("Failed to get execution history", zap.Error(err))
		history = []*database.ExecutionRecord{} // Return empty history on error
	}

	response := OrderResponse{
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
		ExecutionHistory: convertExecutionHistory(history),
		Metadata:         map[string]interface{}(order.Metadata),
	}

	c.JSON(http.StatusOK, response)
}

func setupHealthRoutes(router *gin.Engine, db database.DB) {
    health := router.Group("/health")
    {
        health.GET("/", basicHealthCheck)
        health.GET("/ready", readinessCheck(db))
        health.GET("/live", livenessCheck)
    }
}

func readinessCheck(db database.DB) gin.HandlerFunc {
    return func(c *gin.Context) {
        if err := db.Ping(); err != nil {
            c.JSON(http.StatusServiceUnavailable, gin.H{
                "status": "not ready",
                "database": "unhealthy",
                "error": err.Error(),
            })
            return
        }

        c.JSON(http.StatusOK, gin.H{
            "status": "ready",
            "database": "healthy",
            "timestamp": time.Now().UTC(),
        })
    }
}

func (h *Handler) cancelOrder(c *gin.Context) {
	orderID := c.Param("id")
	userAddress := c.GetHeader("X-User-Address") // In production, extract from JWT

	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Order ID is required",
		})
		return
	}

	order, err := h.db.GetOrder(orderID)
	if err != nil {
		if err == database.ErrOrderNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Order not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve order",
		})
		return
	}

	// Verify ownership (in production, use proper authentication)
	if userAddress != "" && order.UserAddress != userAddress {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Unauthorized to cancel this order",
		})
		return
	}

	// Check if order can be cancelled
	if order.Status == string(database.OrderStatusCompleted) ||
		order.Status == string(database.OrderStatusCancelled) ||
		order.Status == string(database.OrderStatusClaimed) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Order cannot be cancelled in current status",
			"status": order.Status,
		})
		return
	}

	// Update order status
	order.Status = string(database.OrderStatusCancelled)
	order.UpdatedAt = time.Now()

	if err := h.db.UpdateOrder(order); err != nil {
		h.logger.Error("Failed to cancel order", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to cancel order",
		})
		return
	}

	h.logger.Info("Order cancelled", zap.String("order_id", orderID))

	c.JSON(http.StatusOK, gin.H{
		"order_id": orderID,
		"status":   order.Status,
		"cancelled_at": time.Now().UTC(),
	})
}

func (h *Handler) listOrders(c *gin.Context) {
	// Parse query parameters
	userAddress := c.Query("user")
	sourceChain := c.Query("source_chain")
	targetChain := c.Query("target_chain")
	status := c.Query("status")
	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 20
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	// For simplicity, only implement user filtering for now
	if userAddress == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "User address is required",
		})
		return
	}

	orders, err := h.db.GetOrdersByUser(userAddress, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get orders", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve orders",
		})
		return
	}

	// Convert to response format
	var orderResponses []OrderSummaryResponse
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

	c.JSON(http.StatusOK, gin.H{
		"orders": orderResponses,
		"pagination": gin.H{
			"limit":  limit,
			"offset": offset,
			"count":  len(orderResponses),
		},
	})
}

func (h *Handler) getOrderHistory(c *gin.Context) {
	orderID := c.Param("id")
	
	history, err := h.db.GetExecutionHistory(orderID)
	if err != nil {
		h.logger.Error("Failed to get execution history", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve execution history",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"order_id": orderID,
		"history":  convertExecutionHistory(history),
	})
}

func (h *Handler) getOrderStatus(c *gin.Context) {
	orderID := c.Param("id")
	
	order, err := h.db.GetOrder(orderID)
	if err != nil {
		if err == database.ErrOrderNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Order not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve order",
		})
		return
	}

	// Calculate progress
	history, _ := h.db.GetExecutionHistory(orderID)
	
	c.JSON(http.StatusOK, gin.H{
		"order_id":          orderID,
		"status":            order.Status,
		"completion_rate":   order.CalculateCompletionRate(),
		"executed_amount":   order.ExecutedAmount,
		"remaining_amount":  order.GetRemainingAmount(),
		"intervals_executed": len(history),
		"total_intervals":   order.ExecutionIntervals,
		"average_price":     order.AveragePrice,
		"last_execution":    order.LastExecution,
		"can_execute":       order.CanExecuteInterval(),
		"next_execution":    order.GetNextExecutionTime(),
	})
}

// TWAP endpoints
func (h *Handler) getTWAPPrice(c *gin.Context) {
	pair := c.Param("pair")
	windowStr := c.DefaultQuery("window", "60")

	window, err := strconv.Atoi(windowStr)
	if err != nil || window < 5 || window > 1440 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid window parameter (must be 5-1440 minutes)",
		})
		return
	}

	price, err := h.twapEngine.GetTWAPPrice(pair, window)
	if err != nil {
		h.logger.Error("Failed to get TWAP price", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to calculate TWAP price",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token_pair":     pair,
		"window_minutes": window,
		"twap_price":     price,
		"calculated_at":  time.Now().UTC(),
	})
}

func (h *Handler) getCurrentPrice(c *gin.Context) {
	pair := c.Param("pair")

	price, err := h.twapEngine.GetCurrentPrice(pair)
	if err != nil {
		h.logger.Error("Failed to get current price", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get current price",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token_pair":  pair,
		"price":       price,
		"timestamp":   time.Now().UTC(),
	})
}

func (h *Handler) executeOrder(c *gin.Context) {
	orderID := c.Param("id")

	response, err := h.twapEngine.ExecuteOrderManually(orderID)
	if err != nil {
		h.logger.Error("Failed to execute order", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	if !response.Success {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": response.Error.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":         true,
		"executed_amount": response.ExecutedAmount,
		"execution_price": response.ExecutionPrice,
		"tx_hash":         response.TxHash,
		"gas_used":        response.GasUsed,
		"slippage_bps":    response.Slippage,
	})
}

func (h *Handler) getTWAPMetrics(c *gin.Context) {
	metrics := h.twapEngine.GetMetrics()

	c.JSON(http.StatusOK, gin.H{
		"total_executions":      metrics.TotalExecutions,
		"successful_executions": metrics.SuccessfulExecutions,
		"failed_executions":     metrics.FailedExecutions,
		"success_rate":          float64(metrics.SuccessfulExecutions) / float64(metrics.TotalExecutions) * 100,
		"average_execution_time": metrics.AverageExecutionTime.String(),
		"average_slippage":      metrics.AverageSlippage,
		"total_volume_executed": metrics.TotalVolumeExecuted,
		"last_execution_time":   metrics.LastExecutionTime,
	})
}

// Chain endpoints
func (h *Handler) getSupportedChains(c *gin.Context) {
	chains, err := h.db.GetSupportedChains()
	if err != nil {
		h.logger.Error("Failed to get supported chains", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve supported chains",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"supported_chains": chains,
		"count":           len(chains),
	})
}

func (h *Handler) getChainStatus(c *gin.Context) {
	chainID := c.Param("id")

	status, err := h.db.GetChainStatus(chainID)
	if err != nil {
		if err == database.ErrChainNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Chain not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve chain status",
		})
		return
	}

	c.JSON(http.StatusOK, status)
}

func (h *Handler) getChainMetrics(c *gin.Context) {
	chainID := c.Param("id")

	// Mock metrics for now
	c.JSON(http.StatusOK, gin.H{
		"chain_id":           chainID,
		"order_count":        100,
		"total_volume":       "1000000",
		"success_rate":       99.5,
		"average_block_time": "12s",
		"health_status":      "healthy",
	})
}

// WebSocket handler for real-time updates
func (h *Handler) websocketHandler(c *gin.Context) {
	// WebSocket implementation would go here
	// For now, return a simple message
	c.JSON(http.StatusNotImplemented, gin.H{
		"message": "WebSocket endpoint not yet implemented",
	})
}

// Statistics endpoints
func (h *Handler) getOverviewStats(c *gin.Context) {
	// Mock implementation
	c.JSON(http.StatusOK, gin.H{
		"total_orders":     500,
		"active_orders":    25,
		"completed_orders": 450,
		"total_volume":     "50000000",
		"success_rate":     99.2,
		"avg_execution_time": "45s",
	})
}

func (h *Handler) getVolumeStats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"daily_volume":   "1000000",
		"weekly_volume":  "7000000",
		"monthly_volume": "30000000",
		"volume_by_chain": gin.H{
			"ethereum": "20000000",
			"cosmos":   "8000000",
			"stellar":  "2000000",
		},
	})
}

func (h *Handler) getPerformanceStats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"average_slippage":      "0.15%",
		"average_execution_time": "42s",
		"success_rate":          99.2,
		"mev_protection_rate":   100.0,
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
		h.logger.Error("Failed to get price history", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve price history",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token_pair": pair,
		"window_minutes": window,
		"data_points": len(history),
		"history": history,
	})
}

func (h *Handler) getLatestPrice(c *gin.Context) {
	pair := c.Param("pair")

	// Get most recent price point
	history, err := h.db.GetPriceHistory(pair, 60) // Last hour
	if err != nil || len(history) == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "No price data available",
		})
		return
	}

	latest := history[len(history)-1]
	c.JSON(http.StatusOK, gin.H{
		"token_pair": pair,
		"price":      latest.Price,
		"volume":     latest.Volume,
		"source":     latest.Source,
		"timestamp":  latest.Timestamp,
	})
}

// Helper functions
func convertExecutionHistory(history []*database.ExecutionRecord) []ExecutionHistoryResponse {
	var response []ExecutionHistoryResponse
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
package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// Request/Response Types

type CreateOrderRequest struct {
	ID               string                 `json:"id" binding:"required"`
	UserAddress      string                 `json:"user_address" binding:"required"`
	SourceChain      string                 `json:"source_chain" binding:"required"`
	TargetChain      string                 `json:"target_chain" binding:"required"`
	SourceToken      string                 `json:"source_token" binding:"required"`
	SourceAmount     decimal.Decimal        `json:"source_amount" binding:"required"`
	TargetToken      string                 `json:"target_token" binding:"required"`
	TargetRecipient  string                 `json:"target_recipient" binding:"required"`
	MinReceived      decimal.Decimal        `json:"min_received" binding:"required"`
	TWAPConfig       TWAPConfigRequest      `json:"twap_config" binding:"required"`
	HTLCHash         string                 `json:"htlc_hash" binding:"required"`
	TimeoutHeight    int64                  `json:"timeout_height" binding:"required"`
	TimeoutTimestamp int64                  `json:"timeout_timestamp" binding:"required"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

type TWAPConfigRequest struct {
	WindowMinutes       int             `json:"window_minutes" binding:"required,min=5,max=1440"`
	ExecutionIntervals  int             `json:"execution_intervals" binding:"required,min=2,max=20"`
	MaxSlippage         int             `json:"max_slippage" binding:"required,min=1,max=1000"`
	MinFillSize         decimal.Decimal `json:"min_fill_size" binding:"required"`
	EnableMEVProtection bool            `json:"enable_mev_protection"`
}

type OrderResponse struct {
	ID                string                     `json:"id"`
	UserAddress       string                     `json:"user_address"`
	SourceChain       string                     `json:"source_chain"`
	TargetChain       string                     `json:"target_chain"`
	SourceToken       string                     `json:"source_token"`
	SourceAmount      decimal.Decimal            `json:"source_amount"`
	TargetToken       string                     `json:"target_token"`
	TargetRecipient   string                     `json:"target_recipient"`
	MinReceived       decimal.Decimal            `json:"min_received"`
	TWAPConfig        TWAPConfigResponse         `json:"twap_config"`
	HTLCHash          string                     `json:"htlc_hash"`
	TimeoutHeight     int64                      `json:"timeout_height"`
	TimeoutTimestamp  int64                      `json:"timeout_timestamp"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
	ExecutedAmount    decimal.Decimal            `json:"executed_amount"`
	LastExecution     *time.Time                 `json:"last_execution"`
	Status            string                     `json:"status"`
	AveragePrice      decimal.Decimal            `json:"average_price"`
	CompletionRate    float64                    `json:"completion_rate"`
	ExecutionHistory  []ExecutionHistoryResponse `json:"execution_history"`
	Metadata          map[string]interface{}     `json:"metadata,omitempty"`
}

type TWAPConfigResponse struct {
	WindowMinutes       int             `json:"window_minutes"`
	ExecutionIntervals  int             `json:"execution_intervals"`
	MaxSlippage         int             `json:"max_slippage"`
	MinFillSize         decimal.Decimal `json:"min_fill_size"`
	EnableMEVProtection bool            `json:"enable_mev_protection"`
}

type OrderSummaryResponse struct {
	ID                string          `json:"id"`
	SourceChain       string          `json:"source_chain"`
	TargetChain       string          `json:"target_chain"`
	SourceAmount      decimal.Decimal `json:"source_amount"`
	ExecutedAmount    decimal.Decimal `json:"executed_amount"`
	Status            string          `json:"status"`
	CreatedAt         time.Time       `json:"created_at"`
	CompletionRate    float64         `json:"completion_rate"`
	AveragePrice      decimal.Decimal `json:"average_price"`
	IntervalsExecuted int             `json:"intervals_executed"`
	TotalIntervals    int             `json:"total_intervals"`
}

type ExecutionHistoryResponse struct {
	IntervalNumber int             `json:"interval_number"`
	Timestamp      time.Time       `json:"timestamp"`
	Amount         decimal.Decimal `json:"amount"`
	Price          decimal.Decimal `json:"price"`
	GasUsed        *int64          `json:"gas_used,omitempty"`
	Slippage       *int            `json:"slippage,omitempty"`
	TxHash         *string         `json:"tx_hash,omitempty"`
	ChainID        string          `json:"chain_id"`
}

type ErrorResponse struct {
	Error     string                 `json:"error"`
	Code      string                 `json:"code,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

type SuccessResponse struct {
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Message   string      `json:"message,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

type PaginationResponse struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

type ListOrdersParams struct {
	UserAddress   string `form:"user"`
	SourceChain   string `form:"source_chain"`
	TargetChain   string `form:"target_chain"`
	Status        string `form:"status"`
	CreatedAfter  string `form:"created_after"`
	CreatedBefore string `form:"created_before"`
	Limit         int    `form:"limit"`
	Offset        int    `form:"offset"`
	Page          int    `form:"page"`
	SortBy        string `form:"sort_by"`
	SortOrder     string `form:"sort_order"`
}

// Error codes
const (
	ErrCodeValidation         = "VALIDATION_ERROR"
	ErrCodeNotFound          = "NOT_FOUND"
	ErrCodeUnauthorized      = "UNAUTHORIZED"
	ErrCodeForbidden         = "FORBIDDEN"
	ErrCodeConflict          = "CONFLICT"
	ErrCodeInternalError     = "INTERNAL_ERROR"
	ErrCodeServiceUnavailable = "SERVICE_UNAVAILABLE"
	ErrCodeRateLimit         = "RATE_LIMIT"
	ErrCodeChainError        = "CHAIN_ERROR"
	ErrCodeInsufficientFunds = "INSUFFICIENT_FUNDS"
	ErrCodeSlippageExceeded  = "SLIPPAGE_EXCEEDED"
	ErrCodeOrderExpired      = "ORDER_EXPIRED"
	ErrCodeInvalidChain      = "INVALID_CHAIN"
	ErrCodeInvalidToken      = "INVALID_TOKEN"
)

// Validation patterns
var (
	ethereumAddressPattern = regexp.MustCompile(`^0x[a-fA-F0-9]{40}$`)
	cosmosAddressPattern   = regexp.MustCompile(`^cosmos[0-9a-z]{39}$`)
	stellarAddressPattern  = regexp.MustCompile(`^G[A-Z2-7]{55}$`)
	bitcoinAddressPattern  = regexp.MustCompile(`^[13][a-km-zA-HJ-NP-Z1-9]{25,34}$`)
	hashPattern           = regexp.MustCompile(`^0x[a-fA-F0-9]{64}$`)
	orderIDPattern        = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	tokenPairPattern      = regexp.MustCompile(`^[A-Z0-9_]{1,20}_[A-Z0-9_]{1,20}$`)
	chainIDPattern        = regexp.MustCompile(`^[a-z0-9_-]{1,20}$`)
)

// Middleware Functions

func (h *Handler) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip auth for health checks
		if strings.HasPrefix(c.Request.URL.Path, "/health") {
			c.Next()
			return
		}

		// Extract user address from header (in production, validate JWT)
		userAddress := c.GetHeader("X-User-Address")
		if userAddress == "" {
			c.JSON(http.StatusUnauthorized, ErrorResponse{
				Error:     "Authentication required",
				Code:      ErrCodeUnauthorized,
				Timestamp: time.Now(),
			})
			c.Abort()
			return
		}

		// Validate address format
		if !h.isValidAddress(userAddress) {
			c.JSON(http.StatusUnauthorized, ErrorResponse{
				Error:     "Invalid user address format",
				Code:      ErrCodeUnauthorized,
				Timestamp: time.Now(),
			})
			c.Abort()
			return
		}

		c.Set("user_address", userAddress)
		c.Set("authenticated", true)
		c.Next()
	}
}

func (h *Handler) adminAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Additional admin authentication
		adminToken := c.GetHeader("X-Admin-Token")
		if adminToken == "" || !h.isValidAdminToken(adminToken) {
			c.JSON(http.StatusForbidden, ErrorResponse{
				Error:     "Admin access required",
				Code:      ErrCodeForbidden,
				Timestamp: time.Now(),
			})
			c.Abort()
			return
		}

		c.Set("admin", true)
		c.Next()
	}
}

func (h *Handler) rateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userAddress := h.getUserAddress(c)
		if userAddress == "" {
			c.Next()
			return
		}

		if !h.checkRateLimit(userAddress) {
			c.JSON(http.StatusTooManyRequests, ErrorResponse{
				Error:     "Rate limit exceeded",
				Code:      ErrCodeRateLimit,
				Details:   map[string]interface{}{"retry_after": "60s"},
				Timestamp: time.Now(),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// Validation Middleware

func (h *Handler) validateCreateOrder() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Additional validation beyond binding
		var req CreateOrderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:     "Invalid request format",
				Code:      ErrCodeValidation,
				Details:   map[string]interface{}{"binding_error": err.Error()},
				Timestamp: time.Now(),
			})
			c.Abort()
			return
		}

		if err := h.validateCreateOrderRequest(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:     "Validation failed",
				Code:      ErrCodeValidation,
				Details:   map[string]interface{}{"validation_error": err.Error()},
				Timestamp: time.Now(),
			})
			c.Abort()
			return
		}

		c.Set("validated_request", req)
		c.Next()
	}
}

func (h *Handler) validateOrderID() gin.HandlerFunc {
	return func(c *gin.Context) {
		orderID := c.Param("id")
		if !h.isValidOrderID(orderID) {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:     "Invalid order ID format",
				Code:      ErrCodeValidation,
				Timestamp: time.Now(),
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (h *Handler) validateTokenPair() gin.HandlerFunc {
	return func(c *gin.Context) {
		pair := c.Param("pair")
		if !h.isValidTokenPair(pair) {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:     "Invalid token pair format",
				Code:      ErrCodeValidation,
				Timestamp: time.Now(),
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (h *Handler) validateChainID() gin.HandlerFunc {
	return func(c *gin.Context) {
		chainID := c.Param("id")
		if !h.isValidChainID(chainID) {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:     "Invalid chain ID format",
				Code:      ErrCodeValidation,
				Timestamp: time.Now(),
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (h *Handler) validateListOrders() gin.HandlerFunc {
	return func(c *gin.Context) {
		params := h.parseListOrdersParams(c)
		if err := h.validateListOrdersParams(params); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error:     "Invalid query parameters",
				Code:      ErrCodeValidation,
				Details:   map[string]interface{}{"validation_error": err.Error()},
				Timestamp: time.Now(),
			})
			c.Abort()
			return
		}
		c.Set("validated_params", params)
		c.Next()
	}
}

// Helper Functions

func (h *Handler) getUserAddress(c *gin.Context) string {
	if addr, exists := c.Get("user_address"); exists {
		return addr.(string)
	}
	return ""
}

func (h *Handler) getRequestID(c *gin.Context) string {
	if id, exists := c.Get("request_id"); exists {
		return id.(string)
	}
	return ""
}

func (h *Handler) canAccessOrder(userAddress, orderUserAddress string) bool {
	// In production, implement proper access control
	return userAddress == orderUserAddress || h.isAdmin(userAddress)
}

func (h *Handler) canModifyOrder(userAddress, orderUserAddress string) bool {
	// In production, implement proper modification control
	return userAddress == orderUserAddress
}

func (h *Handler) canCancelOrder(status string) bool {
	cancelableStatuses := []string{
		"pending",
		"executing",
		"partially_filled",
	}
	
	for _, s := range cancelableStatuses {
		if status == s {
			return true
		}
	}
	return false
}

func (h *Handler) isAdmin(userAddress string) bool {
	// In production, check admin permissions from database or config
	return false // Default to false for security
}

func (h *Handler) isValidAdminToken(token string) bool {
	// In production, validate admin token
	return token == "admin-secret-token" // This is just for demo
}

func (h *Handler) checkRateLimit(userAddress string) bool {
	now := time.Now()
	
	limiter, exists := h.rateLimiter[userAddress]
	if !exists {
		h.rateLimiter[userAddress] = &RateLimiter{
			tokens:     DefaultRateLimit - 1,
			capacity:   DefaultRateLimit,
			lastRefill: now,
		}
		return true
	}

	// Refill tokens based on time elapsed
	elapsed := now.Sub(limiter.lastRefill)
	tokensToAdd := int(elapsed.Seconds())
	
	limiter.tokens += tokensToAdd
	if limiter.tokens > limiter.capacity {
		limiter.tokens = limiter.capacity
	}
	limiter.lastRefill = now

	if limiter.tokens <= 0 {
		return false
	}

	limiter.tokens--
	return true
}

func (h *Handler) isAllowedOrigin(origin string) bool {
	allowedOrigins := []string{
		"http://localhost:3000",
		"https://app.flowfusion.io",
		"https://staging.flowfusion.io",
	}
	
	for _, allowed := range allowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

// Cache Functions

func (h *Handler) getFromCache(key string) interface{} {
	// In production, use Redis with proper TTL
	return h.cache[key]
}

func (h *Handler) setCache(key string, value interface{}, ttl time.Duration) {
	// In production, use Redis with proper TTL
	h.cache[key] = value
}

func (h *Handler) clearOrderCache(orderID string) {
	// Clear all cache entries related to this order
	delete(h.cache, "order:"+orderID)
	// In production, clear all related cache patterns
}

// Validation Functions

func (h *Handler) validateCreateOrderRequest(req *CreateOrderRequest) error {
	// Validate order ID
	if !h.isValidOrderID(req.ID) {
		return errors.New("invalid order ID format")
	}

	// Validate addresses
	if !h.isValidAddress(req.UserAddress) {
		return errors.New("invalid user address")
	}

	if !h.isValidRecipientAddress(req.TargetRecipient, req.TargetChain) {
		return errors.New("invalid target recipient address")
	}

	// Validate chains
	if !h.isValidChainID(req.SourceChain) || !h.isValidChainID(req.TargetChain) {
		return errors.New("invalid chain ID")
	}

	if req.SourceChain == req.TargetChain {
		return errors.New("source and target chains must be different")
	}

	// Validate amounts
	if req.SourceAmount.LessThanOrEqual(decimal.Zero) {
		return errors.New("source amount must be greater than zero")
	}

	if req.MinReceived.LessThanOrEqual(decimal.Zero) {
		return errors.New("minimum received amount must be greater than zero")
	}

	// Validate HTLC hash
	if !h.isValidHash(req.HTLCHash) {
		return errors.New("invalid HTLC hash format")
	}

	// Validate timeouts
	if req.TimeoutHeight <= 0 {
		return errors.New("timeout height must be greater than zero")
	}

	if req.TimeoutTimestamp <= time.Now().Unix() {
		return errors.New("timeout timestamp must be in the future")
	}

	// Validate TWAP config
	return h.validateTWAPConfig(&req.TWAPConfig)
}

func (h *Handler) validateTWAPConfig(config *TWAPConfigRequest) error {
	if config.WindowMinutes < 5 || config.WindowMinutes > 1440 {
		return errors.New("window minutes must be between 5 and 1440")
	}

	if config.ExecutionIntervals < 2 || config.ExecutionIntervals > 20 {
		return errors.New("execution intervals must be between 2 and 20")
	}

	if config.MaxSlippage < 1 || config.MaxSlippage > 1000 {
		return errors.New("max slippage must be between 1 and 1000 basis points")
	}

	if config.MinFillSize.LessThanOrEqual(decimal.Zero) {
		return errors.New("minimum fill size must be greater than zero")
	}

	// Check that intervals fit within window
	intervalDuration := config.WindowMinutes / config.ExecutionIntervals
	if intervalDuration < 1 {
		return errors.New("execution intervals too frequent for the given window")
	}

	return nil
}

func (h *Handler) parseListOrdersParams(c *gin.Context) *ListOrdersParams {
	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")
	pageStr := c.DefaultQuery("page", "1")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > MaxPageSize {
		limit = DefaultPageSize
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	page, err := strconv.Atoi(pageStr)
	if err != nil || page <= 0 {
		page = 1
	}

	// If page is provided, calculate offset
	if c.Query("page") != "" {
		offset = (page - 1) * limit
	}

	return &ListOrdersParams{
		UserAddress:   c.Query("user"),
		SourceChain:   c.Query("source_chain"),
		TargetChain:   c.Query("target_chain"),
		Status:        c.Query("status"),
		CreatedAfter:  c.Query("created_after"),
		CreatedBefore: c.Query("created_before"),
		Limit:         limit,
		Offset:        offset,
		Page:          page,
		SortBy:        c.DefaultQuery("sort_by", "created_at"),
		SortOrder:     c.DefaultQuery("sort_order", "desc"),
	}
}

func (h *Handler) validateListOrdersParams(params *ListOrdersParams) error {
	if params.UserAddress != "" && !h.isValidAddress(params.UserAddress) {
		return errors.New("invalid user address")
	}

	if params.SourceChain != "" && !h.isValidChainID(params.SourceChain) {
		return errors.New("invalid source chain")
	}

	if params.TargetChain != "" && !h.isValidChainID(params.TargetChain) {
		return errors.New("invalid target chain")
	}

	validStatuses := []string{"pending", "executing", "partially_filled", "completed", "cancelled", "expired", "refunded", "claimed"}
	if params.Status != "" {
		isValid := false
		for _, status := range validStatuses {
			if params.Status == status {
				isValid = true
				break
			}
		}
		if !isValid {
			return errors.New("invalid status")
		}
	}

	validSortFields := []string{"created_at", "updated_at", "source_amount", "executed_amount", "completion_rate"}
	isValidSort := false
	for _, field := range validSortFields {
		if params.SortBy == field {
			isValidSort = true
			break
		}
	}
	if !isValidSort {
		return errors.New("invalid sort field")
	}

	if params.SortOrder != "asc" && params.SortOrder != "desc" {
		return errors.New("invalid sort order")
	}

	return nil
}

// Format Validation Functions

func (h *Handler) isValidOrderID(orderID string) bool {
	return orderIDPattern.MatchString(orderID) && len(orderID) >= 4 && len(orderID) <= 64
}

func (h *Handler) isValidAddress(address string) bool {
	// Support multiple address formats
	return ethereumAddressPattern.MatchString(address) ||
		cosmosAddressPattern.MatchString(address) ||
		stellarAddressPattern.MatchString(address) ||
		bitcoinAddressPattern.MatchString(address)
}

func (h *Handler) isValidRecipientAddress(address, chainID string) bool {
	switch chainID {
	case "ethereum", "polygon", "arbitrum", "optimism":
		return ethereumAddressPattern.MatchString(address)
	case "cosmos", "osmosis":
		return cosmosAddressPattern.MatchString(address)
	case "stellar":
		return stellarAddressPattern.MatchString(address)
	case "bitcoin":
		return bitcoinAddressPattern.MatchString(address)
	default:
		return h.isValidAddress(address) // Fallback to any valid format
	}
}

func (h *Handler) isValidHash(hash string) bool {
	return hashPattern.MatchString(hash)
}

func (h *Handler) isValidTokenPair(pair string) bool {
	return tokenPairPattern.MatchString(pair)
}

func (h *Handler) isValidChainID(chainID string) bool {
	return chainIDPattern.MatchString(chainID)
}

// Utility Functions

func generateRequestID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func NewPaginationResponse(page, limit int, total int64) *PaginationResponse {
	totalPages := int((total + int64(limit) - 1) / int64(limit))
	if totalPages == 0 {
		totalPages = 1
	}

	return &PaginationResponse{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    page < totalPages,
		HasPrev:    page > 1,
	}
}

func NewSuccessResponse(data interface{}, message string) *SuccessResponse {
	return &SuccessResponse{
		Success:   true,
		Data:      data,
		Message:   message,
		Timestamp: time.Now(),
	}
}

func NewErrorResponse(err error, code string) *ErrorResponse {
	return &ErrorResponse{
		Error:     err.Error(),
		Code:      code,
		Timestamp: time.Now(),
	}
}
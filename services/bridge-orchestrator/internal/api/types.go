package api

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// Request types

// CreateOrderRequest represents a request to create a new TWAP order
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

// TWAPConfigRequest represents TWAP configuration in requests
type TWAPConfigRequest struct {
	WindowMinutes       int             `json:"window_minutes" binding:"required,min=5,max=1440"`
	ExecutionIntervals  int             `json:"execution_intervals" binding:"required,min=2,max=20"`
	MaxSlippage         int             `json:"max_slippage" binding:"required,min=1,max=1000"`
	MinFillSize         decimal.Decimal `json:"min_fill_size" binding:"required"`
	EnableMEVProtection bool            `json:"enable_mev_protection"`
}

// ExecuteOrderRequest represents a request to manually execute an order
type ExecuteOrderRequest struct {
	Force bool `json:"force,omitempty"` // Force execution even if conditions aren't met
}

// Response types

// OrderResponse represents a complete order in API responses
type OrderResponse struct {
	ID                string                    `json:"id"`
	UserAddress       string                    `json:"user_address"`
	SourceChain       string                    `json:"source_chain"`
	TargetChain       string                    `json:"target_chain"`
	SourceToken       string                    `json:"source_token"`
	SourceAmount      decimal.Decimal           `json:"source_amount"`
	TargetToken       string                    `json:"target_token"`
	TargetRecipient   string                    `json:"target_recipient"`
	MinReceived       decimal.Decimal           `json:"min_received"`
	TWAPConfig        TWAPConfigResponse        `json:"twap_config"`
	HTLCHash          string                    `json:"htlc_hash"`
	TimeoutHeight     int64                     `json:"timeout_height"`
	TimeoutTimestamp  int64                     `json:"timeout_timestamp"`
	CreatedAt         time.Time                 `json:"created_at"`
	UpdatedAt         time.Time                 `json:"updated_at"`
	ExecutedAmount    decimal.Decimal           `json:"executed_amount"`
	LastExecution     *time.Time                `json:"last_execution"`
	Status            string                    `json:"status"`
	AveragePrice      decimal.Decimal           `json:"average_price"`
	CompletionRate    float64                   `json:"completion_rate"`
	ExecutionHistory  []ExecutionHistoryResponse `json:"execution_history"`
	Metadata          map[string]interface{}    `json:"metadata,omitempty"`
}

// OrderSummaryResponse represents a summarized order for list endpoints
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

// TWAPConfigResponse represents TWAP configuration in responses
type TWAPConfigResponse struct {
	WindowMinutes       int             `json:"window_minutes"`
	ExecutionIntervals  int             `json:"execution_intervals"`
	MaxSlippage         int             `json:"max_slippage"`
	MinFillSize         decimal.Decimal `json:"min_fill_size"`
	EnableMEVProtection bool            `json:"enable_mev_protection"`
}

// ExecutionHistoryResponse represents execution history in API responses
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

// PriceResponse represents price data in API responses
type PriceResponse struct {
	TokenPair    string          `json:"token_pair"`
	Price        decimal.Decimal `json:"price"`
	Timestamp    time.Time       `json:"timestamp"`
	Source       string          `json:"source"`
	WindowMinutes *int           `json:"window_minutes,omitempty"`
}

// TWAPPriceResponse represents TWAP price calculation results
type TWAPPriceResponse struct {
	TokenPair        string          `json:"token_pair"`
	WindowMinutes    int             `json:"window_minutes"`
	TWAPPrice        decimal.Decimal `json:"twap_price"`
	CurrentPrice     decimal.Decimal `json:"current_price,omitempty"`
	PriceChange      decimal.Decimal `json:"price_change,omitempty"`
	DataPoints       int             `json:"data_points"`
	CalculatedAt     time.Time       `json:"calculated_at"`
}

// ChainStatusResponse represents blockchain status in API responses
type ChainStatusResponse struct {
	ChainID         string     `json:"chain_id"`
	Name            string     `json:"name"`
	Enabled         bool       `json:"enabled"`
	LastBlockHeight *int64     `json:"last_block_height,omitempty"`
	LastBlockTime   *time.Time `json:"last_block_time,omitempty"`
	AvgBlockTime    *string    `json:"avg_block_time,omitempty"`
	GasPrice        *decimal.Decimal `json:"gas_price,omitempty"`
	HealthStatus    string     `json:"health_status"`
	LastHealthCheck time.Time  `json:"last_health_check"`
}

// ChainMetricsResponse represents chain performance metrics
type ChainMetricsResponse struct {
	ChainID              string          `json:"chain_id"`
	Name                 string          `json:"name"`
	OrderCount           int64           `json:"order_count"`
	TotalVolume          decimal.Decimal `json:"total_volume"`
	AverageBlockTime     string          `json:"average_block_time"`
	CurrentBlockHeight   int64           `json:"current_block_height"`
	GasPrice             decimal.Decimal `json:"gas_price"`
	SuccessfulExecutions int64           `json:"successful_executions"`
	FailedExecutions     int64           `json:"failed_executions"`
	SuccessRate          float64         `json:"success_rate"`
	LastHealthCheck      time.Time       `json:"last_health_check"`
	HealthStatus         string          `json:"health_status"`
}

// MetricsResponse represents system performance metrics
type MetricsResponse struct {
	TotalExecutions      int64           `json:"total_executions"`
	SuccessfulExecutions int64           `json:"successful_executions"`
	FailedExecutions     int64           `json:"failed_executions"`
	SuccessRate          float64         `json:"success_rate"`
	AverageExecutionTime string          `json:"average_execution_time"`
	AverageSlippage      decimal.Decimal `json:"average_slippage"`
	TotalVolumeExecuted  decimal.Decimal `json:"total_volume_executed"`
	LastExecutionTime    time.Time       `json:"last_execution_time"`
}

// StatisticsResponse represents system statistics
type StatisticsResponse struct {
	TotalOrders       int64                       `json:"total_orders"`
	ActiveOrders      int64                       `json:"active_orders"`
	CompletedOrders   int64                       `json:"completed_orders"`
	CancelledOrders   int64                       `json:"cancelled_orders"`
	TotalVolume       decimal.Decimal             `json:"total_volume"`
	VolumeByChain     map[string]decimal.Decimal  `json:"volume_by_chain"`
	VolumeByTimeframe map[string]decimal.Decimal  `json:"volume_by_timeframe"`
	AverageSlippage   decimal.Decimal             `json:"average_slippage"`
	AverageExecutionTime string                   `json:"average_execution_time"`
	SuccessRate       float64                     `json:"success_rate"`
	LastUpdateTime    time.Time                   `json:"last_update_time"`
}

// ErrorResponse represents API error responses
type ErrorResponse struct {
	Error   string                 `json:"error"`
	Code    string                 `json:"code,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
	RequestID string               `json:"request_id,omitempty"`
	Timestamp time.Time             `json:"timestamp"`
}

// SuccessResponse represents successful API responses
type SuccessResponse struct {
	Success   bool                   `json:"success"`
	Data      interface{}            `json:"data,omitempty"`
	Message   string                 `json:"message,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// PaginationResponse represents pagination metadata
type PaginationResponse struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

// WebSocketMessage represents WebSocket message structure
type WebSocketMessage struct {
	Type      string                 `json:"type"`
	Event     string                 `json:"event"`
	Data      interface{}            `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
	RequestID string                 `json:"request_id,omitempty"`
}

// WebSocket event types
const (
	WSEventOrderCreated   = "order_created"
	WSEventOrderUpdated   = "order_updated"
	WSEventOrderExecuted  = "order_executed"
	WSEventOrderCompleted = "order_completed"
	WSEventOrderCancelled = "order_cancelled"
	WSEventPriceUpdate    = "price_update"
	WSEventChainStatus    = "chain_status"
	WSEventError          = "error"
)

// Validation methods

// Validate validates a CreateOrderRequest
func (r *CreateOrderRequest) Validate() error {
	// Basic validation
	if r.ID == "" {
		return errors.New("order ID is required")
	}
	
	if r.UserAddress == "" {
		return errors.New("user address is required")
	}
	
	if r.SourceChain == r.TargetChain {
		return errors.New("source and target chains must be different")
	}
	
	if r.SourceAmount.LessThanOrEqual(decimal.Zero) {
		return errors.New("source amount must be greater than zero")
	}
	
	if r.MinReceived.LessThanOrEqual(decimal.Zero) {
		return errors.New("minimum received amount must be greater than zero")
	}
	
	if r.TimeoutHeight <= 0 {
		return errors.New("timeout height must be greater than zero")
	}
	
	if r.TimeoutTimestamp <= time.Now().Unix() {
		return errors.New("timeout timestamp must be in the future")
	}
	
	// Validate TWAP config
	return r.TWAPConfig.Validate()
}

// Validate validates a TWAPConfigRequest
func (t *TWAPConfigRequest) Validate() error {
	if t.WindowMinutes < 5 || t.WindowMinutes > 1440 {
		return errors.New("window minutes must be between 5 and 1440")
	}
	
	if t.ExecutionIntervals < 2 || t.ExecutionIntervals > 20 {
		return errors.New("execution intervals must be between 2 and 20")
	}
	
	if t.MaxSlippage < 1 || t.MaxSlippage > 1000 {
		return errors.New("max slippage must be between 1 and 1000 basis points")
	}
	
	if t.MinFillSize.LessThanOrEqual(decimal.Zero) {
		return errors.New("minimum fill size must be greater than zero")
	}
	
	// Check that intervals fit within window
	intervalDuration := t.WindowMinutes / t.ExecutionIntervals
	if intervalDuration < 1 {
		return errors.New("execution intervals too frequent for the given window")
	}
	
	return nil
}

// Query parameter helpers

// ListOrdersQuery represents query parameters for listing orders
type ListOrdersQuery struct {
	UserAddress  string `form:"user"`
	SourceChain  string `form:"source_chain"`
	TargetChain  string `form:"target_chain"`
	Status       string `form:"status"`
	CreatedAfter string `form:"created_after"`
	CreatedBefore string `form:"created_before"`
	Limit        int    `form:"limit" binding:"min=1,max=100"`
	Offset       int    `form:"offset" binding:"min=0"`
	SortBy       string `form:"sort_by"`
	SortOrder    string `form:"sort_order"`
}

// PriceHistoryQuery represents query parameters for price history
type PriceHistoryQuery struct {
	TokenPair string `form:"pair" binding:"required"`
	Window    int    `form:"window" binding:"min=5,max=10080"` // Max 1 week
	Source    string `form:"source"`
	ChainID   string `form:"chain_id"`
	Format    string `form:"format"` // json, csv
}

// TWAPQuery represents query parameters for TWAP calculations
type TWAPQuery struct {
	TokenPair string `form:"pair" binding:"required"`
	Window    int    `form:"window" binding:"min=5,max=1440"`
	Source    string `form:"source"`
}

// Default values for query parameters
const (
	DefaultLimit       = 20
	DefaultOffset      = 0
	DefaultWindow      = 60
	DefaultSortBy      = "created_at"
	DefaultSortOrder   = "desc"
	MaxLimit           = 100
	MinWindow          = 5
	MaxWindow          = 1440
	MaxHistoryWindow   = 10080 // 1 week
)

// Helper functions for response building

// NewSuccessResponse creates a standardized success response
func NewSuccessResponse(data interface{}, message string) *SuccessResponse {
	return &SuccessResponse{
		Success:   true,
		Data:      data,
		Message:   message,
		Timestamp: time.Now().UTC(),
	}
}

// NewErrorResponse creates a standardized error response
func NewErrorResponse(err error, code string) *ErrorResponse {
	return &ErrorResponse{
		Error:     err.Error(),
		Code:      code,
		Timestamp: time.Now().UTC(),
	}
}

// NewPaginationResponse creates pagination metadata
func NewPaginationResponse(page, limit int, total int64) *PaginationResponse {
	totalPages := int((total + int64(limit) - 1) / int64(limit))
	
	return &PaginationResponse{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    page < totalPages,
		HasPrev:    page > 1,
	}
}

// Response status codes
const (
	StatusSuccess = "success"
	StatusError   = "error"
	StatusPending = "pending"
)

// Error codes for API responses
const (
	ErrCodeValidation      = "VALIDATION_ERROR"
	ErrCodeNotFound        = "NOT_FOUND"
	ErrCodeUnauthorized    = "UNAUTHORIZED"
	ErrCodeForbidden       = "FORBIDDEN"
	ErrCodeConflict        = "CONFLICT"
	ErrCodeInternalError   = "INTERNAL_ERROR"
	ErrCodeServiceUnavailable = "SERVICE_UNAVAILABLE"
	ErrCodeRateLimit       = "RATE_LIMIT"
	ErrCodeChainError      = "CHAIN_ERROR"
	ErrCodeInsufficientFunds = "INSUFFICIENT_FUNDS"
	ErrCodeSlippageExceeded = "SLIPPAGE_EXCEEDED"
	ErrCodeOrderExpired    = "ORDER_EXPIRED"
	ErrCodeInvalidChain    = "INVALID_CHAIN"
	ErrCodeInvalidToken    = "INVALID_TOKEN"
)
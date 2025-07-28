package database

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// Order represents a TWAP order in the database
type Order struct {
	ID                  string          `json:"id" db:"id"`
	UserAddress         string          `json:"user_address" db:"user_address"`
	SourceChain         string          `json:"source_chain" db:"source_chain"`
	TargetChain         string          `json:"target_chain" db:"target_chain"`
	SourceToken         string          `json:"source_token" db:"source_token"`
	SourceAmount        decimal.Decimal `json:"source_amount" db:"source_amount"`
	TargetToken         string          `json:"target_token" db:"target_token"`
	TargetRecipient     string          `json:"target_recipient" db:"target_recipient"`
	MinReceived         decimal.Decimal `json:"min_received" db:"min_received"`
	WindowMinutes       int             `json:"window_minutes" db:"window_minutes"`
	ExecutionIntervals  int             `json:"execution_intervals" db:"execution_intervals"`
	MaxSlippage         int             `json:"max_slippage" db:"max_slippage"`
	MinFillSize         decimal.Decimal `json:"min_fill_size" db:"min_fill_size"`
	EnableMEVProtection bool            `json:"enable_mev_protection" db:"enable_mev_protection"`
	HTLCHash            string          `json:"htlc_hash" db:"htlc_hash"`
	TimeoutHeight       int64           `json:"timeout_height" db:"timeout_height"`
	TimeoutTimestamp    int64           `json:"timeout_timestamp" db:"timeout_timestamp"`
	CreatedAt           time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at" db:"updated_at"`
	ExecutedAmount      decimal.Decimal `json:"executed_amount" db:"executed_amount"`
	LastExecution       *time.Time      `json:"last_execution" db:"last_execution"`
	Status              string          `json:"status" db:"status"`
	AveragePrice        decimal.Decimal `json:"average_price" db:"average_price"`
	Metadata            Metadata        `json:"metadata" db:"metadata"`
}

// ExecutionRecord represents a single TWAP execution interval
type ExecutionRecord struct {
	ID             int64           `json:"id" db:"id"`
	OrderID        string          `json:"order_id" db:"order_id"`
	IntervalNumber int             `json:"interval_number" db:"interval_number"`
	Timestamp      time.Time       `json:"timestamp" db:"timestamp"`
	Amount         decimal.Decimal `json:"amount" db:"amount"`
	Price          decimal.Decimal `json:"price" db:"price"`
	GasUsed        *int64          `json:"gas_used" db:"gas_used"`
	Slippage       *int            `json:"slippage" db:"slippage"`
	TxHash         *string         `json:"tx_hash" db:"tx_hash"`
	ChainID        string          `json:"chain_id" db:"chain_id"`
}

// PricePoint represents a price data point for TWAP calculations
type PricePoint struct {
	ID        int64           `json:"id" db:"id"`
	TokenPair string          `json:"token_pair" db:"token_pair"`
	Timestamp time.Time       `json:"timestamp" db:"timestamp"`
	Price     decimal.Decimal `json:"price" db:"price"`
	Volume    *decimal.Decimal `json:"volume" db:"volume"`
	Source    string          `json:"source" db:"source"`
	ChainID   string          `json:"chain_id" db:"chain_id"`
}


// HTLC represents a Hash Time Lock Contract
type HTLC struct {
	Address          string     `json:"address" db:"address"`
	OrderID          string     `json:"order_id" db:"order_id"`
	HashedSecret     string     `json:"hashed_secret" db:"hashed_secret"`
	Amount           decimal.Decimal `json:"amount" db:"amount"`
	Token            string     `json:"token" db:"token"`
	Sender           string     `json:"sender" db:"sender"`
	Receiver         string     `json:"receiver" db:"receiver"`
	TimeoutHeight    int64      `json:"timeout_height" db:"timeout_height"`
	TimeoutTimestamp int64      `json:"timeout_timestamp" db:"timeout_timestamp"`
	Status           string     `json:"status" db:"status"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	ClaimedAt        *time.Time `json:"claimed_at" db:"claimed_at"`
	Secret           *string    `json:"secret" db:"secret"`
	ChainID          string     `json:"chain_id" db:"chain_id"`
}

// ChainStatus represents the status of a blockchain
type ChainStatus struct {
	ChainID         string     `json:"chain_id" db:"chain_id"`
	Name            string     `json:"name" db:"name"`
	Enabled         bool       `json:"enabled" db:"enabled"`
	LastBlockHeight *int64     `json:"last_block_height" db:"last_block_height"`
	LastBlockTime   *time.Time `json:"last_block_time" db:"last_block_time"`
	AvgBlockTime    *string    `json:"avg_block_time" db:"avg_block_time"`
	GasPrice        *decimal.Decimal `json:"gas_price" db:"gas_price"`
	HealthStatus    string     `json:"health_status" db:"health_status"`
	LastHealthCheck time.Time  `json:"last_health_check" db:"last_health_check"`
}

// Metadata represents additional order metadata
type Metadata map[string]interface{}

// Value implements the driver.Valuer interface for database storage
func (m Metadata) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

// Scan implements the sql.Scanner interface for database retrieval
func (m *Metadata) Scan(value interface{}) error {
	if value == nil {
		*m = nil
		return nil
	}

	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, m)
	case string:
		return json.Unmarshal([]byte(v), m)
	default:
		return errors.New("cannot scan non-string into Metadata")
	}
}

// OrderStatus represents the various states of an order
type OrderStatus string

const (
	OrderStatusPending         OrderStatus = "pending"
	OrderStatusExecuting       OrderStatus = "executing"
	OrderStatusPartiallyFilled OrderStatus = "partially_filled"
	OrderStatusCompleted       OrderStatus = "completed"
	OrderStatusCancelled       OrderStatus = "cancelled"
	OrderStatusExpired         OrderStatus = "expired"
	OrderStatusRefunded        OrderStatus = "refunded"
	OrderStatusClaimed         OrderStatus = "claimed"
)

// HTLCStatus represents the various states of an HTLC
type HTLCStatus string

const (
	HTLCStatusActive   HTLCStatus = "active"
	HTLCStatusClaimed  HTLCStatus = "claimed"
	HTLCStatusRefunded HTLCStatus = "refunded"
	HTLCStatusExpired  HTLCStatus = "expired"
)

// HealthStatus represents the health status of a blockchain
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// OrderFilter represents filters for querying orders
type OrderFilter struct {
	UserAddress  *string
	SourceChain  *string
	TargetChain  *string
	Status       *OrderStatus
	CreatedAfter *time.Time
	CreatedBefore *time.Time
	Limit        int
	Offset       int
}

// PriceFilter represents filters for querying price history
type PriceFilter struct {
	TokenPair     string
	ChainID       *string
	Source        *string
	TimestampFrom *time.Time
	TimestampTo   *time.Time
	Limit         int
}

// ExecutionFilter represents filters for querying execution history
type ExecutionFilter struct {
	OrderID       *string
	ChainID       *string
	TimestampFrom *time.Time
	TimestampTo   *time.Time
	Limit         int
}

// Statistics represents various system statistics
type Statistics struct {
	TotalOrders       int64           `json:"total_orders"`
	ActiveOrders      int64           `json:"active_orders"`
	CompletedOrders   int64           `json:"completed_orders"`
	TotalVolume       decimal.Decimal `json:"total_volume"`
	VolumeByChain     map[string]decimal.Decimal `json:"volume_by_chain"`
	AverageSlippage   decimal.Decimal `json:"average_slippage"`
	AverageExecutionTime time.Duration `json:"average_execution_time"`
	SuccessRate       float64         `json:"success_rate"`
	LastUpdateTime    time.Time       `json:"last_update_time"`
}

// OrderSummary represents a summary of an order for API responses
type OrderSummary struct {
	ID               string          `json:"id"`
	UserAddress      string          `json:"user_address"`
	SourceChain      string          `json:"source_chain"`
	TargetChain      string          `json:"target_chain"`
	SourceAmount     decimal.Decimal `json:"source_amount"`
	ExecutedAmount   decimal.Decimal `json:"executed_amount"`
	Status           string          `json:"status"`
	CreatedAt        time.Time       `json:"created_at"`
	CompletionRate   float64         `json:"completion_rate"`
	AveragePrice     decimal.Decimal `json:"average_price"`
	IntervalsExecuted int            `json:"intervals_executed"`
	TotalIntervals   int             `json:"total_intervals"`
}

// TWAPData represents TWAP calculation data
type TWAPData struct {
	TokenPair        string          `json:"token_pair"`
	WindowMinutes    int             `json:"window_minutes"`
	TWAPPrice        decimal.Decimal `json:"twap_price"`
	VWAPPrice        decimal.Decimal `json:"vwap_price"`
	LastPrice        decimal.Decimal `json:"last_price"`
	PriceChange      decimal.Decimal `json:"price_change"`
	PriceChangePercent decimal.Decimal `json:"price_change_percent"`
	Volume           decimal.Decimal `json:"volume"`
	NumDataPoints    int             `json:"num_data_points"`
	CalculatedAt     time.Time       `json:"calculated_at"`
}

// ChainMetrics represents metrics for a specific blockchain
type ChainMetrics struct {
	ChainID              string          `json:"chain_id"`
	Name                 string          `json:"name"`
	OrderCount           int64           `json:"order_count"`
	TotalVolume          decimal.Decimal `json:"total_volume"`
	AverageBlockTime     time.Duration   `json:"average_block_time"`
	CurrentBlockHeight   int64           `json:"current_block_height"`
	GasPrice             decimal.Decimal `json:"gas_price"`
	SuccessfulExecutions int64           `json:"successful_executions"`
	FailedExecutions     int64           `json:"failed_executions"`
	SuccessRate          float64         `json:"success_rate"`
	LastHealthCheck      time.Time       `json:"last_health_check"`
	HealthStatus         string          `json:"health_status"`
}

// Database errors
var (
	ErrOrderNotFound     = errors.New("order not found")
	ErrHTLCNotFound      = errors.New("HTLC not found")
	ErrChainNotFound     = errors.New("chain not found")
	ErrDuplicateOrder    = errors.New("duplicate order")
	ErrInvalidOrderStatus = errors.New("invalid order status")
	ErrOrderExpired      = errors.New("order expired")
	ErrInsufficientData  = errors.New("insufficient data for calculation")
)

// Helper functions

// IsValidOrderStatus checks if the order status is valid
func IsValidOrderStatus(status string) bool {
	switch OrderStatus(status) {
	case OrderStatusPending, OrderStatusExecuting, OrderStatusPartiallyFilled,
		 OrderStatusCompleted, OrderStatusCancelled, OrderStatusExpired,
		 OrderStatusRefunded, OrderStatusClaimed:
		return true
	default:
		return false
	}
}

// IsValidHTLCStatus checks if the HTLC status is valid
func IsValidHTLCStatus(status string) bool {
	switch HTLCStatus(status) {
	case HTLCStatusActive, HTLCStatusClaimed, HTLCStatusRefunded, HTLCStatusExpired:
		return true
	default:
		return false
	}
}

// CalculateCompletionRate calculates the completion rate of an order
func (o *Order) CalculateCompletionRate() float64 {
	if o.SourceAmount.IsZero() {
		return 0
	}
	executed, _ := o.ExecutedAmount.Float64()
	total, _ := o.SourceAmount.Float64()
	return executed / total * 100
}

// IsExpired checks if an order has expired based on timeout height
func (o *Order) IsExpired(currentHeight int64) bool {
	return currentHeight >= o.TimeoutHeight
}

// IsTimedOut checks if an order has timed out based on timestamp
func (o *Order) IsTimedOut() bool {
	return time.Now().Unix() >= o.TimeoutTimestamp
}

// GetNextExecutionTime calculates when the next execution should occur
func (o *Order) GetNextExecutionTime() time.Time {
	if o.LastExecution == nil {
		return o.CreatedAt
	}
	
	intervalDuration := time.Duration(o.WindowMinutes/o.ExecutionIntervals) * time.Minute
	return o.LastExecution.Add(intervalDuration)
}

// CanExecuteInterval checks if an interval can be executed now
func (o *Order) CanExecuteInterval() bool {
	now := time.Now()
	nextExecution := o.GetNextExecutionTime()
	return now.After(nextExecution) || now.Equal(nextExecution)
}

// GetRemainingAmount calculates the remaining amount to be executed
func (o *Order) GetRemainingAmount() decimal.Decimal {
	return o.SourceAmount.Sub(o.ExecutedAmount)
}

// GetExecutedIntervals calculates how many intervals have been executed
func (o *Order) GetExecutedIntervals(executionHistory []*ExecutionRecord) int {
	return len(executionHistory)
}

// GetRemainingIntervals calculates how many intervals remain
func (o *Order) GetRemainingIntervals(executionHistory []*ExecutionRecord) int {
	executed := o.GetExecutedIntervals(executionHistory)
	return o.ExecutionIntervals - executed
}

// UpdateAveragePrice updates the weighted average price with a new execution
func (o *Order) UpdateAveragePrice(executionAmount, executionPrice decimal.Decimal) {
	if o.ExecutedAmount.IsZero() {
		o.AveragePrice = executionPrice
		return
	}

	// Calculate weighted average: (prevAvg * prevAmount + newPrice * newAmount) / totalAmount
	prevValue := o.AveragePrice.Mul(o.ExecutedAmount)
	newValue := executionPrice.Mul(executionAmount)
	totalValue := prevValue.Add(newValue)
	newTotalAmount := o.ExecutedAmount.Add(executionAmount)
	
	if !newTotalAmount.IsZero() {
		o.AveragePrice = totalValue.Div(newTotalAmount)
	}
}
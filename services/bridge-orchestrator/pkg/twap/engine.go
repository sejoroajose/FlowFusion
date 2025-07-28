package twap

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"flowfusion/bridge-orchestrator/internal/config"
	"flowfusion/bridge-orchestrator/internal/database"
	"flowfusion/bridge-orchestrator/pkg/adapters"
)

// Engine handles TWAP calculations and execution logic
type Engine struct {
	config         config.TWAPConfig
	db             database.DB
	adapterManager *adapters.Manager
	logger         *zap.Logger

	// Internal state
	priceCache     *PriceCache
	executionQueue chan *ExecutionRequest
	stopChan       chan struct{}
	wg             sync.WaitGroup
	mutex          sync.RWMutex

	// Metrics
	metrics *Metrics
}

// ExecutionRequest represents a request to execute a TWAP interval
type ExecutionRequest struct {
	OrderID         string
	IntervalNumber  int
	TargetAmount    decimal.Decimal
	MaxSlippage     int
	PriceHint       decimal.Decimal
	ResponseChannel chan *ExecutionResponse
}

// ExecutionResponse represents the result of a TWAP execution
type ExecutionResponse struct {
	Success       bool
	ExecutedAmount decimal.Decimal
	ExecutionPrice decimal.Decimal
	TxHash        string
	GasUsed       uint64
	Slippage      int
	Error         error
}

// PriceCache manages cached price data for TWAP calculations
type PriceCache struct {
	data   map[string][]*PricePoint
	mutex  sync.RWMutex
	maxAge time.Duration
}

// PricePoint represents a price data point
type PricePoint struct {
	Timestamp time.Time
	Price     decimal.Decimal
	Volume    decimal.Decimal
	Source    string
}

// Metrics tracks TWAP engine performance
type Metrics struct {
	TotalExecutions       int64
	SuccessfulExecutions  int64
	FailedExecutions      int64
	AverageExecutionTime  time.Duration
	AverageSlippage       decimal.Decimal
	TotalVolumeExecuted   decimal.Decimal
	LastExecutionTime     time.Time
	mutex                sync.RWMutex
}

// NewEngine creates a new TWAP engine
func NewEngine(
	config config.TWAPConfig,
	db database.DB,
	adapterManager *adapters.Manager,
	logger *zap.Logger,
) (*Engine, error) {
	engine := &Engine{
		config:         config,
		db:             db,
		adapterManager: adapterManager,
		logger:         logger,
		priceCache: &PriceCache{
			data:   make(map[string][]*PricePoint),
			maxAge: 24 * time.Hour,
		},
		executionQueue: make(chan *ExecutionRequest, 100),
		stopChan:       make(chan struct{}),
		metrics:        &Metrics{},
	}

	return engine, nil
}

// Start starts the TWAP engine
func (e *Engine) Start(ctx context.Context) error {
	e.logger.Info("Starting TWAP engine")

	// Start price feed updater
	e.wg.Add(1)
	go e.priceFeedUpdater(ctx)

	// Start order processor
	e.wg.Add(1)
	go e.orderProcessor(ctx)

	// Start execution worker
	e.wg.Add(1)
	go e.executionWorker(ctx)

	// Start metrics updater
	e.wg.Add(1)
	go e.metricsUpdater(ctx)

	// Wait for context cancellation
	<-ctx.Done()
	
	e.logger.Info("Stopping TWAP engine")
	close(e.stopChan)
	e.wg.Wait()

	return nil
}

// priceFeedUpdater continuously updates price data from various sources
func (e *Engine) priceFeedUpdater(ctx context.Context) {
	defer e.wg.Done()
	
	ticker := time.NewTicker(e.config.PriceUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopChan:
			return
		case <-ticker.C:
			if err := e.updatePriceFeeds(); err != nil {
				e.logger.Error("Failed to update price feeds", zap.Error(err))
			}
		}
	}
}

// orderProcessor identifies and queues orders ready for execution
func (e *Engine) orderProcessor(ctx context.Context) {
	defer e.wg.Done()
	
	ticker := time.NewTicker(e.config.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopChan:
			return
		case <-ticker.C:
			if err := e.processExecutableOrders(); err != nil {
				e.logger.Error("Failed to process executable orders", zap.Error(err))
			}
		}
	}
}

// executionWorker processes execution requests
func (e *Engine) executionWorker(ctx context.Context) {
	defer e.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopChan:
			return
		case request := <-e.executionQueue:
			response := e.executeInterval(request)
			if request.ResponseChannel != nil {
				request.ResponseChannel <- response
			}
		}
	}
}

// metricsUpdater periodically updates performance metrics
func (e *Engine) metricsUpdater(ctx context.Context) {
	defer e.wg.Done()
	
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopChan:
			return
		case <-ticker.C:
			e.updateMetrics()
		}
	}
}

// updatePriceFeeds fetches latest price data from all sources
func (e *Engine) updatePriceFeeds() error {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    tokenPairs := []string{"ETH_USDC", "ATOM_USDC", "XLM_USDC"}
    
    for _, pair := range tokenPairs {
        // Chainlink Price Feed
        if price, err := e.getChainlinkPrice(ctx, pair); err == nil {
            if err := e.storePricePoint(pair, "chainlink", price); err != nil {
                e.logger.Error("Failed to store Chainlink price", 
                    zap.String("pair", pair), zap.Error(err))
            }
        }

        // CoinGecko API
        if price, err := e.getCoinGeckoPrice(ctx, pair); err == nil {
            if err := e.storePricePoint(pair, "coingecko", price); err != nil {
                e.logger.Error("Failed to store CoinGecko price", 
                    zap.String("pair", pair), zap.Error(err))
            }
        }

        // DEX aggregator prices (1inch, etc.)
        if price, err := e.getDEXPrice(ctx, pair); err == nil {
            if err := e.storePricePoint(pair, "dex", price); err != nil {
                e.logger.Error("Failed to store DEX price", 
                    zap.String("pair", pair), zap.Error(err))
            }
        }
    }

    return nil
}

// processExecutableOrders finds orders ready for execution
func (e *Engine) processExecutableOrders() error {
	orders, err := e.db.GetExecutableOrders()
	if err != nil {
		return fmt.Errorf("failed to get executable orders: %w", err)
	}

	e.logger.Debug("Processing executable orders", zap.Int("count", len(orders)))

	for _, order := range orders {
		if err := e.processOrder(order); err != nil {
			e.logger.Error("Failed to process order",
				zap.String("order_id", order.ID),
				zap.Error(err))
		}
	}

	return nil
}

// processOrder determines if an order is ready for execution and queues it
func (e *Engine) processOrder(order *database.Order) error {
	if !order.CanExecuteInterval() {
		return nil
	}

	history, err := e.db.GetExecutionHistory(order.ID)
	if err != nil {
		return fmt.Errorf("failed to get execution history: %w", err)
	}

	if len(history) >= order.ExecutionIntervals {
		order.Status = string(database.OrderStatusCompleted)
		return e.db.UpdateOrder(order)
	}

	// Calculate target amount for this interval
	remainingAmount := order.GetRemainingAmount()
	remainingIntervals := order.GetRemainingIntervals(history)
	
	if remainingIntervals <= 0 {
		return nil
	}

	targetAmount := remainingAmount.Div(decimal.NewFromInt(int64(remainingIntervals)))

	// Check minimum fill size
	if targetAmount.LessThan(order.MinFillSize) && remainingIntervals > 1 {
		e.logger.Debug("Interval amount below minimum fill size",
			zap.String("order_id", order.ID),
			zap.String("target_amount", targetAmount.String()),
			zap.String("min_fill_size", order.MinFillSize.String()))
		return nil
	}

	// Calculate TWAP price for validation
	tokenPair := fmt.Sprintf("%s_%s", order.SourceToken, order.TargetToken)
	twapPrice, err := e.calculateTWAP(tokenPair, order.WindowMinutes)
	if err != nil {
		e.logger.Warn("Failed to calculate TWAP price",
			zap.String("order_id", order.ID),
			zap.String("token_pair", tokenPair),
			zap.Error(err))
		twapPrice = decimal.Zero // Use zero as fallback
	}

	// Create execution request
	request := &ExecutionRequest{
		OrderID:        order.ID,
		IntervalNumber: len(history),
		TargetAmount:   targetAmount,
		MaxSlippage:    order.MaxSlippage,
		PriceHint:      twapPrice,
	}

	// Queue for execution
	select {
	case e.executionQueue <- request:
		e.logger.Debug("Queued order for execution",
			zap.String("order_id", order.ID),
			zap.Int("interval", request.IntervalNumber))
	default:
		e.logger.Warn("Execution queue full, skipping order",
			zap.String("order_id", order.ID))
	}

	return nil
}

// executeInterval executes a single TWAP interval
func (e *Engine) executeInterval(request *ExecutionRequest) *ExecutionResponse {
	startTime := time.Now()
	
	e.logger.Info("Executing TWAP interval",
		zap.String("order_id", request.OrderID),
		zap.Int("interval", request.IntervalNumber),
		zap.String("target_amount", request.TargetAmount.String()))

	// Get order details
	order, err := e.db.GetOrder(request.OrderID)
	if err != nil {
		return &ExecutionResponse{
			Success: false,
			Error:   fmt.Errorf("failed to get order: %w", err),
		}
	}

	// Get chain adapter for target chain
	adapter, err := e.adapterManager.GetAdapter(order.TargetChain)
	if err != nil {
		return &ExecutionResponse{
			Success: false,
			Error:   fmt.Errorf("failed to get adapter for chain %s: %w", order.TargetChain, err),
		}
	}

	// Calculate current market price
	tokenPair := fmt.Sprintf("%s_%s", order.SourceToken, order.TargetToken)
	marketPrice, err := e.getCurrentPrice(tokenPair)
	if err != nil {
		e.logger.Warn("Failed to get current market price, using TWAP",
			zap.String("token_pair", tokenPair),
			zap.Error(err))
		marketPrice = request.PriceHint
	}

	// Validate slippage
	if !request.PriceHint.IsZero() {
		slippage := e.calculateSlippage(request.PriceHint, marketPrice)
		if slippage > request.MaxSlippage {
			return &ExecutionResponse{
				Success: false,
				Error:   fmt.Errorf("slippage %d exceeds maximum %d", slippage, request.MaxSlippage),
			}
		}
	}

	// Execute the swap (mock implementation)
	executedAmount, executionPrice, txHash, gasUsed, err := e.executeSwap(
		adapter,
		order.SourceToken,
		order.TargetToken,
		request.TargetAmount,
		marketPrice,
	)
	if err != nil {
		e.updateMetricsOnFailure()
		return &ExecutionResponse{
			Success: false,
			Error:   fmt.Errorf("swap execution failed: %w", err),
		}
	}

	// Calculate actual slippage
	actualSlippage := 0
	if !request.PriceHint.IsZero() {
		actualSlippage = e.calculateSlippage(request.PriceHint, executionPrice)
	}

	// Record execution in database
	executionRecord := &database.ExecutionRecord{
		OrderID:        request.OrderID,
		IntervalNumber: request.IntervalNumber,
		Timestamp:      time.Now(),
		Amount:         executedAmount,
		Price:          executionPrice,
		GasUsed:        &gasUsed,
		Slippage:       &actualSlippage,
		TxHash:         &txHash,
		ChainID:        order.TargetChain,
	}

	if err := e.db.CreateExecutionRecord(executionRecord); err != nil {
		e.logger.Error("Failed to record execution", zap.Error(err))
	}

	// Update order state
	order.ExecutedAmount = order.ExecutedAmount.Add(executedAmount)
	order.LastExecution = &executionRecord.Timestamp
	order.UpdateAveragePrice(executedAmount, executionPrice)
	order.Status = string(database.OrderStatusExecuting)

	// Check if order is complete
	if order.ExecutedAmount.GreaterThanOrEqual(order.SourceAmount) {
		order.Status = string(database.OrderStatusCompleted)
	}

	if err := e.db.UpdateOrder(order); err != nil {
		e.logger.Error("Failed to update order", zap.Error(err))
	}

	// Update metrics
	e.updateMetricsOnSuccess(time.Since(startTime), actualSlippage, executedAmount)

	e.logger.Info("TWAP interval executed successfully",
		zap.String("order_id", request.OrderID),
		zap.String("executed_amount", executedAmount.String()),
		zap.String("execution_price", executionPrice.String()),
		zap.Int("slippage_bps", actualSlippage),
		zap.String("tx_hash", txHash))

	return &ExecutionResponse{
		Success:        true,
		ExecutedAmount: executedAmount,
		ExecutionPrice: executionPrice,
		TxHash:         txHash,
		GasUsed:        gasUsed,
		Slippage:       actualSlippage,
	}
}

// executeSwap performs the actual token swap (mock implementation)
func (e *Engine) executeSwap(
	adapter adapters.ChainAdapter,
	sourceToken, targetToken string,
	amount, priceHint decimal.Decimal,
) (executedAmount, executionPrice decimal.Decimal, txHash string, gasUsed int64, err error) {
	// Mock implementation - in reality this would call the appropriate adapter
	// to execute the swap on the target chain
	
	// Simulate some variance in execution
	variance := decimal.NewFromFloat(0.99 + 0.02*float64(time.Now().Unix()%100)/100)
	executedAmount = amount.Mul(variance)
	
	// Simulate price with small slippage
	priceVariance := decimal.NewFromFloat(0.995 + 0.01*float64(time.Now().Unix()%100)/100)
	executionPrice = priceHint.Mul(priceVariance)
	
	// Mock transaction hash
	txHash = fmt.Sprintf("0x%x", time.Now().UnixNano())
	
	// Mock gas usage
	gasUsed = 150000 + time.Now().Unix()%50000

	e.logger.Debug("Mock swap executed",
		zap.String("source_token", sourceToken),
		zap.String("target_token", targetToken),
		zap.String("amount", amount.String()),
		zap.String("executed_amount", executedAmount.String()),
		zap.String("execution_price", executionPrice.String()),
		zap.String("tx_hash", txHash))

	return executedAmount, executionPrice, txHash, gasUsed, nil
}

// calculateTWAP calculates the Time-Weighted Average Price
func (e *Engine) calculateTWAP(tokenPair string, windowMinutes int) (decimal.Decimal, error) {
	pricePoints := e.getPricePoints(tokenPair, time.Duration(windowMinutes)*time.Minute)
	
	if len(pricePoints) == 0 {
		return decimal.Zero, fmt.Errorf("no price data available for %s", tokenPair)
	}

	if len(pricePoints) == 1 {
		return pricePoints[0].Price, nil
	}

	// Calculate TWAP using time-weighted average
	var totalValue decimal.Decimal
	var totalWeight decimal.Decimal
	
	windowStart := time.Now().Add(-time.Duration(windowMinutes) * time.Minute)

	for i, point := range pricePoints {
		if point.Timestamp.Before(windowStart) {
			continue
		}

		weight := decimal.NewFromInt(1)
		if i > 0 {
			duration := point.Timestamp.Sub(pricePoints[i-1].Timestamp)
			weight = decimal.NewFromInt(int64(duration.Seconds()))
		}

		totalValue = totalValue.Add(point.Price.Mul(weight))
		totalWeight = totalWeight.Add(weight)
	}

	if totalWeight.IsZero() {
		return decimal.Zero, fmt.Errorf("insufficient data for TWAP calculation")
	}

	twap := totalValue.Div(totalWeight)
	
	e.logger.Debug("TWAP calculated",
		zap.String("token_pair", tokenPair),
		zap.Int("window_minutes", windowMinutes),
		zap.Int("data_points", len(pricePoints)),
		zap.String("twap_price", twap.String()))

	return twap, nil
}

// getCurrentPrice gets the most recent price for a token pair
func (e *Engine) getCurrentPrice(tokenPair string) (decimal.Decimal, error) {
	pricePoints := e.getPricePoints(tokenPair, 1*time.Hour)
	
	if len(pricePoints) == 0 {
		return decimal.Zero, fmt.Errorf("no price data available for %s", tokenPair)
	}

	// Return the most recent price
	return pricePoints[len(pricePoints)-1].Price, nil
}

// calculateSlippage calculates slippage in basis points
func (e *Engine) calculateSlippage(expectedPrice, actualPrice decimal.Decimal) int {
	if expectedPrice.IsZero() {
		return 0
	}

	diff := expectedPrice.Sub(actualPrice).Abs()
	slippage := diff.Div(expectedPrice).Mul(decimal.NewFromInt(10000))
	
	slippageInt, _ := slippage.Float64()
	return int(slippageInt)
}

// Price cache management
func (e *Engine) addPricePoint(tokenPair string, point *PricePoint) {
	e.priceCache.mutex.Lock()
	defer e.priceCache.mutex.Unlock()

	if _, exists := e.priceCache.data[tokenPair]; !exists {
		e.priceCache.data[tokenPair] = make([]*PricePoint, 0)
	}

	// Add new point
	e.priceCache.data[tokenPair] = append(e.priceCache.data[tokenPair], point)

	// Clean old data
	cutoff := time.Now().Add(-e.priceCache.maxAge)
	var filtered []*PricePoint
	for _, p := range e.priceCache.data[tokenPair] {
		if p.Timestamp.After(cutoff) {
			filtered = append(filtered, p)
		}
	}
	e.priceCache.data[tokenPair] = filtered
}

func (e *Engine) getPricePoints(tokenPair string, window time.Duration) []*PricePoint {
	e.priceCache.mutex.RLock()
	defer e.priceCache.mutex.RUnlock()

	points, exists := e.priceCache.data[tokenPair]
	if !exists {
		return nil
	}

	cutoff := time.Now().Add(-window)
	var filtered []*PricePoint
	for _, point := range points {
		if point.Timestamp.After(cutoff) {
			filtered = append(filtered, point)
		}
	}

	return filtered
}

// Metrics management
func (e *Engine) updateMetricsOnSuccess(executionTime time.Duration, slippage int, volume decimal.Decimal) {
	e.metrics.mutex.Lock()
	defer e.metrics.mutex.Unlock()

	e.metrics.TotalExecutions++
	e.metrics.SuccessfulExecutions++
	e.metrics.LastExecutionTime = time.Now()
	e.metrics.TotalVolumeExecuted = e.metrics.TotalVolumeExecuted.Add(volume)

	// Update average execution time
	if e.metrics.TotalExecutions == 1 {
		e.metrics.AverageExecutionTime = executionTime
	} else {
		// Exponential moving average
		alpha := 0.1
		newAvg := time.Duration(float64(e.metrics.AverageExecutionTime)*(1-alpha) + float64(executionTime)*alpha)
		e.metrics.AverageExecutionTime = newAvg
	}

	// Update average slippage
	slippageDecimal := decimal.NewFromInt(int64(slippage))
	if e.metrics.TotalExecutions == 1 {
		e.metrics.AverageSlippage = slippageDecimal
	} else {
		// Exponential moving average
		alpha := decimal.NewFromFloat(0.1)
		oldWeight := decimal.NewFromFloat(0.9)
		e.metrics.AverageSlippage = e.metrics.AverageSlippage.Mul(oldWeight).Add(slippageDecimal.Mul(alpha))
	}
}

func (e *Engine) updateMetricsOnFailure() {
	e.metrics.mutex.Lock()
	defer e.metrics.mutex.Unlock()

	e.metrics.TotalExecutions++
	e.metrics.FailedExecutions++
}

func (e *Engine) updateMetrics() {
	// Update any additional metrics that need periodic recalculation
	e.logger.Debug("Updating TWAP engine metrics",
		zap.Int64("total_executions", e.metrics.TotalExecutions),
		zap.Int64("successful_executions", e.metrics.SuccessfulExecutions),
		zap.String("average_execution_time", e.metrics.AverageExecutionTime.String()))
}

// Public API methods

// GetTWAPPrice calculates TWAP for a token pair
func (e *Engine) GetTWAPPrice(tokenPair string, windowMinutes int) (decimal.Decimal, error) {
	return e.calculateTWAP(tokenPair, windowMinutes)
}

// GetCurrentPrice gets the latest price for a token pair
func (e *Engine) GetCurrentPrice(tokenPair string) (decimal.Decimal, error) {
	return e.getCurrentPrice(tokenPair)
}

// GetMetrics returns current engine metrics
func (e *Engine) GetMetrics() *Metrics {
	e.metrics.mutex.RLock()
	defer e.metrics.mutex.RUnlock()

	// Return a copy to avoid race conditions
	return &Metrics{
		TotalExecutions:       e.metrics.TotalExecutions,
		SuccessfulExecutions:  e.metrics.SuccessfulExecutions,
		FailedExecutions:      e.metrics.FailedExecutions,
		AverageExecutionTime:  e.metrics.AverageExecutionTime,
		AverageSlippage:       e.metrics.AverageSlippage,
		TotalVolumeExecuted:   e.metrics.TotalVolumeExecuted,
		LastExecutionTime:     e.metrics.LastExecutionTime,
	}
}

// ExecuteOrderManually allows manual execution of an order (for testing)
func (e *Engine) ExecuteOrderManually(orderID string) (*ExecutionResponse, error) {
	order, err := e.db.GetOrder(orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	history, err := e.db.GetExecutionHistory(orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get execution history: %w", err)
	}

	if len(history) >= order.ExecutionIntervals {
		return nil, fmt.Errorf("order already fully executed")
	}

	remainingAmount := order.GetRemainingAmount()
	remainingIntervals := order.GetRemainingIntervals(history)
	targetAmount := remainingAmount.Div(decimal.NewFromInt(int64(remainingIntervals)))

	request := &ExecutionRequest{
		OrderID:         orderID,
		IntervalNumber:  len(history),
		TargetAmount:    targetAmount,
		MaxSlippage:     order.MaxSlippage,
		ResponseChannel: make(chan *ExecutionResponse, 1),
	}

	// Queue and wait for response
	select {
	case e.executionQueue <- request:
		return <-request.ResponseChannel, nil
	default:
		return nil, fmt.Errorf("execution queue full")
	}
}
package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"flowfusion/bridge-orchestrator/internal/config"
	"flowfusion/bridge-orchestrator/internal/database"
	"flowfusion/bridge-orchestrator/pkg/adapters"
	"flowfusion/bridge-orchestrator/pkg/twap"
)

// Orchestrator coordinates cross-chain operations and TWAP execution
type Orchestrator struct {
	config         *config.Config
	db             database.DB
	adapterManager *adapters.Manager
	twapEngine     *twap.Engine
	logger         *zap.Logger

	// Internal state
	eventHandlers map[string]EventHandler
	stopChan      chan struct{}
	wg            sync.WaitGroup
	mutex         sync.RWMutex

	// Statistics
	stats *Statistics
}

// EventHandler handles blockchain events
type EventHandler func(event *adapters.ChainEvent) error

// Statistics tracks orchestrator performance
type Statistics struct {
	TotalOrders        int64     `json:"total_orders"`
	ActiveOrders       int64     `json:"active_orders"`
	CompletedOrders    int64     `json:"completed_orders"`
	FailedOrders       int64     `json:"failed_orders"`
	TotalVolume        string    `json:"total_volume"`
	CrossChainSwaps    int64     `json:"cross_chain_swaps"`
	SuccessfulSwaps    int64     `json:"successful_swaps"`
	AverageProcessTime string    `json:"average_process_time"`
	LastProcessedOrder time.Time `json:"last_processed_order"`
	UptimeSeconds      int64     `json:"uptime_seconds"`
	startTime          time.Time
	mutex              sync.RWMutex
}

// New creates a new orchestrator
func New(
	config *config.Config,
	db database.DB,
	adapterManager *adapters.Manager,
	twapEngine *twap.Engine,
	logger *zap.Logger,
) (*Orchestrator, error) {
	orchestrator := &Orchestrator{
		config:         config,
		db:             db,
		adapterManager: adapterManager,
		twapEngine:     twapEngine,
		logger:         logger,
		eventHandlers:  make(map[string]EventHandler),
		stopChan:       make(chan struct{}),
		stats: &Statistics{
			startTime: time.Now(),
		},
	}

	// Setup default event handlers
	orchestrator.setupEventHandlers()

	return orchestrator, nil
}

// Start starts the orchestrator
func (o *Orchestrator) Start(ctx context.Context) error {
	o.logger.Info("Starting bridge orchestrator")

	// Connect all adapters
	if err := o.adapterManager.ConnectAll(); err != nil {
		o.logger.Error("Failed to connect adapters", zap.Error(err))
		return fmt.Errorf("failed to connect adapters: %w", err)
	}

	// Subscribe to blockchain events
	o.wg.Add(1)
	go o.eventSubscriber(ctx)

	// Start order monitor
	o.wg.Add(1)
	go o.orderMonitor(ctx)

	// Start statistics updater
	o.wg.Add(1)
	go o.statisticsUpdater(ctx)

	// Wait for context cancellation
	<-ctx.Done()

	o.logger.Info("Stopping bridge orchestrator")
	close(o.stopChan)
	o.wg.Wait()

	// Disconnect all adapters
	if err := o.adapterManager.DisconnectAll(); err != nil {
		o.logger.Error("Failed to disconnect adapters", zap.Error(err))
	}

	return nil
}

// setupEventHandlers configures default event handlers
func (o *Orchestrator) setupEventHandlers() {
	o.eventHandlers[adapters.EventOrderCreated] = o.handleOrderCreated
	o.eventHandlers[adapters.EventOrderExecuted] = o.handleOrderExecuted
	o.eventHandlers[adapters.EventOrderCompleted] = o.handleOrderCompleted
	o.eventHandlers[adapters.EventHTLCCreated] = o.handleHTLCCreated
	o.eventHandlers[adapters.EventHTLCClaimed] = o.handleHTLCClaimed
	o.eventHandlers[adapters.EventPriceUpdate] = o.handlePriceUpdate
}

// eventSubscriber subscribes to events from all chains
func (o *Orchestrator) eventSubscriber(ctx context.Context) {
	defer o.wg.Done()

	o.logger.Info("Starting event subscriber")

	// Subscribe to events from all adapters
	chainAdapters := o.adapterManager.GetAllAdapters() 
	for chainID, adapter := range chainAdapters {
		go func(chainID string, adapter adapters.ChainAdapter) { 
			if err := adapter.SubscribeToEvents(o.handleEvent); err != nil {
				o.logger.Error("Failed to subscribe to events",
					zap.String("chain_id", chainID),
					zap.Error(err))
			}
		}(chainID, adapter)
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Unsubscribe from all events
	for chainID, adapter := range chainAdapters {
		if err := adapter.UnsubscribeFromEvents(); err != nil {
			o.logger.Error("Failed to unsubscribe from events",
				zap.String("chain_id", chainID),
				zap.Error(err))
		}
	}
}

// orderMonitor monitors order statuses and handles timeouts
func (o *Orchestrator) orderMonitor(ctx context.Context) {
	defer o.wg.Done()

	o.logger.Info("Starting order monitor")

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.stopChan:
			return
		case <-ticker.C:
			if err := o.checkOrderTimeouts(); err != nil {
				o.logger.Error("Failed to check order timeouts", zap.Error(err))
			}
		}
	}
}

// statisticsUpdater updates orchestrator statistics
func (o *Orchestrator) statisticsUpdater(ctx context.Context) {
	defer o.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.stopChan:
			return
		case <-ticker.C:
			o.updateStatistics()
		}
	}
}

// handleEvent routes events to appropriate handlers
func (o *Orchestrator) handleEvent(event *adapters.ChainEvent) error {
	o.logger.Debug("Received blockchain event",
		zap.String("chain_id", event.ChainID),
		zap.String("event_type", event.EventType),
		zap.String("tx_hash", event.TxHash))

	handler, exists := o.eventHandlers[event.EventType]
	if !exists {
		o.logger.Debug("No handler for event type",
			zap.String("event_type", event.EventType))
		return nil
	}

	if err := handler(event); err != nil {
		o.logger.Error("Event handler failed",
			zap.String("event_type", event.EventType),
			zap.String("chain_id", event.ChainID),
			zap.Error(err))
		return err
	}

	return nil
}

// Event handlers
func (o *Orchestrator) handleOrderCreated(event *adapters.ChainEvent) error {
	o.logger.Info("Order created event received",
		zap.String("chain_id", event.ChainID),
		zap.String("tx_hash", event.TxHash))

	// Update statistics
	o.stats.mutex.Lock()
	o.stats.TotalOrders++
	o.stats.ActiveOrders++
	o.stats.mutex.Unlock()

	return nil
}

func (o *Orchestrator) handleOrderExecuted(event *adapters.ChainEvent) error {
	o.logger.Info("Order executed event received",
		zap.String("chain_id", event.ChainID),
		zap.String("tx_hash", event.TxHash))

	// Extract order ID from event data
	orderID, ok := event.Data["order_id"].(string)
	if !ok {
		return fmt.Errorf("missing order_id in event data")
	}

	// Update order in database
	order, err := o.db.GetOrder(orderID)
	if err != nil {
		return fmt.Errorf("failed to get order: %w", err)
	}

	// Update order status based on event
	order.Status = string(database.OrderStatusExecuting)
	order.UpdatedAt = time.Now()

	if err := o.db.UpdateOrder(order); err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}

	return nil
}

func (o *Orchestrator) handleOrderCompleted(event *adapters.ChainEvent) error {
	o.logger.Info("Order completed event received",
		zap.String("chain_id", event.ChainID),
		zap.String("tx_hash", event.TxHash))

	// Update statistics
	o.stats.mutex.Lock()
	o.stats.CompletedOrders++
	o.stats.ActiveOrders--
	o.stats.LastProcessedOrder = time.Now()
	o.stats.mutex.Unlock()

	return nil
}

func (o *Orchestrator) handleHTLCCreated(event *adapters.ChainEvent) error {
	o.logger.Info("HTLC created event received",
		zap.String("chain_id", event.ChainID),
		zap.String("tx_hash", event.TxHash))

	// Extract HTLC data from event
	htlcAddress, ok := event.Data["htlc_address"].(string)
	if !ok {
		return fmt.Errorf("missing htlc_address in event data")
	}

	// Get HTLC details from the chain
	adapter, err := o.adapterManager.GetAdapter(event.ChainID)
	if err != nil {
		return fmt.Errorf("failed to get adapter: %w", err)
	}

	htlcStatus, err := adapter.GetHTLCStatus(htlcAddress)
	if err != nil {
		return fmt.Errorf("failed to get HTLC status: %w", err)
	}

	// Store HTLC in database
	htlc := &database.HTLC{
		Address:          htlcStatus.Address,
		OrderID:          fmt.Sprintf("order_%s", htlcStatus.HashedSecret[:8]),
		HashedSecret:     htlcStatus.HashedSecret,
		Amount:           htlcStatus.Amount,
		Token:            htlcStatus.TokenAddress,
		Sender:           htlcStatus.Sender,
		Receiver:         htlcStatus.Recipient,
		TimeoutHeight:    htlcStatus.TimeoutHeight,
		TimeoutTimestamp: htlcStatus.TimeoutTimestamp,
		Status:           htlcStatus.Status,
		CreatedAt:        htlcStatus.CreatedAt,
		ChainID:          event.ChainID,
	}

	if err := o.db.CreateHTLC(htlc); err != nil {
		o.logger.Error("Failed to store HTLC in database", zap.Error(err))
		return err
	}

	return nil
}

func (o *Orchestrator) handleHTLCClaimed(event *adapters.ChainEvent) error {
	o.logger.Info("HTLC claimed event received",
		zap.String("chain_id", event.ChainID),
		zap.String("tx_hash", event.TxHash))

	// Update statistics
	o.stats.mutex.Lock()
	o.stats.SuccessfulSwaps++
	o.stats.mutex.Unlock()

	return nil
}

func (o *Orchestrator) handlePriceUpdate(event *adapters.ChainEvent) error {
	o.logger.Debug("Price update event received",
		zap.String("chain_id", event.ChainID))

	// Price updates are handled by the TWAP engine
	return nil
}

// checkOrderTimeouts checks for expired orders and handles them
func (o *Orchestrator) checkOrderTimeouts() error {
	orders, err := o.db.GetExecutableOrders()
	if err != nil {
		return fmt.Errorf("failed to get orders: %w", err)
	}

	currentTime := time.Now().Unix()
	
	for _, order := range orders {
		// Check if order has timed out
		if currentTime >= order.TimeoutTimestamp {
			o.logger.Info("Order timed out",
				zap.String("order_id", order.ID),
				zap.Int64("timeout_timestamp", order.TimeoutTimestamp))

			// Update order status to expired
			order.Status = string(database.OrderStatusExpired)
			order.UpdatedAt = time.Now()

			if err := o.db.UpdateOrder(order); err != nil {
				o.logger.Error("Failed to update expired order",
					zap.String("order_id", order.ID),
					zap.Error(err))
			}

			// Update statistics
			o.stats.mutex.Lock()
			o.stats.FailedOrders++
			o.stats.ActiveOrders--
			o.stats.mutex.Unlock()
		}
	}

	return nil
}

// updateStatistics updates orchestrator statistics
func (o *Orchestrator) updateStatistics() {
	o.stats.mutex.Lock()
	defer o.stats.mutex.Unlock()

	o.stats.UptimeSeconds = int64(time.Since(o.stats.startTime).Seconds())

	// Log current statistics
	o.logger.Debug("Statistics update",
		zap.Int64("total_orders", o.stats.TotalOrders),
		zap.Int64("active_orders", o.stats.ActiveOrders),
		zap.Int64("completed_orders", o.stats.CompletedOrders),
		zap.Int64("uptime_seconds", o.stats.UptimeSeconds))
}

// Public API methods

// GetStatistics returns current orchestrator statistics
func (o *Orchestrator) GetStatistics() *Statistics {
	o.stats.mutex.RLock()
	defer o.stats.mutex.RUnlock()

	// Return a copy to avoid race conditions
	return &Statistics{
		TotalOrders:        o.stats.TotalOrders,
		ActiveOrders:       o.stats.ActiveOrders,
		CompletedOrders:    o.stats.CompletedOrders,
		FailedOrders:       o.stats.FailedOrders,
		TotalVolume:        o.stats.TotalVolume,
		CrossChainSwaps:    o.stats.CrossChainSwaps,
		SuccessfulSwaps:    o.stats.SuccessfulSwaps,
		AverageProcessTime: o.stats.AverageProcessTime,
		LastProcessedOrder: o.stats.LastProcessedOrder,
		UptimeSeconds:      o.stats.UptimeSeconds,
	}
}

// GetAdapterManager returns the adapter manager
func (o *Orchestrator) GetAdapterManager() *adapters.Manager {
	return o.adapterManager
}

// GetTWAPEngine returns the TWAP engine
func (o *Orchestrator) GetTWAPEngine() *twap.Engine {
	return o.twapEngine
}

// HealthCheck performs a comprehensive health check
func (o *Orchestrator) HealthCheck() map[string]interface{} {
	health := make(map[string]interface{})

	// Check database health
	if err := o.db.Health(); err != nil {
		health["database"] = map[string]interface{}{
			"status": "unhealthy",
			"error":  err.Error(),
		}
	} else {
		health["database"] = map[string]interface{}{
			"status": "healthy",
		}
	}

	// Check adapter health
	adapterHealth := o.adapterManager.HealthCheckAll()
	adapters := make(map[string]interface{})
	for chainID, err := range adapterHealth {
		if err != nil {
			adapters[chainID] = map[string]interface{}{
				"status": "unhealthy",
				"error":  err.Error(),
			}
		} else {
			adapters[chainID] = map[string]interface{}{
				"status": "healthy",
			}
		}
	}
	health["adapters"] = adapters

	// Add statistics
	health["statistics"] = o.GetStatistics()

	// Add uptime
	health["uptime"] = time.Since(o.stats.startTime).String()

	return health
}

// ProcessOrder manually processes a specific order (for testing/debugging)
func (o *Orchestrator) ProcessOrder(orderID string) error {
	order, err := o.db.GetOrder(orderID)
	if err != nil {
		return fmt.Errorf("failed to get order: %w", err)
	}

	o.logger.Info("Manually processing order",
		zap.String("order_id", orderID),
		zap.String("status", order.Status))

	// Trigger TWAP execution
	response, err := o.twapEngine.ExecuteOrderManually(orderID)
	if err != nil {
		return fmt.Errorf("failed to execute order: %w", err)
	}

	if !response.Success {
		return fmt.Errorf("order execution failed: %s", response.Error.Error())
	}

	o.logger.Info("Order processed successfully",
		zap.String("order_id", orderID),
		zap.String("tx_hash", response.TxHash))

	return nil
}

// AddEventHandler adds a custom event handler
func (o *Orchestrator) AddEventHandler(eventType string, handler EventHandler) {
	o.mutex.Lock()
	defer o.mutex.Unlock()

	o.eventHandlers[eventType] = handler
}

// RemoveEventHandler removes an event handler
func (o *Orchestrator) RemoveEventHandler(eventType string) {
	o.mutex.Lock()
	defer o.mutex.Unlock()

	delete(o.eventHandlers, eventType)
}
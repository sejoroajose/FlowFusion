package adapters

import (
	"fmt"
	"sync"

	"go.uber.org/zap"

	"flowfusion/bridge-orchestrator/internal/config"
)

// ChainAdapter defines the interface that all chain adapters must implement
type ChainAdapter interface {
	// Chain identification
	ChainID() string
	Name() string

	// Connection management
	Connect() error
	Disconnect() error
	IsConnected() bool

	// Account management
	GetAddress() (string, error)
	GetBalance(tokenAddress string) (string, error)

	// TWAP operations
	CreateTWAPOrder(params CreateTWAPOrderParams) (string, error)
	ExecuteTWAPInterval(params ExecuteIntervalParams) (*ExecutionResult, error)
	CancelOrder(orderID string) error
	GetOrderStatus(orderID string) (*OrderStatus, error)

	// HTLC operations
	CreateHTLC(params CreateHTLCParams) (string, error)
	ClaimHTLC(htlcAddress, secret string) (string, error)
	RefundHTLC(htlcAddress string) (string, error)
	GetHTLCStatus(htlcAddress string) (*HTLCStatus, error)

	// Price operations
	GetCurrentPrice(tokenPair string) (string, error)
	GetTWAPPrice(tokenPair string, windowMinutes int) (string, error)

	// Event handling
	SubscribeToEvents(callback EventCallback) error
	UnsubscribeFromEvents() error

	// Health and status
	GetChainStatus() (*ChainStatus, error)
	Health() error
}

// Manager manages all chain adapters
type Manager struct {
	adapters map[string]ChainAdapter
	config   *config.Config
	logger   *zap.Logger
	mutex    sync.RWMutex
}

// NewManager creates a new adapter manager
func NewManager(cfg *config.Config, logger *zap.Logger) (*Manager, error) {
	manager := &Manager{
		adapters: make(map[string]ChainAdapter),
		config:   cfg,
		logger:   logger,
	}

	// Initialize adapters for supported chains
	for _, chainID := range cfg.SupportedChains {
		adapter, err := manager.createAdapter(chainID)
		if err != nil {
			logger.Error("Failed to create adapter", 
				zap.String("chain_id", chainID), 
				zap.Error(err))
			continue
		}

		manager.adapters[chainID] = adapter
		logger.Info("Adapter created", zap.String("chain_id", chainID))
	}

	return manager, nil
}

// createAdapter creates an adapter for the specified chain
func (m *Manager) createAdapter(chainID string) (ChainAdapter, error) {
	switch chainID {
	case "ethereum":
		return NewEthereumAdapter(m.config.EthereumConfig, m.logger)
	case "cosmos":
		return NewCosmosAdapter(m.config.CosmosConfig, m.logger)
	case "stellar":
		return NewStellarAdapter(m.config.StellarConfig, m.logger)
	case "bitcoin":
		if m.config.BitcoinConfig.Enabled {
			return NewBitcoinAdapter(m.config.BitcoinConfig, m.logger)
		}
		return nil, fmt.Errorf("bitcoin adapter is disabled")
	default:
		return nil, fmt.Errorf("unsupported chain: %s", chainID)
	}
}

// GetAdapter returns the adapter for the specified chain
func (m *Manager) GetAdapter(chainID string) (ChainAdapter, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	adapter, exists := m.adapters[chainID]
	if !exists {
		return nil, fmt.Errorf("adapter not found for chain: %s", chainID)
	}

	return adapter, nil
}

// GetAllAdapters returns all registered adapters
func (m *Manager) GetAllAdapters() map[string]ChainAdapter {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]ChainAdapter)
	for chainID, adapter := range m.adapters {
		result[chainID] = adapter
	}

	return result
}

// GetAdapterCount returns the number of registered adapters
func (m *Manager) GetAdapterCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return len(m.adapters)
}

// ConnectAll connects all adapters
func (m *Manager) ConnectAll() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var errors []error
	for chainID, adapter := range m.adapters {
		if err := adapter.Connect(); err != nil {
			m.logger.Error("Failed to connect adapter", 
				zap.String("chain_id", chainID), 
				zap.Error(err))
			errors = append(errors, fmt.Errorf("failed to connect %s: %w", chainID, err))
		} else {
			m.logger.Info("Adapter connected", zap.String("chain_id", chainID))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to connect %d adapters", len(errors))
	}

	return nil
}

// DisconnectAll disconnects all adapters
func (m *Manager) DisconnectAll() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var errors []error
	for chainID, adapter := range m.adapters {
		if err := adapter.Disconnect(); err != nil {
			m.logger.Error("Failed to disconnect adapter", 
				zap.String("chain_id", chainID), 
				zap.Error(err))
			errors = append(errors, fmt.Errorf("failed to disconnect %s: %w", chainID, err))
		} else {
			m.logger.Info("Adapter disconnected", zap.String("chain_id", chainID))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to disconnect %d adapters", len(errors))
	}

	return nil
}

// HealthCheckAll performs health checks on all adapters
func (m *Manager) HealthCheckAll() map[string]error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	results := make(map[string]error)
	for chainID, adapter := range m.adapters {
		if err := adapter.Health(); err != nil {
			results[chainID] = err
		} else {
			results[chainID] = nil
		}
	}

	return results
}

// GetChainStatuses returns the status of all chains
func (m *Manager) GetChainStatuses() map[string]*ChainStatus {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	results := make(map[string]*ChainStatus)
	for chainID, adapter := range m.adapters {
		if status, err := adapter.GetChainStatus(); err != nil {
			m.logger.Error("Failed to get chain status", 
				zap.String("chain_id", chainID), 
				zap.Error(err))
			results[chainID] = &ChainStatus{
				ChainID:     chainID,
				IsHealthy:   false,
				ErrorMessage: err.Error(),
			}
		} else {
			results[chainID] = status
		}
	}

	return results
}

// AddAdapter dynamically adds a new adapter
func (m *Manager) AddAdapter(chainID string, adapter ChainAdapter) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.adapters[chainID]; exists {
		return fmt.Errorf("adapter already exists for chain: %s", chainID)
	}

	m.adapters[chainID] = adapter
	m.logger.Info("Adapter added", zap.String("chain_id", chainID))

	return nil
}

// RemoveAdapter removes an adapter
func (m *Manager) RemoveAdapter(chainID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	adapter, exists := m.adapters[chainID]
	if !exists {
		return fmt.Errorf("adapter not found for chain: %s", chainID)
	}

	// Disconnect before removing
	if err := adapter.Disconnect(); err != nil {
		m.logger.Warn("Failed to disconnect adapter before removal", 
			zap.String("chain_id", chainID), 
			zap.Error(err))
	}

	delete(m.adapters, chainID)
	m.logger.Info("Adapter removed", zap.String("chain_id", chainID))

	return nil
}

// IsChainSupported checks if a chain is supported
func (m *Manager) IsChainSupported(chainID string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	_, exists := m.adapters[chainID]
	return exists
}

// GetSupportedChains returns a list of supported chain IDs
func (m *Manager) GetSupportedChains() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	chains := make([]string, 0, len(m.adapters))
	for chainID := range m.adapters {
		chains = append(chains, chainID)
	}

	return chains
}

// ExecuteCrossChainSwap coordinates a cross-chain swap between two adapters
func (m *Manager) ExecuteCrossChainSwap(params CrossChainSwapParams) (*CrossChainSwapResult, error) {
	sourceAdapter, err := m.GetAdapter(params.SourceChain)
	if err != nil {
		return nil, fmt.Errorf("source adapter not found: %w", err)
	}

	targetAdapter, err := m.GetAdapter(params.TargetChain)
	if err != nil {
		return nil, fmt.Errorf("target adapter not found: %w", err)
	}

	// Create HTLC on source chain
	htlcParams := CreateHTLCParams{
		HashedSecret:     params.HashedSecret,
		Amount:           params.Amount,
		TokenAddress:     params.SourceToken,
		Recipient:        params.TargetRecipient,
		TimeoutHeight:    params.TimeoutHeight,
		TimeoutTimestamp: params.TimeoutTimestamp,
	}

	sourceHTLC, err := sourceAdapter.CreateHTLC(htlcParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create source HTLC: %w", err)
	}

	// Create corresponding HTLC on target chain
	targetHTLCParams := CreateHTLCParams{
		HashedSecret:     params.HashedSecret,
		Amount:           params.TargetAmount,
		TokenAddress:     params.TargetToken,
		Recipient:        params.SourceUser,
		TimeoutHeight:    params.TimeoutHeight - 100, // Shorter timeout for target
		TimeoutTimestamp: params.TimeoutTimestamp - 3600, // 1 hour shorter
	}

	targetHTLC, err := targetAdapter.CreateHTLC(targetHTLCParams)
	if err != nil {
		// If target HTLC creation fails, we should handle source HTLC refund
		m.logger.Error("Failed to create target HTLC", 
			zap.String("source_htlc", sourceHTLC),
			zap.Error(err))
		return nil, fmt.Errorf("failed to create target HTLC: %w", err)
	}

	return &CrossChainSwapResult{
		SourceHTLC:    sourceHTLC,
		TargetHTLC:    targetHTLC,
		SourceChain:   params.SourceChain,
		TargetChain:   params.TargetChain,
		Status:        "htlcs_created",
		CreatedAt:     "2024-01-01T00:00:00Z", // Use proper timestamp
	}, nil
}
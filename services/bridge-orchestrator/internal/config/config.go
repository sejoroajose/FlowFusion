package config

import (
	"os"
	"strconv"
	"strings"
	"time"
	"fmt"
)

// Config holds all configuration for the bridge orchestrator
type Config struct {
	// Server configuration
	Environment string
	Port        int
	LogLevel    string

	// Database configuration
	DatabaseURL string
	RedisURL    string

	// Ethereum configuration
	EthereumConfig EthereumConfig

	// Cosmos configuration
	CosmosConfig CosmosConfig

	// Stellar configuration
	StellarConfig StellarConfig

	// Bitcoin configuration (optional)
	BitcoinConfig BitcoinConfig

	// TWAP configuration
	TWAPConfig TWAPConfig

	// API Keys
	APIKeys APIKeys

	// Supported chains
	SupportedChains []string

	// Security
	JWTSecret string
}

type EthereumConfig struct {
	Network        string
	RPCURL         string
	PrivateKey     string
	BridgeAddress  string
	ChainID        int64
	GasLimit       uint64
	GasPrice       int64 // in Gwei
	ConfirmBlocks  int
}

type CosmosConfig struct {
	ChainID        string
	RPCURL         string
	RestURL        string
	Mnemonic       string
	BridgeAddress  string
	GasLimit       uint64
	GasPrices      string
	IBCChannelID   string
	IBCPortID      string
}

type StellarConfig struct {
	Network       string
	HorizonURL    string
	SecretKey     string
	BridgeAddress string
}

type BitcoinConfig struct {
	Network     string
	RPCURL      string
	PrivateKey  string
	Enabled     bool
}

type TWAPConfig struct {
	UpdateInterval        time.Duration
	WindowMinutes         int
	MaxWindowMinutes      int
	MinExecutionInterval  time.Duration
	MaxExecutionInterval  time.Duration
	MaxSlippage           int // basis points
	DefaultSlippage       int // basis points
	PriceUpdateInterval   time.Duration
	MinLiquidity          string
}

type APIKeys struct {
	InfuraAPIKey      string
	AlchemyAPIKey     string
	OneInchAPIKey     string
	CoingeckoAPIKey   string
	ChainlinkAPIKey   string
	PythAPIKey        string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		Environment:     getEnv("NODE_ENV", "development"),
		Port:           getEnvAsInt("BRIDGE_SERVICE_PORT", 8080),
		LogLevel:       getEnv("LOG_LEVEL", "info"),
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://flowfusion:password@localhost:5432/flowfusion_dev"),
		RedisURL:       getEnv("REDIS_URL", "redis://localhost:6379"),
		SupportedChains: getEnvAsSlice("SUPPORTED_CHAINS", []string{"ethereum", "cosmos", "stellar"}),
		JWTSecret:      getEnv("JWT_SECRET", "your-secret-key"),
	}

	// Load chain configurations
	cfg.EthereumConfig = EthereumConfig{
		Network:       getEnv("ETHEREUM_NETWORK", "sepolia"),
		RPCURL:        getEnv("ETHEREUM_RPC_URL", ""),
		PrivateKey:    getEnv("ETHEREUM_PRIVATE_KEY", ""),
		BridgeAddress: getEnv("ETHEREUM_BRIDGE_ADDRESS", ""),
		ChainID:       getEnvAsInt64("ETHEREUM_CHAIN_ID", 11155111), // Sepolia
		GasLimit:      getEnvAsUint64("ETHEREUM_GAS_LIMIT", 300000),
		GasPrice:      getEnvAsInt64("ETHEREUM_GAS_PRICE", 20), // 20 Gwei
		ConfirmBlocks: getEnvAsInt("ETHEREUM_CONFIRM_BLOCKS", 1),
	}

	cfg.CosmosConfig = CosmosConfig{
		ChainID:       getEnv("COSMOS_CHAIN_ID", "theta-testnet-001"),
		RPCURL:        getEnv("COSMOS_RPC_URL", ""),
		RestURL:       getEnv("COSMOS_REST_URL", ""),
		Mnemonic:      getEnv("COSMOS_MNEMONIC", ""),
		BridgeAddress: getEnv("COSMOS_BRIDGE_ADDRESS", ""),
		GasLimit:      getEnvAsUint64("COSMOS_GAS_LIMIT", 200000),
		GasPrices:     getEnv("COSMOS_GAS_PRICES", "0.025uatom"),
		IBCChannelID:  getEnv("IBC_CHANNEL_ID", "channel-0"),
		IBCPortID:     getEnv("IBC_PORT_ID", "transfer"),
	}

	cfg.StellarConfig = StellarConfig{
		Network:       getEnv("STELLAR_NETWORK", "testnet"),
		HorizonURL:    getEnv("STELLAR_HORIZON_URL", "https://horizon-testnet.stellar.org"),
		SecretKey:     getEnv("STELLAR_SECRET_KEY", ""),
		BridgeAddress: getEnv("STELLAR_BRIDGE_ADDRESS", ""),
	}

	cfg.BitcoinConfig = BitcoinConfig{
		Network:    getEnv("BITCOIN_NETWORK", "testnet"),
		RPCURL:     getEnv("BITCOIN_RPC_URL", ""),
		PrivateKey: getEnv("BITCOIN_PRIVATE_KEY", ""),
		Enabled:    getEnvAsBool("ENABLE_BITCOIN", false),
	}

	cfg.TWAPConfig = TWAPConfig{
		UpdateInterval:       getEnvAsDuration("TWAP_UPDATE_INTERVAL", 30*time.Second),
		WindowMinutes:        getEnvAsInt("TWAP_WINDOW_MIN", 5),
		MaxWindowMinutes:     getEnvAsInt("TWAP_WINDOW_MAX", 1440),
		MinExecutionInterval: getEnvAsDuration("MIN_EXECUTION_INTERVAL", 60*time.Second),
		MaxExecutionInterval: getEnvAsDuration("MAX_EXECUTION_INTERVAL", 3600*time.Second),
		MaxSlippage:          getEnvAsInt("TWAP_MAX_SLIPPAGE", 500), // 5%
		DefaultSlippage:      getEnvAsInt("TWAP_DEFAULT_SLIPPAGE", 100), // 1%
		PriceUpdateInterval:  getEnvAsDuration("PRICE_UPDATE_INTERVAL", 10*time.Second),
		MinLiquidity:         getEnv("TWAP_MIN_LIQUIDITY", "10000"),
	}

	cfg.APIKeys = APIKeys{
		InfuraAPIKey:    getEnv("INFURA_API_KEY", ""),
		AlchemyAPIKey:   getEnv("ALCHEMY_API_KEY", ""),
		OneInchAPIKey:   getEnv("ONE_INCH_API_KEY", ""),
		CoingeckoAPIKey: getEnv("COINGECKO_API_KEY", ""),
		ChainlinkAPIKey: getEnv("CHAINLINK_API_KEY", ""),
		PythAPIKey:      getEnv("PYTH_API_KEY", ""),
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Check required fields based on supported chains
	for _, chain := range c.SupportedChains {
		switch chain {
		case "ethereum":
			if c.EthereumConfig.RPCURL == "" {
				return ErrMissingEthereumRPC
			}
			if c.EthereumConfig.PrivateKey == "" {
				return ErrMissingEthereumPrivateKey
			}
		case "cosmos":
			if c.CosmosConfig.RPCURL == "" {
				return ErrMissingCosmosRPC
			}
			if c.CosmosConfig.Mnemonic == "" {
				return ErrMissingCosmosMnemonic
			}
		case "stellar":
			if c.StellarConfig.HorizonURL == "" {
				return ErrMissingStellarHorizon
			}
			if c.StellarConfig.SecretKey == "" {
				return ErrMissingStellarSecretKey
			}
		}
	}

	// Validate TWAP config
	if c.TWAPConfig.WindowMinutes < 5 || c.TWAPConfig.WindowMinutes > c.TWAPConfig.MaxWindowMinutes {
		return ErrInvalidTWAPWindow
	}

	if c.TWAPConfig.MaxSlippage < 1 || c.TWAPConfig.MaxSlippage > 1000 {
		return ErrInvalidSlippage
	}

	return nil
}

// IsChainSupported checks if a chain is supported
func (c *Config) IsChainSupported(chainID string) bool {
	for _, chain := range c.SupportedChains {
		if chain == chainID {
			return true
		}
	}
	return false
}

// GetChainConfig returns configuration for a specific chain
func (c *Config) GetChainConfig(chainID string) interface{} {
	switch chainID {
	case "ethereum":
		return c.EthereumConfig
	case "cosmos":
		return c.CosmosConfig
	case "stellar":
		return c.StellarConfig
	case "bitcoin":
		return c.BitcoinConfig
	default:
		return nil
	}
}

// Helper functions for environment variable parsing
func getEnv(key, defaultVal string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvAsInt64(key string, defaultVal int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvAsUint64(key string, defaultVal uint64) uint64 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseUint(value, 10, 64); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvAsBool(key string, defaultVal bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultVal
}

func getEnvAsDuration(key string, defaultVal time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultVal
}

func getEnvAsSlice(key string, defaultVal []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultVal
}
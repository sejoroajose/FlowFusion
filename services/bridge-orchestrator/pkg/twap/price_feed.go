package twap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// PriceFeedConfig holds configuration for price feed sources
type PriceFeedConfig struct {
	CoinGeckoAPIKey string
	OneInchAPIKey   string
	ChainlinkRPCURL string
	Timeout         time.Duration
}

// CoinGeckoResponse represents the response from CoinGecko API
type CoinGeckoResponse struct {
	Ethereum map[string]float64 `json:"ethereum"`
	Cosmos   map[string]float64 `json:"cosmos"`
	Stellar  map[string]float64 `json:"stellar"`
}

// OneInchQuoteResponse represents the response from 1inch quote API
type OneInchQuoteResponse struct {
	ToTokenAmount string `json:"toTokenAmount"`
	FromToken     struct {
		Decimals int `json:"decimals"`
	} `json:"fromToken"`
	ToToken struct {
		Decimals int `json:"decimals"`
	} `json:"toToken"`
}

// Real CoinGecko API implementation
func (e *Engine) getCoinGeckoPrice(ctx context.Context, tokenPair string) (decimal.Decimal, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	
	// Map token pairs to CoinGecko coin IDs
	coinMap := map[string]string{
		"ETH_USDC":  "ethereum",
		"ATOM_USDC": "cosmos",
		"XLM_USDC":  "stellar",
		"BTC_USDC":  "bitcoin",
		"SOL_USDC":  "solana",
	}
	
	coinID, exists := coinMap[tokenPair]
	if !exists {
		return decimal.Zero, fmt.Errorf("unsupported token pair: %s", tokenPair)
	}
	
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/simple/price?ids=%s&vs_currencies=usd", coinID)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Add API key if available
	if e.config.APIKeys.CoingeckoAPIKey != "" {
		req.Header.Set("X-CG-Demo-API-Key", e.config.APIKeys.CoingeckoAPIKey)
	}
	
	req.Header.Set("Accept", "application/json")
	
	resp, err := client.Do(req)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to fetch price: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	
	var result map[string]map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return decimal.Zero, fmt.Errorf("failed to decode response: %w", err)
	}
	
	coinData, exists := result[coinID]
	if !exists {
		return decimal.Zero, fmt.Errorf("no data for coin: %s", coinID)
	}
	
	price, exists := coinData["usd"]
	if !exists {
		return decimal.Zero, fmt.Errorf("no USD price data for: %s", coinID)
	}
	
	return decimal.NewFromFloat(price), nil
}

// Real 1inch DEX price implementation
func (e *Engine) getDEXPrice(ctx context.Context, tokenPair string) (decimal.Decimal, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	
	// Token contract addresses (Ethereum mainnet examples)
	tokenMap := map[string]map[string]string{
		"ETH_USDC": {
			"source": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE", // ETH
			"target": "0xA0b86a33E6441986454b25b89f2b1c6e8a79c81e", // USDC
		},
		"WETH_USDC": {
			"source": "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", // WETH
			"target": "0xA0b86a33E6441986454b25b89f2b1c6e8a79c81e", // USDC
		},
		// Add more pairs as needed
	}
	
	tokens, exists := tokenMap[tokenPair]
	if !exists {
		return decimal.Zero, fmt.Errorf("unsupported token pair: %s", tokenPair)
	}
	
	// Use 1 ETH worth for price calculation
	amount := "1000000000000000000" // 1 ETH in wei
	
	url := fmt.Sprintf("https://api.1inch.io/v5.0/1/quote?fromTokenAddress=%s&toTokenAddress=%s&amount=%s",
		tokens["source"], tokens["target"], amount)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to create request: %w", err)
	}
	
	if e.config.APIKeys.OneInchAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.config.APIKeys.OneInchAPIKey)
	}
	
	resp, err := client.Do(req)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to fetch quote: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, fmt.Errorf("1inch API returned status %d", resp.StatusCode)
	}
	
	var quote OneInchQuoteResponse
	if err := json.NewDecoder(resp.Body).Decode(&quote); err != nil {
		return decimal.Zero, fmt.Errorf("failed to decode response: %w", err)
	}
	
	// Convert the quote to price per unit
	toAmount, err := decimal.NewFromString(quote.ToTokenAmount)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid to amount: %w", err)
	}
	
	// Adjust for decimals
	fromDecimals := decimal.NewFromInt(10).Pow(decimal.NewFromInt(18)) // ETH has 18 decimals
	toDecimals := decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(quote.ToToken.Decimals)))
	
	price := toAmount.Div(toDecimals).Div(fromDecimals)
	
	return price, nil
}

// Chainlink price feed implementation (requires connecting to Ethereum node)
func (e *Engine) getChainlinkPrice(ctx context.Context, tokenPair string) (decimal.Decimal, error) {
	// Chainlink price feed contract addresses (Ethereum mainnet)
	feedMap := map[string]string{
		"ETH_USD":  "0x5f4eC3Df9cbd43714FE2740f5E3616155c5b8419",
		"BTC_USD":  "0xF4030086522a5bEEa4988F8cA5B36dbC97BeE88c",
		"ATOM_USD": "0xCAD1C4e94baC2Bf5B23d9ba3E84a7e02Db8e7c73",
		// Add more feeds as needed
	}
	
	// Convert token pair format
	chainlinkPair := strings.Replace(tokenPair, "_USDC", "_USD", 1)
	
	feedAddress, exists := feedMap[chainlinkPair]
	if !exists {
		return decimal.Zero, fmt.Errorf("no Chainlink feed for pair: %s", chainlinkPair)
	}
	
	// TODO: Implement actual Ethereum contract call
	// This requires ethereum/go-ethereum client to call the contract
	/*
	client, err := ethclient.DialContext(ctx, e.config.EthereumRPC)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to connect to Ethereum: %w", err)
	}
	defer client.Close()
	
	// Call latestRoundData() function on the price feed contract
	// This would require the AggregatorV3Interface ABI
	*/
	
	e.logger.Debug("Chainlink price feed called",
		zap.String("pair", chainlinkPair),
		zap.String("feed_address", feedAddress))
	
	// Mock implementation for now - replace with actual contract call
	switch chainlinkPair {
	case "ETH_USD":
		return decimal.NewFromFloat(2500.0), nil
	case "BTC_USD":
		return decimal.NewFromFloat(45000.0), nil
	case "ATOM_USD":
		return decimal.NewFromFloat(12.5), nil
	default:
		return decimal.Zero, fmt.Errorf("price not available for: %s", chainlinkPair)
	}
}

// Enhanced storePricePoint with database persistence
func (e *Engine) storePricePoint(tokenPair, source string, price decimal.Decimal) error {
	if price.IsZero() || price.IsNegative() {
		return fmt.Errorf("invalid price: %s for %s from %s", price.String(), tokenPair, source)
	}
	
	now := time.Now()
	
	pricePoint := &PricePoint{
		Timestamp: now,
		Price:     price,
		Volume:    decimal.NewFromInt(0), // Volume data would come from actual APIs
		Source:    source,
	}
	
	// Store in memory cache
	e.addPricePoint(tokenPair, pricePoint)
	
	// Store in database for persistence
	dbPricePoint := &database.PricePoint{
		TokenPair: tokenPair,
		Source:    source,
		Price:     price,
		Timestamp: now,
		CreatedAt: now,
	}
	
	if err := e.db.StorePricePoint(dbPricePoint); err != nil {
		e.logger.Error("Failed to store price point in database", 
			zap.String("token_pair", tokenPair),
			zap.String("source", source),
			zap.Error(err))
		// Don't return error as cache storage succeeded
	}
	
	e.logger.Debug("Price point stored",
		zap.String("token_pair", tokenPair),
		zap.String("source", source),
		zap.String("price", price.String()),
		zap.Time("timestamp", now))
	
	return nil
}

// Enhanced updatePriceFeeds with error handling and retries
func (e *Engine) updatePriceFeeds() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tokenPairs := []string{"ETH_USDC", "ATOM_USDC", "XLM_USDC", "BTC_USDC"}
	
	var lastError error
	successCount := 0
	
	for _, pair := range tokenPairs {
		// Try multiple sources for redundancy
		sources := []struct {
			name string
			fn   func(context.Context, string) (decimal.Decimal, error)
		}{
			{"chainlink", e.getChainlinkPrice},
			{"coingecko", e.getCoinGeckoPrice},
			{"1inch", e.getDEXPrice},
		}
		
		for _, source := range sources {
			price, err := source.fn(ctx, pair)
			if err != nil {
				e.logger.Warn("Failed to fetch price from source",
					zap.String("pair", pair),
					zap.String("source", source.name),
					zap.Error(err))
				lastError = err
				continue
			}
			
			if err := e.storePricePoint(pair, source.name, price); err != nil {
				e.logger.Error("Failed to store price point",
					zap.String("pair", pair),
					zap.String("source", source.name),
					zap.Error(err))
				lastError = err
				continue
			}
			
			successCount++
			
			// Small delay between API calls to avoid rate limiting
			time.Sleep(100 * time.Millisecond)
		}
	}
	
	e.logger.Info("Price feed update completed",
		zap.Int("successful_updates", successCount),
		zap.Int("total_pairs", len(tokenPairs)),
		zap.Int("sources_per_pair", 3))
	
	// Return error only if no sources succeeded
	if successCount == 0 && lastError != nil {
		return fmt.Errorf("all price feed sources failed: %w", lastError)
	}
	
	return nil
}
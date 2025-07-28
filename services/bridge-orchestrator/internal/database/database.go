package database

import (
	"database/sql"
	"fmt"
	"time"
	"context"

	_ "github.com/lib/pq" 
	"go.uber.org/zap"
)

// DB interface for database operations
type DB interface {
	// Order operations
	CreateOrder(order *Order) error
	GetOrder(orderID string) (*Order, error)
	GetOrdersByUser(userAddress string, limit, offset int) ([]*Order, error)
	UpdateOrder(order *Order) error
	GetExecutableOrders() ([]*Order, error)

	// Execution history operations
	CreateExecutionRecord(record *ExecutionRecord) error
	GetExecutionHistory(orderID string) ([]*ExecutionRecord, error)

	// Price history operations
	CreatePricePoint(point *PricePoint) error
	GetPriceHistory(tokenPair string, windowMinutes int) ([]*PricePoint, error)
	UpdatePriceHistory(tokenPair string, points []*PricePoint) error

	// Price point methods
    StorePricePoint(point *PricePoint) error
    GetPricePoints(tokenPair string, since time.Time) ([]*PricePoint, error)
    GetLatestPrice(tokenPair, source string) (*PricePoint, error)
    CleanupOldPricePoints(olderThan time.Time) error

	// HTLC operations
	CreateHTLC(htlc *HTLC) error
	GetHTLC(htlcAddress string) (*HTLC, error)
	UpdateHTLC(htlc *HTLC) error

	// Chain operations
	GetSupportedChains() ([]string, error)
	GetChainStatus(chainID string) (*ChainStatus, error)

	// Health check
	Health() error
	Close() error
}

// PostgreSQLDB implements the DB interface using PostgreSQL
type PostgreSQLDB struct {
	db     *sql.DB
	logger *zap.Logger
}

func Initialize(databaseURL string) (DB, error) {
    db, err := sql.Open("postgres", databaseURL)
    if err != nil {
        return nil, err
    }

    // Production connection pool settings
    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(5)
    db.SetConnMaxLifetime(5 * time.Minute)
    db.SetConnMaxIdleTime(2 * time.Minute)

    // Test connection
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    if err := db.PingContext(ctx); err != nil {
        return nil, fmt.Errorf("failed to ping database: %w", err)
    }

    return &PostgreSQLDB{db: db}, nil
}


// initSchema creates the necessary database tables
func (db *PostgreSQLDB) initSchema() error {
	schema := `
		-- Orders table
		CREATE TABLE IF NOT EXISTS orders (
			id VARCHAR(66) PRIMARY KEY,
			user_address VARCHAR(42) NOT NULL,
			source_chain VARCHAR(20) NOT NULL,
			target_chain VARCHAR(20) NOT NULL,
			source_token VARCHAR(42) NOT NULL,
			source_amount DECIMAL(78, 0) NOT NULL,
			target_token VARCHAR(50) NOT NULL,
			target_recipient TEXT NOT NULL,
			min_received DECIMAL(78, 0) NOT NULL,
			window_minutes INTEGER NOT NULL,
			execution_intervals INTEGER NOT NULL,
			max_slippage INTEGER NOT NULL,
			min_fill_size DECIMAL(78, 0) NOT NULL,
			enable_mev_protection BOOLEAN NOT NULL DEFAULT true,
			htlc_hash VARCHAR(66) NOT NULL,
			timeout_height BIGINT NOT NULL,
			timeout_timestamp BIGINT NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			executed_amount DECIMAL(78, 0) DEFAULT 0,
			last_execution TIMESTAMP WITH TIME ZONE,
			status VARCHAR(20) DEFAULT 'pending',
			average_price DECIMAL(78, 18) DEFAULT 0,
			metadata JSONB
		);

		-- Execution history table
		CREATE TABLE IF NOT EXISTS execution_history (
			id SERIAL PRIMARY KEY,
			order_id VARCHAR(66) NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
			interval_number INTEGER NOT NULL,
			timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			amount DECIMAL(78, 0) NOT NULL,
			price DECIMAL(78, 18) NOT NULL,
			gas_used BIGINT,
			slippage INTEGER,
			tx_hash VARCHAR(66),
			chain_id VARCHAR(20)
		);

		-- Price history table
		CREATE TABLE IF NOT EXISTS price_history (
			id SERIAL PRIMARY KEY,
			token_pair VARCHAR(100) NOT NULL,
			timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			price DECIMAL(78, 18) NOT NULL,
			volume DECIMAL(78, 0),
			source VARCHAR(50) NOT NULL,
			chain_id VARCHAR(20)
		);

		-- HTLC table
		CREATE TABLE IF NOT EXISTS htlcs (
			address VARCHAR(100) PRIMARY KEY,
			order_id VARCHAR(66) NOT NULL REFERENCES orders(id),
			hashed_secret VARCHAR(66) NOT NULL,
			amount DECIMAL(78, 0) NOT NULL,
			token VARCHAR(42) NOT NULL,
			sender VARCHAR(42) NOT NULL,
			receiver VARCHAR(42) NOT NULL,
			timeout_height BIGINT NOT NULL,
			timeout_timestamp BIGINT NOT NULL,
			status VARCHAR(20) DEFAULT 'active',
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			claimed_at TIMESTAMP WITH TIME ZONE,
			secret VARCHAR(66),
			chain_id VARCHAR(20) NOT NULL
		);

		-- Chain status table
		CREATE TABLE IF NOT EXISTS chain_status (
			chain_id VARCHAR(20) PRIMARY KEY,
			name VARCHAR(50) NOT NULL,
			enabled BOOLEAN DEFAULT true,
			last_block_height BIGINT,
			last_block_time TIMESTAMP WITH TIME ZONE,
			avg_block_time INTERVAL,
			gas_price DECIMAL(78, 0),
			health_status VARCHAR(20) DEFAULT 'unknown',
			last_health_check TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);

		-- Indexes for performance
		CREATE INDEX IF NOT EXISTS idx_orders_user_address ON orders(user_address);
		CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
		CREATE INDEX IF NOT EXISTS idx_orders_source_chain ON orders(source_chain);
		CREATE INDEX IF NOT EXISTS idx_orders_target_chain ON orders(target_chain);
		CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at);
		CREATE INDEX IF NOT EXISTS idx_orders_last_execution ON orders(last_execution);

		CREATE INDEX IF NOT EXISTS idx_execution_history_order_id ON execution_history(order_id);
		CREATE INDEX IF NOT EXISTS idx_execution_history_timestamp ON execution_history(timestamp);

		CREATE INDEX IF NOT EXISTS idx_price_history_token_pair ON price_history(token_pair);
		CREATE INDEX IF NOT EXISTS idx_price_history_timestamp ON price_history(timestamp);
		CREATE INDEX IF NOT EXISTS idx_price_history_composite ON price_history(token_pair, timestamp);

		CREATE INDEX IF NOT EXISTS idx_htlcs_order_id ON htlcs(order_id);
		CREATE INDEX IF NOT EXISTS idx_htlcs_status ON htlcs(status);
		CREATE INDEX IF NOT EXISTS idx_htlcs_chain_id ON htlcs(chain_id);

		-- Insert default chain status
		INSERT INTO chain_status (chain_id, name, enabled) 
		VALUES 
			('ethereum', 'Ethereum', true),
			('cosmos', 'Cosmos', true),
			('stellar', 'Stellar', true),
			('bitcoin', 'Bitcoin', false)
		ON CONFLICT (chain_id) DO NOTHING;
	`

	_, err := db.db.Exec(schema)
	return err
}

// Order operations
func (db *PostgreSQLDB) CreateOrder(order *Order) error {
	query := `
		INSERT INTO orders (
			id, user_address, source_chain, target_chain, source_token, 
			source_amount, target_token, target_recipient, min_received,
			window_minutes, execution_intervals, max_slippage, min_fill_size,
			enable_mev_protection, htlc_hash, timeout_height, timeout_timestamp,
			status, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	`

	_, err := db.db.Exec(
		query,
		order.ID, order.UserAddress, order.SourceChain, order.TargetChain,
		order.SourceToken, order.SourceAmount, order.TargetToken,
		order.TargetRecipient, order.MinReceived, order.WindowMinutes,
		order.ExecutionIntervals, order.MaxSlippage, order.MinFillSize,
		order.EnableMEVProtection, order.HTLCHash, order.TimeoutHeight,
		order.TimeoutTimestamp, order.Status, order.Metadata,
	)

	if err != nil {
		db.logger.Error("Failed to create order", zap.Error(err), zap.String("order_id", order.ID))
		return err
	}

	db.logger.Info("Order created", zap.String("order_id", order.ID))
	return nil
}

func (db *PostgreSQLDB) GetOrder(orderID string) (*Order, error) {
	query := `
		SELECT id, user_address, source_chain, target_chain, source_token,
			   source_amount, target_token, target_recipient, min_received,
			   window_minutes, execution_intervals, max_slippage, min_fill_size,
			   enable_mev_protection, htlc_hash, timeout_height, timeout_timestamp,
			   created_at, updated_at, executed_amount, last_execution,
			   status, average_price, metadata
		FROM orders WHERE id = $1
	`

	row := db.db.QueryRow(query, orderID)
	
	order := &Order{}
	err := row.Scan(
		&order.ID, &order.UserAddress, &order.SourceChain, &order.TargetChain,
		&order.SourceToken, &order.SourceAmount, &order.TargetToken,
		&order.TargetRecipient, &order.MinReceived, &order.WindowMinutes,
		&order.ExecutionIntervals, &order.MaxSlippage, &order.MinFillSize,
		&order.EnableMEVProtection, &order.HTLCHash, &order.TimeoutHeight,
		&order.TimeoutTimestamp, &order.CreatedAt, &order.UpdatedAt,
		&order.ExecutedAmount, &order.LastExecution, &order.Status,
		&order.AveragePrice, &order.Metadata,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}

	return order, nil
}

func (db *PostgreSQLDB) GetOrdersByUser(userAddress string, limit, offset int) ([]*Order, error) {
	query := `
		SELECT id, user_address, source_chain, target_chain, source_token,
			   source_amount, target_token, target_recipient, min_received,
			   window_minutes, execution_intervals, max_slippage, min_fill_size,
			   enable_mev_protection, htlc_hash, timeout_height, timeout_timestamp,
			   created_at, updated_at, executed_amount, last_execution,
			   status, average_price, metadata
		FROM orders 
		WHERE user_address = $1 
		ORDER BY created_at DESC 
		LIMIT $2 OFFSET $3
	`

	rows, err := db.db.Query(query, userAddress, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []*Order
	for rows.Next() {
		order := &Order{}
		err := rows.Scan(
			&order.ID, &order.UserAddress, &order.SourceChain, &order.TargetChain,
			&order.SourceToken, &order.SourceAmount, &order.TargetToken,
			&order.TargetRecipient, &order.MinReceived, &order.WindowMinutes,
			&order.ExecutionIntervals, &order.MaxSlippage, &order.MinFillSize,
			&order.EnableMEVProtection, &order.HTLCHash, &order.TimeoutHeight,
			&order.TimeoutTimestamp, &order.CreatedAt, &order.UpdatedAt,
			&order.ExecutedAmount, &order.LastExecution, &order.Status,
			&order.AveragePrice, &order.Metadata,
		)
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}

	return orders, nil
}

func (db *PostgreSQLDB) UpdateOrder(order *Order) error {
    query := `
        UPDATE orders SET
            executed_amount = $2,
            last_execution = $3,
            status = $4,
            average_price = $5,
            updated_at = NOW()
        WHERE id = $1
    `

    result, err := db.db.Exec(
        query,
        order.ID, 
        order.ExecutedAmount,
        order.LastExecution,
        order.Status,
        order.AveragePrice,
    )
    
    if err != nil {
        return err
    }

    rowsAffected, err := result.RowsAffected()
    if err != nil {
        return err
    }

    if rowsAffected == 0 {
        return ErrOrderNotFound
    }

    return nil
}

func (db *PostgreSQLDB) GetExecutableOrders() ([]*Order, error) {
	query := `
		SELECT id, user_address, source_chain, target_chain, source_token,
			   source_amount, target_token, target_recipient, min_received,
			   window_minutes, execution_intervals, max_slippage, min_fill_size,
			   enable_mev_protection, htlc_hash, timeout_height, timeout_timestamp,
			   created_at, updated_at, executed_amount, last_execution,
			   status, average_price, metadata
		FROM orders 
		WHERE status IN ('pending', 'executing')
		AND timeout_height > $1
		AND (
			last_execution IS NULL 
			OR last_execution + INTERVAL '1 minute' * (window_minutes / execution_intervals) <= NOW()
		)
		ORDER BY created_at ASC
	`

	// Use a mock current block height for now
	currentHeight := int64(1000000)

	rows, err := db.db.Query(query, currentHeight)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []*Order
	for rows.Next() {
		order := &Order{}
		err := rows.Scan(
			&order.ID, &order.UserAddress, &order.SourceChain, &order.TargetChain,
			&order.SourceToken, &order.SourceAmount, &order.TargetToken,
			&order.TargetRecipient, &order.MinReceived, &order.WindowMinutes,
			&order.ExecutionIntervals, &order.MaxSlippage, &order.MinFillSize,
			&order.EnableMEVProtection, &order.HTLCHash, &order.TimeoutHeight,
			&order.TimeoutTimestamp, &order.CreatedAt, &order.UpdatedAt,
			&order.ExecutedAmount, &order.LastExecution, &order.Status,
			&order.AveragePrice, &order.Metadata,
		)
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}

	return orders, nil
}

// Execution history operations
func (db *PostgreSQLDB) CreateExecutionRecord(record *ExecutionRecord) error {
	query := `
		INSERT INTO execution_history (
			order_id, interval_number, timestamp, amount, price,
			gas_used, slippage, tx_hash, chain_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := db.db.Exec(
		query,
		record.OrderID, record.IntervalNumber, record.Timestamp,
		record.Amount, record.Price, record.GasUsed, record.Slippage,
		record.TxHash, record.ChainID,
	)

	return err
}

func (db *PostgreSQLDB) GetExecutionHistory(orderID string) ([]*ExecutionRecord, error) {
	query := `
		SELECT id, order_id, interval_number, timestamp, amount, price,
			   gas_used, slippage, tx_hash, chain_id
		FROM execution_history 
		WHERE order_id = $1 
		ORDER BY interval_number ASC
	`

	rows, err := db.db.Query(query, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*ExecutionRecord
	for rows.Next() {
		record := &ExecutionRecord{}
		err := rows.Scan(
			&record.ID, &record.OrderID, &record.IntervalNumber,
			&record.Timestamp, &record.Amount, &record.Price,
			&record.GasUsed, &record.Slippage, &record.TxHash, &record.ChainID,
		)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, nil
}

// Price history operations
func (db *PostgreSQLDB) CreatePricePoint(point *PricePoint) error {
	query := `
		INSERT INTO price_history (token_pair, timestamp, price, volume, source, chain_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := db.db.Exec(
		query,
		point.TokenPair, point.Timestamp, point.Price,
		point.Volume, point.Source, point.ChainID,
	)

	return err
}

func (db *PostgreSQLDB) GetPriceHistory(tokenPair string, windowMinutes int) ([]*PricePoint, error) {
	query := `
		SELECT id, token_pair, timestamp, price, volume, source, chain_id
		FROM price_history 
		WHERE token_pair = $1 
		AND timestamp >= NOW() - INTERVAL '%d minutes'
		ORDER BY timestamp ASC
	`

	rows, err := db.db.Query(fmt.Sprintf(query, windowMinutes), tokenPair)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []*PricePoint
	for rows.Next() {
		point := &PricePoint{}
		err := rows.Scan(
			&point.ID, &point.TokenPair, &point.Timestamp,
			&point.Price, &point.Volume, &point.Source, &point.ChainID,
		)
		if err != nil {
			return nil, err
		}
		points = append(points, point)
	}

	return points, nil
}

func (db *PostgreSQLDB) UpdatePriceHistory(tokenPair string, points []*PricePoint) error {
	tx, err := db.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO price_history (token_pair, timestamp, price, volume, source, chain_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (token_pair, timestamp, source) 
		DO UPDATE SET price = EXCLUDED.price, volume = EXCLUDED.volume
	`

	for _, point := range points {
		_, err = tx.Exec(
			query,
			point.TokenPair, point.Timestamp, point.Price,
			point.Volume, point.Source, point.ChainID,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// HTLC operations
func (db *PostgreSQLDB) CreateHTLC(htlc *HTLC) error {
	query := `
		INSERT INTO htlcs (
			address, order_id, hashed_secret, amount, token,
			sender, receiver, timeout_height, timeout_timestamp,
			status, chain_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err := db.db.Exec(
		query,
		htlc.Address, htlc.OrderID, htlc.HashedSecret, htlc.Amount,
		htlc.Token, htlc.Sender, htlc.Receiver, htlc.TimeoutHeight,
		htlc.TimeoutTimestamp, htlc.Status, htlc.ChainID,
	)

	return err
}

func (db *PostgreSQLDB) GetHTLC(htlcAddress string) (*HTLC, error) {
	query := `
		SELECT address, order_id, hashed_secret, amount, token,
			   sender, receiver, timeout_height, timeout_timestamp,
			   status, created_at, claimed_at, secret, chain_id
		FROM htlcs WHERE address = $1
	`

	row := db.db.QueryRow(query, htlcAddress)
	
	htlc := &HTLC{}
	err := row.Scan(
		&htlc.Address, &htlc.OrderID, &htlc.HashedSecret, &htlc.Amount,
		&htlc.Token, &htlc.Sender, &htlc.Receiver, &htlc.TimeoutHeight,
		&htlc.TimeoutTimestamp, &htlc.Status, &htlc.CreatedAt,
		&htlc.ClaimedAt, &htlc.Secret, &htlc.ChainID,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrHTLCNotFound
		}
		return nil, err
	}

	return htlc, nil
}

func (db *PostgreSQLDB) UpdateHTLC(htlc *HTLC) error {
	query := `
		UPDATE htlcs SET
			status = $2,
			claimed_at = $3,
			secret = $4
		WHERE address = $1
	`

	_, err := db.db.Exec(
		query,
		htlc.Address, htlc.Status, htlc.ClaimedAt, htlc.Secret,
	)

	return err
}

// Chain operations
func (db *PostgreSQLDB) GetSupportedChains() ([]string, error) {
	query := `SELECT chain_id FROM chain_status WHERE enabled = true`

	rows, err := db.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chains []string
	for rows.Next() {
		var chainID string
		if err := rows.Scan(&chainID); err != nil {
			return nil, err
		}
		chains = append(chains, chainID)
	}

	return chains, nil
}

func (db *PostgreSQLDB) GetChainStatus(chainID string) (*ChainStatus, error) {
	query := `
		SELECT chain_id, name, enabled, last_block_height, last_block_time,
			   avg_block_time, gas_price, health_status, last_health_check
		FROM chain_status WHERE chain_id = $1
	`

	row := db.db.QueryRow(query, chainID)
	
	status := &ChainStatus{}
	err := row.Scan(
		&status.ChainID, &status.Name, &status.Enabled,
		&status.LastBlockHeight, &status.LastBlockTime,
		&status.AvgBlockTime, &status.GasPrice,
		&status.HealthStatus, &status.LastHealthCheck,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrChainNotFound
		}
		return nil, err
	}

	return status, nil
}

func (db *PostgreSQLDB) StorePricePoint(point *PricePoint) error {
    query := `
        INSERT INTO price_points (token_pair, source, price, volume, timestamp, created_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `
    
    _, err := db.db.Exec(query,
        point.TokenPair,
        point.Source, 
        point.Price,
        point.Volume,
        point.Timestamp,
        point.CreatedAt,
    )
    
    return err
}

func (db *PostgreSQLDB) GetPricePoints(tokenPair string, since time.Time) ([]*PricePoint, error) {
    query := `
        SELECT id, token_pair, source, price, volume, timestamp, created_at
        FROM price_points
        WHERE token_pair = $1 AND timestamp >= $2
        ORDER BY timestamp ASC
    `
    
    rows, err := db.db.Query(query, tokenPair, since)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var points []*PricePoint
    for rows.Next() {
        point := &PricePoint{}
        err := rows.Scan(
            &point.ID,
            &point.TokenPair,
            &point.Source,
            &point.Price,
            &point.Volume,
            &point.Timestamp,
            &point.CreatedAt,
        )
        if err != nil {
            return nil, err
        }
        points = append(points, point)
    }
    
    return points, nil
}

func (db *PostgreSQLDB) GetLatestPrice(tokenPair, source string) (*PricePoint, error) {
    query := `
        SELECT id, token_pair, source, price, volume, timestamp, created_at
        FROM price_points
        WHERE token_pair = $1 AND source = $2
        ORDER BY timestamp DESC
        LIMIT 1
    `
    
    row := db.db.QueryRow(query, tokenPair, source)
    
    point := &PricePoint{}
    err := row.Scan(
        &point.ID,
        &point.TokenPair,
        &point.Source,
        &point.Price,
        &point.Volume,
        &point.Timestamp,
        &point.CreatedAt,
    )
    
    if err != nil {
        if err == sql.ErrNoRows {
            return nil, nil // No price data found
        }
        return nil, err
    }
    
    return point, nil
}

func (db *PostgreSQLDB) CleanupOldPricePoints(olderThan time.Time) error {
    query := `DELETE FROM price_points WHERE timestamp < $1`
    
    result, err := db.db.Exec(query, olderThan)
    if err != nil {
        return err
    }
    
    rowsAffected, err := result.RowsAffected()
    if err != nil {
        return err
    }
    
    if rowsAffected > 0 {
        // Log cleanup activity
        fmt.Printf("Cleaned up %d old price points\n", rowsAffected)
    }
    
    return nil
}

// Health check
func (db *PostgreSQLDB) Health() error {
	return db.db.Ping()
}

// Close closes the database connection
func (db *PostgreSQLDB) Close() error {
	return db.db.Close()
}
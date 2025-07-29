-- Database initialization script
-- This runs when the PostgreSQL container starts for the first time

-- Create extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Create indexes for better performance
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_user_status ON orders(user_address, status);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_chains ON orders(source_chain, target_chain);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_created_status ON orders(created_at, status);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_execution_history_order_timestamp ON execution_history(order_id, timestamp);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_price_history_pair_timestamp ON price_history(token_pair, timestamp DESC);

-- Create materialized view for order statistics
CREATE MATERIALIZED VIEW IF NOT EXISTS order_statistics AS
SELECT 
    DATE_TRUNC('day', created_at) as date,
    source_chain,
    target_chain,
    status,
    COUNT(*) as order_count,
    SUM(source_amount) as total_volume,
    AVG(executed_amount::numeric / source_amount::numeric * 100) as avg_completion_rate
FROM orders 
WHERE created_at >= NOW() - INTERVAL '30 days'
GROUP BY DATE_TRUNC('day', created_at), source_chain, target_chain, status;

-- Create refresh function
CREATE OR REPLACE FUNCTION refresh_order_statistics()
RETURNS void AS $$
BEGIN
    REFRESH MATERIALIZED VIEW order_statistics;
END;
$$ LANGUAGE plpgsql;

-- Schedule the refresh (requires pg_cron extension in production)
-- SELECT cron.schedule('refresh-stats', '*/15 * * * *', 'SELECT refresh_order_statistics();');

---
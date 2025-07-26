pragma solidity ^0.8.24;

/**
 * @title TWAPMath
 * @notice Library for Time-Weighted Average Price calculations and related math
 * @dev Used by FlowFusion Bridge for sophisticated TWAP execution
 */
library TWAPMath {
    
    /*//////////////////////////////////////////////////////////////
                                CONSTANTS
    //////////////////////////////////////////////////////////////*/
    
    uint256 constant BASIS_POINTS = 10000;
    uint256 constant PRECISION = 1e18;
    
    /*//////////////////////////////////////////////////////////////
                                STRUCTS
    //////////////////////////////////////////////////////////////*/
    
    struct PricePoint {
        uint256 timestamp;
        uint256 price;
        uint256 volume;
    }
    
    struct TWAPData {
        uint256 cumulativePrice;
        uint256 cumulativeVolume;
        uint256 lastUpdateTime;
        uint256 windowStart;
    }

    /*//////////////////////////////////////////////////////////////
                            TWAP CALCULATIONS
    //////////////////////////////////////////////////////////////*/
    
    /**
     * @notice Calculate Time-Weighted Average Price from price points
     * @param pricePoints Array of price data points
     * @param windowStart Start of the TWAP window (timestamp)
     * @param windowEnd End of the TWAP window (timestamp)
     * @return twapPrice Calculated TWAP price
     */
    function calculateTWAP(
        PricePoint[] memory pricePoints,
        uint256 windowStart,
        uint256 windowEnd
    ) internal pure returns (uint256 twapPrice) {
        require(windowEnd > windowStart, "Invalid window");
        
        if (pricePoints.length == 0) return 0;
        
        uint256 totalValue = 0;
        uint256 totalTime = 0;
        
        for (uint256 i = 0; i < pricePoints.length; i++) {
            if (pricePoints[i].timestamp >= windowStart && pricePoints[i].timestamp <= windowEnd) {
                uint256 timeWeight = i > 0 ? 
                    pricePoints[i].timestamp - pricePoints[i-1].timestamp : 
                    windowEnd - windowStart;
                
                // Prevent division by zero and overflow
                if (timeWeight > 0 && pricePoints[i].price > 0) {
                    totalValue += pricePoints[i].price * timeWeight;
                    totalTime += timeWeight;
                }
            }
        }
        
        return totalTime > 0 ? totalValue / totalTime : 0;
    }
    
    /**
     * @notice Calculate Volume-Weighted Average Price
     * @param pricePoints Array of price data points with volume
     * @param windowStart Start of the VWAP window
     * @param windowEnd End of the VWAP window
     * @return vwapPrice Calculated VWAP price
     */
    function calculateVWAP(
        PricePoint[] memory pricePoints,
        uint256 windowStart,
        uint256 windowEnd
    ) internal pure returns (uint256 vwapPrice) {
        require(windowEnd > windowStart, "Invalid window");
        
        if (pricePoints.length == 0) return 0;
        
        uint256 totalValue = 0;
        uint256 totalVolume = 0;
        
        for (uint256 i = 0; i < pricePoints.length; i++) {
            if (pricePoints[i].timestamp >= windowStart && 
                pricePoints[i].timestamp <= windowEnd &&
                pricePoints[i].volume > 0) {
                
                totalValue += pricePoints[i].price * pricePoints[i].volume;
                totalVolume += pricePoints[i].volume;
            }
        }
        
        return totalVolume > 0 ? totalValue / totalVolume : 0;
    }

    /*//////////////////////////////////////////////////////////////
                         SLIPPAGE CALCULATIONS
    //////////////////////////////////////////////////////////////*/
    
    /**
     * @notice Calculate slippage between expected and actual price
     * @param expectedPrice Expected price
     * @param actualPrice Actual execution price
     * @return slippage Slippage in basis points
     */
    function calculateSlippage(
        uint256 expectedPrice,
        uint256 actualPrice
    ) internal pure returns (uint256 slippage) {
        if (expectedPrice == 0) return 0;
        
        uint256 difference = expectedPrice > actualPrice ? 
            expectedPrice - actualPrice : 
            actualPrice - expectedPrice;
        
        return (difference * BASIS_POINTS) / expectedPrice;
    }
    
    /**
     * @notice Check if slippage is within tolerance
     * @param expectedPrice Expected price
     * @param actualPrice Actual price
     * @param maxSlippage Maximum allowed slippage in basis points
     * @return isValid True if slippage is within tolerance
     */
    function isSlippageAcceptable(
        uint256 expectedPrice,
        uint256 actualPrice,
        uint256 maxSlippage
    ) internal pure returns (bool isValid) {
        uint256 slippage = calculateSlippage(expectedPrice, actualPrice);
        return slippage <= maxSlippage;
    }

    /*//////////////////////////////////////////////////////////////
                        PRICE IMPACT CALCULATIONS
    //////////////////////////////////////////////////////////////*/
    
    /**
     * @notice Calculate price impact for a given trade size
     * @param currentPrice Current market price
     * @param tradeSize Size of the trade
     * @param liquidity Available liquidity
     * @return priceImpact Price impact in basis points
     */
    function calculatePriceImpact(
        uint256 currentPrice,
        uint256 tradeSize,
        uint256 liquidity
    ) internal pure returns (uint256 priceImpact) {
        if (liquidity == 0 || currentPrice == 0) return BASIS_POINTS; // 100% impact
        
        // Simplified price impact model: impact = (tradeSize / liquidity) * 100%
        uint256 impactRatio = (tradeSize * BASIS_POINTS) / liquidity;
        
        // Cap at 100% impact
        return impactRatio > BASIS_POINTS ? BASIS_POINTS : impactRatio;
    }

    /*//////////////////////////////////////////////////////////////
                         INTERVAL CALCULATIONS
    //////////////////////////////////////////////////////////////*/
    
    /**
     * @notice Calculate optimal interval size for TWAP execution
     * @param totalAmount Total amount to execute
     * @param numberOfIntervals Number of execution intervals
     * @param minFillSize Minimum fill size per interval
     * @return intervalSize Optimal size per interval
     */
    function calculateIntervalSize(
        uint256 totalAmount,
        uint256 numberOfIntervals,
        uint256 minFillSize
    ) internal pure returns (uint256 intervalSize) {
        require(numberOfIntervals > 0, "Invalid intervals");
        
        uint256 baseSize = totalAmount / numberOfIntervals;
        return baseSize < minFillSize ? minFillSize : baseSize;
    }
    
    /**
     * @notice Calculate time between intervals
     * @param windowMinutes Total execution window in minutes
     * @param numberOfIntervals Number of execution intervals
     * @return intervalDuration Duration between intervals in seconds
     */
    function calculateIntervalDuration(
        uint256 windowMinutes,
        uint256 numberOfIntervals
    ) internal pure returns (uint256 intervalDuration) {
        require(numberOfIntervals > 0, "Invalid intervals");
        return (windowMinutes * 60) / numberOfIntervals;
    }

    /*//////////////////////////////////////////////////////////////
                         WEIGHTED AVERAGES
    //////////////////////////////////////////////////////////////*/
    
    /**
     * @notice Calculate weighted average price
     * @param prices Array of prices
     * @param weights Array of weights (same length as prices)
     * @return weightedAverage Calculated weighted average
     */
    function calculateWeightedAverage(
        uint256[] memory prices,
        uint256[] memory weights
    ) internal pure returns (uint256 weightedAverage) {
        require(prices.length == weights.length, "Array length mismatch");
        
        if (prices.length == 0) return 0;
        
        uint256 totalValue = 0;
        uint256 totalWeight = 0;
        
        for (uint256 i = 0; i < prices.length; i++) {
            totalValue += prices[i] * weights[i];
            totalWeight += weights[i];
        }
        
        return totalWeight > 0 ? totalValue / totalWeight : 0;
    }
    
    /**
     * @notice Update running weighted average with new data point
     * @param currentAverage Current weighted average
     * @param currentWeight Current total weight
     * @param newPrice New price to incorporate
     * @param newWeight Weight of new price
     * @return newAverage Updated weighted average
     * @return newTotalWeight Updated total weight
     */
    function updateWeightedAverage(
        uint256 currentAverage,
        uint256 currentWeight,
        uint256 newPrice,
        uint256 newWeight
    ) internal pure returns (uint256 newAverage, uint256 newTotalWeight) {
        newTotalWeight = currentWeight + newWeight;
        
        if (newTotalWeight == 0) {
            return (0, 0);
        }
        
        uint256 totalValue = (currentAverage * currentWeight) + (newPrice * newWeight);
        newAverage = totalValue / newTotalWeight;
        
        return (newAverage, newTotalWeight);
    }

    /*//////////////////////////////////////////////////////////////
                          UTILITY FUNCTIONS
    //////////////////////////////////////////////////////////////*/
    
    /**
     * @notice Safe multiplication with overflow protection
     */
    function safeMul(uint256 a, uint256 b) internal pure returns (uint256) {
        if (a == 0) return 0;
        uint256 c = a * b;
        require(c / a == b, "Multiplication overflow");
        return c;
    }
    
    /**
     * @notice Safe division with zero protection
     */
    function safeDiv(uint256 a, uint256 b) internal pure returns (uint256) {
        require(b > 0, "Division by zero");
        return a / b;
    }
    
    /**
     * @notice Calculate percentage of a value
     * @param value Base value
     * @param percentage Percentage in basis points
     * @return result Calculated percentage
     */
    function percentage(uint256 value, uint256 percentage) internal pure returns (uint256) {
        return (value * percentage) / BASIS_POINTS;
    }
    
    /**
     * @notice Linear interpolation between two values
     * @param start Start value
     * @param end End value
     * @param progress Progress from 0 to PRECISION (1e18)
     * @return interpolated Interpolated value
     */
    function linearInterpolation(
        uint256 start,
        uint256 end,
        uint256 progress
    ) internal pure returns (uint256 interpolated) {
        require(progress <= PRECISION, "Progress out of range");
        
        if (progress == 0) return start;
        if (progress == PRECISION) return end;
        
        if (end >= start) {
            uint256 diff = end - start;
            return start + (diff * progress) / PRECISION;
        } else {
            uint256 diff = start - end;
            return start - (diff * progress) / PRECISION;
        }
    }
    
    /**
     * @notice Calculate exponential moving average
     * @param currentEMA Current EMA value
     * @param newValue New value to incorporate
     * @param alpha Smoothing factor (0 to PRECISION)
     * @return newEMA Updated EMA value
     */
    function exponentialMovingAverage(
        uint256 currentEMA,
        uint256 newValue,
        uint256 alpha
    ) internal pure returns (uint256 newEMA) {
        require(alpha <= PRECISION, "Alpha out of range");
        
        // EMA = alpha * newValue + (1 - alpha) * currentEMA
        uint256 newComponent = (alpha * newValue) / PRECISION;
        uint256 oldComponent = ((PRECISION - alpha) * currentEMA) / PRECISION;
        
        return newComponent + oldComponent;
    }
}
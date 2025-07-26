// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/**
 * @title IFlowFusionBridge
 * @notice Interface for FlowFusion Multi-Chain TWAP Bridge
 */
interface IFlowFusionBridge {
    
    /*//////////////////////////////////////////////////////////////
                                STRUCTS
    //////////////////////////////////////////////////////////////*/
    
    struct TWAPConfig {
        uint256 windowMinutes;      // TWAP calculation window (5-1440 minutes)
        uint256 executionIntervals; // Number of execution intervals (2-20)
        uint256 maxSlippage;        // Maximum slippage in basis points (1-1000)
        uint256 minFillSize;        // Minimum fill size per interval
        bool enableMEVProtection;   // Enable MEV protection
    }
    
    struct TWAPOrder {
        bytes32 id;                 // Unique order identifier
        address user;               // Order creator
        address sourceToken;        // Source token address (address(0) for ETH)
        uint256 sourceAmount;       // Total source amount
        string targetChain;         // Target blockchain identifier
        string targetToken;         // Target token identifier
        string targetRecipient;     // Recipient address on target chain
        TWAPConfig twapConfig;      // TWAP execution configuration
        bytes32 htlcHash;          // Hash for HTLC mechanism
        uint256 timeoutHeight;      // Block height timeout
        uint256 createdAt;          // Creation timestamp
        uint256 executedAmount;     // Amount already executed
        uint256 lastExecution;      // Last execution timestamp
        OrderStatus status;         // Current order status
        uint256 averagePrice;       // Weighted average execution price
    }
    
    struct ExecutionRecord {
        uint256 timestamp;          // Execution timestamp
        uint256 amount;            // Amount executed
        uint256 price;             // Execution price
        uint256 gasUsed;           // Gas used for execution
        uint256 slippage;          // Slippage in basis points
    }
    
    enum OrderStatus {
        Executing,      // Order is being executed
        Completed,      // All intervals executed
        Cancelled,      // Order cancelled by user
        Expired,        // Order expired (timeout reached)
        Claimed         // HTLC claimed (cross-chain completed)
    }

    /*//////////////////////////////////////////////////////////////
                                EVENTS
    //////////////////////////////////////////////////////////////*/
    
    event OrderCreated(
        bytes32 indexed orderId,
        address indexed user,
        string targetChain,
        address sourceToken,
        uint256 sourceAmount,
        string targetToken,
        TWAPConfig twapConfig
    );

    event TWAPExecution(
        bytes32 indexed orderId,
        uint256 intervalNumber,
        uint256 executedAmount,
        uint256 averagePrice,
        uint256 timestamp
    );

    event OrderCompleted(
        bytes32 indexed orderId,
        uint256 totalExecuted,
        uint256 averagePrice
    );

    event OrderCancelled(
        bytes32 indexed orderId,
        address indexed user,
        uint256 refundAmount
    );

    event HTLCCreated(
        bytes32 indexed orderId,
        bytes32 indexed htlcHash,
        uint256 amount,
        uint256 timeoutHeight
    );

    event HTLCClaimed(
        bytes32 indexed orderId,
        bytes32 secret,
        address claimer
    );

    /*//////////////////////////////////////////////////////////////
                            MAIN FUNCTIONS
    //////////////////////////////////////////////////////////////*/
    
    /**
     * @notice Create a new TWAP order for cross-chain execution
     */
    function createTWAPOrder(
        address sourceToken,
        uint256 sourceAmount,
        string memory targetChain,
        string memory targetToken,
        string memory targetRecipient,
        TWAPConfig memory twapConfig,
        bytes32 htlcHash,
        uint256 timeoutHeight
    ) external payable returns (bytes32 orderId);

    /**
     * @notice Execute a TWAP interval for an order
     */
    function executeTWAPInterval(
        bytes32 orderId,
        uint256 intervalAmount,
        uint256 executionPrice,
        bytes calldata priceProof
    ) external;

    /**
     * @notice Cancel an order and refund remaining tokens
     */
    function cancelOrder(bytes32 orderId) external;

    /**
     * @notice Claim HTLC with secret to complete cross-chain swap
     */
    function claimHTLC(bytes32 orderId, bytes32 secret) external;

    /*//////////////////////////////////////////////////////////////
                            VIEW FUNCTIONS
    //////////////////////////////////////////////////////////////*/
    
    function getOrder(bytes32 orderId) external view returns (TWAPOrder memory);
    function getUserOrders(address user) external view returns (bytes32[] memory);
    function getExecutionHistory(bytes32 orderId) external view returns (ExecutionRecord[] memory);
    function getCurrentTWAPPrice(bytes32 orderId) external view returns (uint256);
    function getNextExecutionTime(bytes32 orderId) external view returns (uint256);
}
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import "@openzeppelin/contracts/utils/Pausable.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "./libraries/TWAPMath.sol";
import "./interfaces/IFlowFusionBridge.sol";

/**
 * @title FlowFusion Bridge
 * @notice Multi-chain TWAP bridge for professional trading with MEV protection
 * @dev Handles order creation, TWAP execution, and cross-chain coordination
 * @author FlowFusion Team
 */
contract FlowFusionBridge is 
    IFlowFusionBridge, 
    ReentrancyGuard, 
    Pausable, 
    Ownable 
{
    using SafeERC20 for IERC20;
    using TWAPMath for uint256;

    /*//////////////////////////////////////////////////////////////
                                CONSTANTS
    //////////////////////////////////////////////////////////////*/
    
    uint256 public constant BASIS_POINTS = 10000;
    uint256 public constant MAX_SLIPPAGE = 1000; // 10%
    uint256 public constant MIN_WINDOW_MINUTES = 5;
    uint256 public constant MAX_WINDOW_MINUTES = 1440; // 24 hours
    uint256 public constant MAX_EXECUTION_INTERVALS = 20;
    uint256 public constant MIN_EXECUTION_AMOUNT = 1000; // Minimum wei to prevent dust attacks
    uint256 public constant VERSION = 1;

    /*//////////////////////////////////////////////////////////////
                                 STATE
    //////////////////////////////////////////////////////////////*/
    
    mapping(bytes32 => TWAPOrder) public orders;
    mapping(address => bytes32[]) public userOrders;
    mapping(bytes32 => ExecutionRecord[]) public executionHistory;
    mapping(string => bool) public supportedChains;
    mapping(address => bool) public authorizedExecutors;
    mapping(address => uint256) public userOrderCount; // Track user order counts
    
    uint256 public orderNonce;
    uint256 public totalOrders;
    uint256 public totalVolume;
    address public feeCollector;
    uint256 public protocolFeeRate; // basis points
    uint256 public maxOrdersPerUser = 100; // Prevent spam
    
    /*//////////////////////////////////////////////////////////////
                                EVENTS
    //////////////////////////////////////////////////////////////*/
    
    /* event OrderCreated(
        bytes32 indexed orderId,
        address indexed user,
        string targetChain,
        address sourceToken,
        uint256 sourceAmount,
        string targetToken,
        TWAPConfig twapConfig
    ); */

    event TWAPExecution(
        bytes32 indexed orderId,
        uint256 intervalNumber,
        uint256 executedAmount,
        uint256 averagePrice,
        uint256 timestamp,
        address executor
    );

    event OrderCompleted(
        bytes32 indexed orderId,
        uint256 totalExecuted,
        uint256 averagePrice,
        uint256 completionTime
    );

    event OrderCancelled(
        bytes32 indexed orderId,
        address indexed user,
        uint256 refundAmount,
        uint256 cancelledAt
    );

    /* event HTLCCreated(
        bytes32 indexed orderId,
        bytes32 indexed htlcHash,
        uint256 amount,
        uint256 timeoutHeight
    ); */

    event HTLCClaimed(
        bytes32 indexed orderId,
        bytes32 secret,
        address claimer,
        uint256 claimedAt
    );

    event ProtocolFeeCollected(
        bytes32 indexed orderId,
        uint256 feeAmount,
        address feeCollector
    );

    /*//////////////////////////////////////////////////////////////
                               MODIFIERS
    //////////////////////////////////////////////////////////////*/
    
    modifier onlyAuthorizedExecutor() {
        require(authorizedExecutors[msg.sender], "FlowFusion: Not authorized executor");
        _;
    }

    modifier orderExists(bytes32 orderId) {
        require(orders[orderId].user != address(0), "FlowFusion: Order does not exist");
        _;
    }

    modifier orderActive(bytes32 orderId) {
        require(orders[orderId].status == OrderStatus.Executing, "FlowFusion: Order not active");
        _;
    }

    modifier onlyOrderOwner(bytes32 orderId) {
        require(orders[orderId].user == msg.sender, "FlowFusion: Not order owner");
        _;
    }

    /*//////////////////////////////////////////////////////////////
                              CONSTRUCTOR
    //////////////////////////////////////////////////////////////*/
    
    constructor(
        address _feeCollector,
        uint256 _protocolFeeRate
    ) Ownable(msg.sender) {
        require(_feeCollector != address(0), "FlowFusion: Invalid fee collector");
        require(_protocolFeeRate <= 100, "FlowFusion: Fee rate too high"); // Max 1%
        
        feeCollector = _feeCollector;
        protocolFeeRate = _protocolFeeRate;
        
        // Initialize supported chains
        supportedChains["cosmos"] = true;
        supportedChains["stellar"] = true;
        supportedChains["bitcoin"] = true;
        supportedChains["ethereum"] = true;
        supportedChains["polygon"] = true;
        supportedChains["arbitrum"] = true;
        supportedChains["optimism"] = true;
    }

    /*//////////////////////////////////////////////////////////////
                            ORDER MANAGEMENT
    //////////////////////////////////////////////////////////////*/
    
    /**
     * @notice Create a new TWAP order for cross-chain execution
     * @param sourceToken Address of source token (address(0) for ETH)
     * @param sourceAmount Amount of source tokens
     * @param targetChain Target blockchain identifier
     * @param targetToken Target token identifier
     * @param targetRecipient Recipient address on target chain
     * @param twapConfig TWAP execution configuration
     * @param htlcHash Hash for HTLC mechanism
     * @param timeoutHeight Block height timeout
     * @return orderId Unique order identifier
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
    ) external payable nonReentrant whenNotPaused returns (bytes32 orderId) {
        // Validate inputs
        require(sourceAmount >= MIN_EXECUTION_AMOUNT, "FlowFusion: Amount too small");
        require(supportedChains[targetChain], "FlowFusion: Unsupported target chain");
        require(bytes(targetToken).length > 0, "FlowFusion: Invalid target token");
        require(bytes(targetRecipient).length > 0, "FlowFusion: Invalid recipient");
        require(htlcHash != bytes32(0), "FlowFusion: Invalid HTLC hash");
        require(timeoutHeight > block.number + 100, "FlowFusion: Timeout too soon"); // At least 100 blocks
        require(userOrderCount[msg.sender] < maxOrdersPerUser, "FlowFusion: Too many orders");
        
        // Validate TWAP configuration
        _validateTWAPConfig(twapConfig);
        
        // Calculate protocol fee
        uint256 protocolFee = (sourceAmount * protocolFeeRate) / BASIS_POINTS;
        uint256 netAmount = sourceAmount - protocolFee;
        
        // Generate order ID
        orderId = keccak256(abi.encodePacked(
            msg.sender,
            sourceToken,
            sourceAmount,
            targetChain,
            block.timestamp,
            orderNonce++
        ));
        
        // Handle token transfer
        if (sourceToken == address(0)) {
            require(msg.value == sourceAmount, "FlowFusion: ETH amount mismatch");
            // Send protocol fee to fee collector
            if (protocolFee > 0) {
                payable(feeCollector).transfer(protocolFee);
                emit ProtocolFeeCollected(orderId, protocolFee, feeCollector);
            }
        } else {
            IERC20(sourceToken).safeTransferFrom(msg.sender, address(this), sourceAmount);
            // Send protocol fee to fee collector
            if (protocolFee > 0) {
                IERC20(sourceToken).safeTransfer(feeCollector, protocolFee);
                emit ProtocolFeeCollected(orderId, protocolFee, feeCollector);
            }
        }
        
        // Create order
        orders[orderId] = TWAPOrder({
            id: orderId,
            user: msg.sender,
            sourceToken: sourceToken,
            sourceAmount: netAmount, // Store net amount after fee
            targetChain: targetChain,
            targetToken: targetToken,
            targetRecipient: targetRecipient,
            twapConfig: twapConfig,
            htlcHash: htlcHash,
            timeoutHeight: timeoutHeight,
            createdAt: block.timestamp,
            executedAmount: 0,
            lastExecution: 0,
            status: OrderStatus.Executing,
            averagePrice: 0
        });
        
        // Update mappings
        userOrders[msg.sender].push(orderId);
        userOrderCount[msg.sender]++;
        totalOrders++;
        totalVolume += sourceAmount;
        
        emit OrderCreated(
            orderId,
            msg.sender,
            targetChain,
            sourceToken,
            sourceAmount,
            targetToken,
            twapConfig
        );
        
        emit HTLCCreated(orderId, htlcHash, netAmount, timeoutHeight);
        
        return orderId;
    }

    /**
     * @notice Execute a TWAP interval for an order
     * @param orderId Order to execute
     * @param intervalAmount Amount to execute in this interval
     * @param executionPrice Price for this execution
     * @param priceProof Proof of price validity (for oracle verification)
     */
    function executeTWAPInterval(
        bytes32 orderId,
        uint256 intervalAmount,
        uint256 executionPrice,
        bytes calldata priceProof
    ) external onlyAuthorizedExecutor orderExists(orderId) orderActive(orderId) nonReentrant {
        TWAPOrder storage order = orders[orderId];
        
        // Validate execution timing
        require(intervalAmount > 0, "FlowFusion: Invalid interval amount");
        require(intervalAmount <= order.sourceAmount - order.executedAmount, "FlowFusion: Exceeds remaining");
        require(block.number < order.timeoutHeight, "FlowFusion: Order expired");
        
        // Check minimum interval time
        uint256 intervalDuration = _getIntervalDuration(order.twapConfig);
        require(
            order.lastExecution == 0 || 
            block.timestamp >= order.lastExecution + intervalDuration,
            "FlowFusion: Too early for next execution"
        );
        
        // Verify price (implement oracle price verification)
        require(_verifyPrice(executionPrice, priceProof), "FlowFusion: Invalid price");
        
        // Calculate and validate slippage
        uint256 twapPrice = _calculateTWAP(orderId);
        if (twapPrice > 0) {
            uint256 slippage = _calculateSlippage(twapPrice, executionPrice);
            require(slippage <= order.twapConfig.maxSlippage, "FlowFusion: Slippage too high");
        }
        
        // Validate minimum fill size
        require(
            intervalAmount >= order.twapConfig.minFillSize || 
            intervalAmount == order.sourceAmount - order.executedAmount, // Allow final small amount
            "FlowFusion: Below minimum fill size"
        );
        
        // Execute interval
        uint256 intervalNumber = executionHistory[orderId].length;
        
        // Record execution
        executionHistory[orderId].push(ExecutionRecord({
            timestamp: block.timestamp,
            amount: intervalAmount,
            price: executionPrice,
            gasUsed: gasleft(),
            slippage: twapPrice > 0 ? _calculateSlippage(twapPrice, executionPrice) : 0
        }));
        
        // Update order state
        order.executedAmount += intervalAmount;
        order.lastExecution = block.timestamp;
        
        // Update average price
        if (order.averagePrice == 0) {
            order.averagePrice = executionPrice;
        } else {
            order.averagePrice = _calculateWeightedAveragePrice(order, executionPrice, intervalAmount);
        }
        
        emit TWAPExecution(
            orderId, 
            intervalNumber, 
            intervalAmount, 
            executionPrice, 
            block.timestamp,
            msg.sender
        );
        
        // Check if order is complete
        if (order.executedAmount >= order.sourceAmount) {
            order.status = OrderStatus.Completed;
            emit OrderCompleted(orderId, order.executedAmount, order.averagePrice, block.timestamp);
        }
    }

    /**
     * @notice Cancel an order and refund remaining tokens
     * @param orderId Order to cancel
     */
    function cancelOrder(bytes32 orderId) 
        external 
        orderExists(orderId) 
        onlyOrderOwner(orderId)
        nonReentrant 
    {
        TWAPOrder storage order = orders[orderId];
        require(order.status == OrderStatus.Executing, "FlowFusion: Order not cancellable");
        
        uint256 refundAmount = order.sourceAmount - order.executedAmount;
        
        if (refundAmount > 0) {
            order.status = OrderStatus.Cancelled;
            
            // Refund tokens
            if (order.sourceToken == address(0)) {
                payable(msg.sender).transfer(refundAmount);
            } else {
                IERC20(order.sourceToken).safeTransfer(msg.sender, refundAmount);
            }
            
            // Decrease user order count
            userOrderCount[msg.sender]--;
            
            emit OrderCancelled(orderId, msg.sender, refundAmount, block.timestamp);
        }
    }

    /**
     * @notice Claim HTLC with secret to complete cross-chain swap
     * @param orderId Order to claim
     * @param secret Secret to unlock HTLC
     */
    function claimHTLC(bytes32 orderId, bytes32 secret) 
        external 
        orderExists(orderId) 
        nonReentrant 
    {
        TWAPOrder storage order = orders[orderId];
        require(order.status == OrderStatus.Completed, "FlowFusion: Order not completed");
        require(keccak256(abi.encodePacked(secret)) == order.htlcHash, "FlowFusion: Invalid secret");
        
        order.status = OrderStatus.Claimed;
        
        // Decrease user order count
        userOrderCount[order.user]--;
        
        emit HTLCClaimed(orderId, secret, msg.sender, block.timestamp);
    }

    /*//////////////////////////////////////////////////////////////
                            VIEW FUNCTIONS
    //////////////////////////////////////////////////////////////*/
    
    function getOrder(bytes32 orderId) external view returns (TWAPOrder memory) {
        return orders[orderId];
    }
    
    function getUserOrders(address user) external view returns (bytes32[] memory) {
        return userOrders[user];
    }
    
    function getExecutionHistory(bytes32 orderId) external view returns (ExecutionRecord[] memory) {
        return executionHistory[orderId];
    }
    
    function getCurrentTWAPPrice(bytes32 orderId) external view returns (uint256) {
        return _calculateTWAP(orderId);
    }
    
    function getNextExecutionTime(bytes32 orderId) external view returns (uint256) {
        TWAPOrder memory order = orders[orderId];
        if (order.lastExecution == 0) return block.timestamp;
        return order.lastExecution + _getIntervalDuration(order.twapConfig);
    }

    function getOrdersByStatus(OrderStatus status) external view returns (bytes32[] memory) {
        bytes32[] memory result = new bytes32[](totalOrders);
        uint256 count = 0;
        
        // Note: This is inefficient for large datasets. Consider using events or off-chain indexing
        for (uint256 i = 0; i < orderNonce; i++) {
            bytes32 orderId = keccak256(abi.encodePacked(i));
            if (orders[orderId].user != address(0) && orders[orderId].status == status) {
                result[count] = orderId;
                count++;
            }
        }
        
        // Resize array
        bytes32[] memory trimmed = new bytes32[](count);
        for (uint256 i = 0; i < count; i++) {
            trimmed[i] = result[i];
        }
        
        return trimmed;
    }

    /*//////////////////////////////////////////////////////////////
                           INTERNAL FUNCTIONS
    //////////////////////////////////////////////////////////////*/
    
    function _validateTWAPConfig(TWAPConfig memory config) internal pure {
        require(config.windowMinutes >= MIN_WINDOW_MINUTES, "FlowFusion: Window too small");
        require(config.windowMinutes <= MAX_WINDOW_MINUTES, "FlowFusion: Window too large");
        require(config.executionIntervals > 1, "FlowFusion: Need multiple intervals");
        require(config.executionIntervals <= MAX_EXECUTION_INTERVALS, "FlowFusion: Too many intervals");
        require(config.maxSlippage <= MAX_SLIPPAGE, "FlowFusion: Slippage too high");
        require(config.minFillSize > 0, "FlowFusion: Invalid min fill size");
    }
    
    function _getIntervalDuration(TWAPConfig memory config) internal pure returns (uint256) {
        return (config.windowMinutes * 60) / config.executionIntervals;
    }
    
    function _calculateTWAP(bytes32 orderId) internal view returns (uint256) {
        ExecutionRecord[] memory history = executionHistory[orderId];
        if (history.length == 0) return 0;
        
        TWAPOrder memory order = orders[orderId];
        uint256 windowStart = block.timestamp - (order.twapConfig.windowMinutes * 60);
        
        uint256 totalValue = 0;
        uint256 totalWeight = 0;
        
        for (uint256 i = 0; i < history.length; i++) {
            if (history[i].timestamp >= windowStart) {
                uint256 weight = i > 0 ? 
                    history[i].timestamp - history[i-1].timestamp : 1;
                totalValue += history[i].price * weight;
                totalWeight += weight;
            }
        }
        
        return totalWeight > 0 ? totalValue / totalWeight : 0;
    }
    
    function _calculateSlippage(uint256 expectedPrice, uint256 actualPrice) 
        internal 
        pure 
        returns (uint256) 
    {
        if (expectedPrice == 0) return 0;
        uint256 diff = expectedPrice > actualPrice ? 
            expectedPrice - actualPrice : actualPrice - expectedPrice;
        return (diff * BASIS_POINTS) / expectedPrice;
    }
    
    function _calculateWeightedAveragePrice(
        TWAPOrder memory order,
        uint256 newPrice,
        uint256 newAmount
    ) internal pure returns (uint256) {
        uint256 totalValue = (order.averagePrice * order.executedAmount) + (newPrice * newAmount);
        uint256 totalAmount = order.executedAmount + newAmount;
        return totalValue / totalAmount;
    }
    
    function _verifyPrice(uint256 price, bytes calldata proof) internal pure returns (bool) {
        // Implement price oracle verification
        // This is simplified - integrate with Chainlink, Pyth, or other oracles
        // For now, just validate that price is positive and proof is provided
        return price > 0 && proof.length > 0;
    }

    /*//////////////////////////////////////////////////////////////
                            ADMIN FUNCTIONS
    //////////////////////////////////////////////////////////////*/
    
    function addSupportedChain(string memory chainId) external onlyOwner {
        supportedChains[chainId] = true;
    }
    
    function removeSupportedChain(string memory chainId) external onlyOwner {
        supportedChains[chainId] = false;
    }
    
    function addAuthorizedExecutor(address executor) external onlyOwner {
        require(executor != address(0), "FlowFusion: Invalid executor");
        authorizedExecutors[executor] = true;
    }
    
    function removeAuthorizedExecutor(address executor) external onlyOwner {
        authorizedExecutors[executor] = false;
    }
    
    function updateFeeCollector(address newFeeCollector) external onlyOwner {
        require(newFeeCollector != address(0), "FlowFusion: Invalid fee collector");
        feeCollector = newFeeCollector;
    }
    
    function updateProtocolFeeRate(uint256 newFeeRate) external onlyOwner {
        require(newFeeRate <= 100, "FlowFusion: Fee rate too high");
        protocolFeeRate = newFeeRate;
    }

    function updateMaxOrdersPerUser(uint256 newMaxOrders) external onlyOwner {
        require(newMaxOrders > 0, "FlowFusion: Invalid max orders");
        maxOrdersPerUser = newMaxOrders;
    }
    
    function pause() external onlyOwner {
        _pause();
    }
    
    function unpause() external onlyOwner {
        _unpause();
    }
    
    function emergencyWithdraw(address token, uint256 amount) external onlyOwner {
        if (token == address(0)) {
            payable(owner()).transfer(amount);
        } else {
            IERC20(token).safeTransfer(owner(), amount);
        }
    }

    /*//////////////////////////////////////////////////////////////
                        RECEIVE & FALLBACK
    //////////////////////////////////////////////////////////////*/
    
    receive() external payable {
        // Allow contract to receive ETH
        require(msg.value > 0, "FlowFusion: No ETH sent");
    }
    
    fallback() external payable {
        revert("FlowFusion: Function not found");
    }
}
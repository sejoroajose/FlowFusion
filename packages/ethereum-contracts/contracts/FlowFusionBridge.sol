pragma solidity ^0.8.24;

import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/security/Pausable.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "./libraries/TWAPMath.sol";
import "./interfaces/IFlowFusionBridge.sol";

/**
 * @title FlowFusion Bridge
 * @notice Multi-chain TWAP bridge for professional trading with MEV protection
 * @dev Handles order creation, TWAP execution, and cross-chain coordination
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
    uint256 public constant VERSION = 1;

    /*//////////////////////////////////////////////////////////////
                                 STATE
    //////////////////////////////////////////////////////////////*/
    
    mapping(bytes32 => TWAPOrder) public orders;
    mapping(address => bytes32[]) public userOrders;
    mapping(bytes32 => ExecutionRecord[]) public executionHistory;
    mapping(string => bool) public supportedChains;
    mapping(address => bool) public authorizedExecutors;
    
    uint256 public orderNonce;
    uint256 public totalOrders;
    uint256 public totalVolume;
    address public feeCollector;
    uint256 public protocolFeeRate; // basis points
    
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
                               MODIFIERS
    //////////////////////////////////////////////////////////////*/
    
    modifier onlyAuthorizedExecutor() {
        require(authorizedExecutors[msg.sender], "Not authorized executor");
        _;
    }

    modifier orderExists(bytes32 orderId) {
        require(orders[orderId].user != address(0), "Order does not exist");
        _;
    }

    modifier orderActive(bytes32 orderId) {
        require(orders[orderId].status == OrderStatus.Executing, "Order not active");
        _;
    }

    /*//////////////////////////////////////////////////////////////
                              CONSTRUCTOR
    //////////////////////////////////////////////////////////////*/
    
    constructor(
        address _feeCollector,
        uint256 _protocolFeeRate
    ) Ownable(msg.sender) {
        require(_feeCollector != address(0), "Invalid fee collector");
        require(_protocolFeeRate <= 100, "Fee rate too high"); // Max 1%
        
        feeCollector = _feeCollector;
        protocolFeeRate = _protocolFeeRate;
        
        // Initialize supported chains
        supportedChains["cosmos"] = true;
        supportedChains["stellar"] = true;
        supportedChains["bitcoin"] = true;
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
        require(sourceAmount > 0, "Invalid amount");
        require(supportedChains[targetChain], "Unsupported target chain");
        require(bytes(targetToken).length > 0, "Invalid target token");
        require(bytes(targetRecipient).length > 0, "Invalid recipient");
        require(htlcHash != bytes32(0), "Invalid HTLC hash");
        require(timeoutHeight > block.number, "Invalid timeout");
        
        // Validate TWAP configuration
        _validateTWAPConfig(twapConfig);
        
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
            require(msg.value == sourceAmount, "ETH amount mismatch");
        } else {
            IERC20(sourceToken).safeTransferFrom(msg.sender, address(this), sourceAmount);
        }
        
        // Create order
        orders[orderId] = TWAPOrder({
            id: orderId,
            user: msg.sender,
            sourceToken: sourceToken,
            sourceAmount: sourceAmount,
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
        
        emit HTLCCreated(orderId, htlcHash, sourceAmount, timeoutHeight);
        
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
        
        // Validate execution
        require(intervalAmount > 0, "Invalid interval amount");
        require(intervalAmount <= order.sourceAmount - order.executedAmount, "Exceeds remaining");
        require(block.timestamp >= order.lastExecution + _getIntervalDuration(order.twapConfig), "Too early");
        require(block.number < order.timeoutHeight, "Order expired");
        
        // Verify price (implement oracle price verification)
        require(_verifyPrice(executionPrice, priceProof), "Invalid price");
        
        // Calculate and validate slippage
        uint256 twapPrice = _calculateTWAP(orderId);
        if (twapPrice > 0) {
            uint256 slippage = _calculateSlippage(twapPrice, executionPrice);
            require(slippage <= order.twapConfig.maxSlippage, "Slippage too high");
        }
        
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
        
        emit TWAPExecution(orderId, intervalNumber, intervalAmount, executionPrice, block.timestamp);
        
        // Check if order is complete
        if (order.executedAmount >= order.sourceAmount) {
            order.status = OrderStatus.Completed;
            emit OrderCompleted(orderId, order.executedAmount, order.averagePrice);
        }
    }

    /**
     * @notice Cancel an order and refund remaining tokens
     * @param orderId Order to cancel
     */
    function cancelOrder(bytes32 orderId) 
        external 
        orderExists(orderId) 
        nonReentrant 
    {
        TWAPOrder storage order = orders[orderId];
        require(order.user == msg.sender, "Not order owner");
        require(order.status == OrderStatus.Executing, "Order not cancellable");
        
        uint256 refundAmount = order.sourceAmount - order.executedAmount;
        
        if (refundAmount > 0) {
            order.status = OrderStatus.Cancelled;
            
            // Refund tokens
            if (order.sourceToken == address(0)) {
                payable(msg.sender).transfer(refundAmount);
            } else {
                IERC20(order.sourceToken).safeTransfer(msg.sender, refundAmount);
            }
            
            emit OrderCancelled(orderId, msg.sender, refundAmount);
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
        require(order.status == OrderStatus.Completed, "Order not completed");
        require(keccak256(abi.encodePacked(secret)) == order.htlcHash, "Invalid secret");
        
        order.status = OrderStatus.Claimed;
        
        emit HTLCClaimed(orderId, secret, msg.sender);
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

    /*//////////////////////////////////////////////////////////////
                           INTERNAL FUNCTIONS
    //////////////////////////////////////////////////////////////*/
    
    function _validateTWAPConfig(TWAPConfig memory config) internal pure {
        require(config.windowMinutes >= MIN_WINDOW_MINUTES, "Window too small");
        require(config.windowMinutes <= MAX_WINDOW_MINUTES, "Window too large");
        require(config.executionIntervals > 1, "Need multiple intervals");
        require(config.executionIntervals <= MAX_EXECUTION_INTERVALS, "Too many intervals");
        require(config.maxSlippage <= MAX_SLIPPAGE, "Slippage too high");
        require(config.minFillSize > 0, "Invalid min fill size");
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
        authorizedExecutors[executor] = true;
    }
    
    function removeAuthorizedExecutor(address executor) external onlyOwner {
        authorizedExecutors[executor] = false;
    }
    
    function updateFeeCollector(address newFeeCollector) external onlyOwner {
        require(newFeeCollector != address(0), "Invalid fee collector");
        feeCollector = newFeeCollector;
    }
    
    function updateProtocolFeeRate(uint256 newFeeRate) external onlyOwner {
        require(newFeeRate <= 100, "Fee rate too high");
        protocolFeeRate = newFeeRate;
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
}
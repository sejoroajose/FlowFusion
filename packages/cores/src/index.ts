import { BigNumber } from 'bignumber.js';

// Chain identifiers
export enum ChainId {
  ETHEREUM = 'ethereum',
  COSMOS = 'cosmos',
  STELLAR = 'stellar',
  BITCOIN = 'bitcoin',
  NEAR = 'near',
}

export enum NetworkType {
  MAINNET = 'mainnet',
  TESTNET = 'testnet',
  LOCAL = 'local',
}

// Token representation
export interface Token {
  chainId: ChainId;
  address: string;
  symbol: string;
  name: string;
  decimals: number;
  logoURI?: string;
  isNative?: boolean;
}

export interface TokenAmount {
  token: Token;
  amount: string; // BigNumber string representation
}

// TWAP Configuration
export interface TWAPConfig {
  windowMinutes: number;        // Time window for TWAP calculation (5-1440)
  executionIntervals: number;   // Number of execution intervals (2-20)
  maxSlippage: number;         // Maximum slippage in basis points (1-1000)
  minFillSize: string;         // Minimum fill size per interval
  enableMEVProtection: boolean; // Enable MEV protection
  priceImpactThreshold: number; // Price impact threshold in basis points
}

// HTLC Parameters
export interface HTLCParams {
  hashedSecret: string;      // SHA256 hash of the secret
  timeoutHeight: number;     // Block height timeout
  timeoutTimestamp: number;  // Unix timestamp timeout
  senderAddress: string;     // Sender's address
  receiverAddress: string;   // Receiver's address
}

// Price data
export interface PricePoint {
  timestamp: number;         // Unix timestamp
  price: string;            // Price as BigNumber string
  volume?: string;          // Volume as BigNumber string
  source: string;           // Price source (coingecko, chainlink, etc.)
}

// Order statuses
export enum OrderStatus {
  PENDING = 'pending',
  EXECUTING = 'executing',
  PARTIALLY_FILLED = 'partially_filled',
  COMPLETED = 'completed',
  CANCELLED = 'cancelled',
  EXPIRED = 'expired',
  REFUNDED = 'refunded',
  CLAIMED = 'claimed',
}

// Execution record
export interface ExecutionRecord {
  timestamp: number;
  amount: string;           // Amount executed as BigNumber string
  price: string;           // Execution price as BigNumber string
  txHash: string;          // Transaction hash
  gasUsed?: string;        // Gas used
  slippage: number;        // Actual slippage in basis points
  interval: number;        // Execution interval number
}

// Main swap order
export interface SwapOrder {
  id: string;                    // Unique order ID
  user: string;                  // User address
  sourceChain: ChainId;          // Source blockchain
  targetChain: ChainId;          // Target blockchain
  sourceToken: TokenAmount;      // Source token and amount
  targetToken: Token;            // Target token
  minReceived: string;           // Minimum amount to receive
  twapConfig: TWAPConfig;        // TWAP configuration
  htlcParams: HTLCParams;        // HTLC parameters
  status: OrderStatus;           // Current order status
  createdAt: number;             // Creation timestamp
  expiresAt: number;             // Expiration timestamp
  executedAmount: string;        // Amount already executed
  executionHistory: ExecutionRecord[]; // Execution history
  metadata?: OrderMetadata;      // Additional metadata
}

export interface OrderMetadata {
  userAgent?: string;
  ipAddress?: string;
  referrer?: string;
  estimatedGas?: string;
  estimatedTime?: number;
}

// Chain configuration
export interface ChainConfig {
  chainId: ChainId;
  network: NetworkType;
  name: string;
  displayName: string;
  nativeCurrency: {
    name: string;
    symbol: string;
    decimals: number;
  };
  rpcUrls: string[];
  blockExplorerUrls?: string[];
  contractAddresses: {
    bridge?: string;
    twap?: string;
    htlc?: string;
    multicall?: string;
  };
  features: {
    supportsTWAP: boolean;
    supportsHTLC: boolean;
    supportsPartialFills: boolean;
    averageBlockTime: number; // in seconds
    finalityBlocks: number;
  };
}

// Event types
export enum EventType {
  ORDER_CREATED = 'order_created',
  ORDER_UPDATED = 'order_updated',
  ORDER_EXECUTED = 'order_executed',
  ORDER_CANCELLED = 'order_cancelled',
  ORDER_COMPLETED = 'order_completed',
  HTLC_CREATED = 'htlc_created',
  HTLC_CLAIMED = 'htlc_claimed',
  HTLC_REFUNDED = 'htlc_refunded',
  PRICE_UPDATE = 'price_update',
  TWAP_CALCULATION = 'twap_calculation',
}

// Chain events
export interface ChainEvent {
  id: string;
  chainId: ChainId;
  type: EventType;
  blockNumber: number;
  transactionHash: string;
  timestamp: number;
  data: any;
  logIndex?: number;
}

// Error codes
export enum ErrorCode {
  // Chain errors
  CHAIN_NOT_SUPPORTED = 'CHAIN_NOT_SUPPORTED',
  CHAIN_CONNECTION_FAILED = 'CHAIN_CONNECTION_FAILED',
  
  // Order errors
  ORDER_NOT_FOUND = 'ORDER_NOT_FOUND',
  ORDER_ALREADY_EXISTS = 'ORDER_ALREADY_EXISTS',
  ORDER_EXPIRED = 'ORDER_EXPIRED',
  ORDER_CANCELLED = 'ORDER_CANCELLED',
  
  // Balance errors
  INSUFFICIENT_BALANCE = 'INSUFFICIENT_BALANCE',
  INSUFFICIENT_LIQUIDITY = 'INSUFFICIENT_LIQUIDITY',
  
  // TWAP errors
  SLIPPAGE_TOO_HIGH = 'SLIPPAGE_TOO_HIGH',
  PRICE_IMPACT_TOO_HIGH = 'PRICE_IMPACT_TOO_HIGH',
  TWAP_WINDOW_TOO_SMALL = 'TWAP_WINDOW_TOO_SMALL',
  TWAP_INTERVALS_INVALID = 'TWAP_INTERVALS_INVALID',
  
  // HTLC errors
  HTLC_NOT_FOUND = 'HTLC_NOT_FOUND',
  HTLC_EXPIRED = 'HTLC_EXPIRED',
  HTLC_ALREADY_CLAIMED = 'HTLC_ALREADY_CLAIMED',
  INVALID_SECRET = 'INVALID_SECRET',
  
  // Technical errors
  INVALID_PARAMETERS = 'INVALID_PARAMETERS',
  NETWORK_ERROR = 'NETWORK_ERROR',
  CONTRACT_ERROR = 'CONTRACT_ERROR',
  TIMEOUT_ERROR = 'TIMEOUT_ERROR',
  PARSING_ERROR = 'PARSING_ERROR',
}

// Custom error class
export class FlowFusionError extends Error {
  public readonly code: ErrorCode;
  public readonly chainId?: ChainId;
  public readonly details?: any;

  constructor(
    code: ErrorCode,
    message: string,
    chainId?: ChainId,
    details?: any
  ) {
    super(message);
    this.name = 'FlowFusionError';
    this.code = code;
    this.chainId = chainId;
    this.details = details;
  }
}

// Chain adapter interface
export interface ChainAdapter {
  readonly chainId: ChainId;
  readonly isConnected: boolean;
  
  // Connection management
  connect(): Promise<void>;
  disconnect(): Promise<void>;
  
  // Account management
  getAddress(): Promise<string>;
  getBalance(token: Token): Promise<string>;
  
  // TWAP operations
  createTWAPOrder(order: SwapOrder): Promise<string>;
  executeTWAPInterval(orderId: string, amount: string, price: string): Promise<string>;
  cancelOrder(orderId: string): Promise<string>;
  getOrder(orderId: string): Promise<SwapOrder | null>;
  
  // HTLC operations
  createHTLC(params: HTLCParams, amount: string, token: Token): Promise<string>;
  claimHTLC(secret: string, htlcAddress: string): Promise<string>;
  refundHTLC(htlcAddress: string): Promise<string>;
  getHTLCInfo(htlcAddress: string): Promise<HTLCInfo | null>;
  
  // Price operations
  getPrice(tokenPair: string): Promise<PricePoint>;
  getTWAPPrice(tokenPair: string, windowMinutes: number): Promise<string>;
  
  // Event management
  subscribeToEvents(callback: (event: ChainEvent) => void): Promise<void>;
  unsubscribeFromEvents(): Promise<void>;
}

export interface HTLCInfo {
  address: string;
  hashedSecret: string;
  amount: string;
  token: Token;
  sender: string;
  receiver: string;
  timeoutHeight: number;
  timeoutTimestamp: number;
  status: HTLCStatus;
  createdAt: number;
}

export enum HTLCStatus {
  ACTIVE = 'active',
  CLAIMED = 'claimed',
  REFUNDED = 'refunded',
  EXPIRED = 'expired',
}

// API Response types
export interface APIResponse<T = any> {
  success: boolean;
  data?: T;
  error?: {
    code: ErrorCode;
    message: string;
    details?: any;
  };
  timestamp: number;
}

export interface PaginatedResponse<T> extends APIResponse<T[]> {
  pagination: {
    page: number;
    limit: number;
    total: number;
    totalPages: number;
  };
}

// Utility functions
export class TWAPUtils {
  static validateConfig(config: TWAPConfig): void {
    if (config.windowMinutes < 5 || config.windowMinutes > 1440) {
      throw new FlowFusionError(
        ErrorCode.TWAP_WINDOW_TOO_SMALL,
        'TWAP window must be between 5 and 1440 minutes'
      );
    }
    
    if (config.executionIntervals < 2 || config.executionIntervals > 20) {
      throw new FlowFusionError(
        ErrorCode.TWAP_INTERVALS_INVALID,
        'Execution intervals must be between 2 and 20'
      );
    }
    
    if (config.maxSlippage < 1 || config.maxSlippage > 1000) {
      throw new FlowFusionError(
        ErrorCode.SLIPPAGE_TOO_HIGH,
        'Max slippage must be between 1 and 1000 basis points'
      );
    }
  }

  static calculateIntervalDuration(config: TWAPConfig): number {
    return (config.windowMinutes * 60) / config.executionIntervals;
  }

  static calculateTWAP(pricePoints: PricePoint[], windowMinutes: number): string {
    if (pricePoints.length === 0) return '0';
    
    const now = Date.now() / 1000;
    const windowStart = now - (windowMinutes * 60);
    
    const filteredPoints = pricePoints.filter(p => p.timestamp >= windowStart);
    if (filteredPoints.length === 0) return '0';
    
    let totalValue = new BigNumber(0);
    let totalWeight = new BigNumber(0);
    
    for (let i = 0; i < filteredPoints.length; i++) {
      const point = filteredPoints[i];
      const weight = i > 0 
        ? new BigNumber(point.timestamp - filteredPoints[i-1].timestamp)
        : new BigNumber(1);
      
      totalValue = totalValue.plus(new BigNumber(point.price).times(weight));
      totalWeight = totalWeight.plus(weight);
    }
    
    return totalWeight.isZero() ? '0' : totalValue.div(totalWeight).toString();
  }
}

export class BigNumberUtils {
  static fromWei(value: string, decimals: number = 18): string {
    return new BigNumber(value).div(new BigNumber(10).pow(decimals)).toString();
  }

  static toWei(value: string, decimals: number = 18): string {
    return new BigNumber(value).times(new BigNumber(10).pow(decimals)).toFixed(0);
  }

  static formatAmount(amount: string, decimals: number = 18, precision: number = 6): string {
    const bn = new BigNumber(amount);
    if (bn.isZero()) return '0';
    
    const formatted = this.fromWei(amount, decimals);
    const truncated = new BigNumber(formatted).toFixed(precision);
    
    // Remove trailing zeros
    return truncated.replace(/\.?0+$/, '');
  }
}

// Constants
export const SUPPORTED_CHAINS: ChainConfig[] = [
  {
    chainId: ChainId.ETHEREUM,
    network: NetworkType.TESTNET,
    name: 'sepolia',
    displayName: 'Ethereum Sepolia',
    nativeCurrency: {
      name: 'Ether',
      symbol: 'ETH',
      decimals: 18,
    },
    rpcUrls: ['https://sepolia.infura.io/v3/'],
    blockExplorerUrls: ['https://sepolia.etherscan.io'],
    contractAddresses: {
      bridge: process.env.ETHEREUM_BRIDGE_ADDRESS,
    },
    features: {
      supportsTWAP: true,
      supportsHTLC: true,
      supportsPartialFills: true,
      averageBlockTime: 12,
      finalityBlocks: 12,
    },
  },
  {
    chainId: ChainId.COSMOS,
    network: NetworkType.TESTNET,
    name: 'theta-testnet',
    displayName: 'Cosmos Theta Testnet',
    nativeCurrency: {
      name: 'Cosmos',
      symbol: 'ATOM',
      decimals: 6,
    },
    rpcUrls: ['https://rpc.sentry-02.theta-testnet.polypore.xyz'],
    contractAddresses: {
      bridge: process.env.COSMOS_BRIDGE_ADDRESS,
    },
    features: {
      supportsTWAP: true,
      supportsHTLC: true,
      supportsPartialFills: true,
      averageBlockTime: 6,
      finalityBlocks: 1,
    },
  },
];

export const BASIS_POINTS_DIVISOR = 10000;
export const MAX_UINT256 = '0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff';
export const ZERO_ADDRESS = '0x0000000000000000000000000000000000000000';
export const ZERO_HASH = '0x0000000000000000000000000000000000000000000000000000000000000000';
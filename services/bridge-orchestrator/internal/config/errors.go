package config

import "errors"

// Configuration validation errors
var (
	ErrMissingEthereumRPC        = errors.New("ethereum RPC URL is required")
	ErrMissingEthereumPrivateKey = errors.New("ethereum private key is required")
	ErrMissingCosmosRPC          = errors.New("cosmos RPC URL is required")
	ErrMissingCosmosMnemonic     = errors.New("cosmos mnemonic is required")
	ErrMissingStellarHorizon     = errors.New("stellar horizon URL is required")
	ErrMissingStellarSecretKey   = errors.New("stellar secret key is required")
	ErrInvalidTWAPWindow         = errors.New("invalid TWAP window configuration")
	ErrInvalidSlippage           = errors.New("invalid slippage configuration")
	ErrUnsupportedChain          = errors.New("unsupported blockchain")
)
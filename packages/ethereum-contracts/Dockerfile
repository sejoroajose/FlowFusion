FROM node:18-alpine AS builder

# Install yarn and build dependencies
RUN apk add --no-cache python3 make g++ git && \
    corepack enable

# Set working directory
WORKDIR /app

# Copy workspace root files (yarn.lock is in root)
COPY package.json yarn.lock* .yarnrc.yml* ./
COPY .yarn ./.yarn

# Copy all workspace package.json files for dependency resolution
COPY packages/cores/package.json ./packages/cores/
COPY packages/ethereum-contracts/package.json ./packages/ethereum-contracts/

# Install all workspace dependencies
RUN yarn install --frozen-lockfile

# Copy source code for ethereum-contracts package
COPY packages/ethereum-contracts ./packages/ethereum-contracts

# Change to ethereum-contracts directory and compile
WORKDIR /app/packages/ethereum-contracts
RUN yarn compile

# Production stage
FROM node:18-alpine

# Install yarn and runtime dependencies
RUN apk add --no-cache curl && \
    corepack enable

# Create non-root user
RUN addgroup -g 1001 -S contractgroup && \
    adduser -u 1001 -S contractuser -G contractgroup

# Set working directory
WORKDIR /app

# Copy workspace configuration
COPY package.json yarn.lock* .yarnrc.yml* ./
COPY .yarn ./.yarn

# Copy package.json files
COPY packages/cores/package.json ./packages/cores/
COPY packages/ethereum-contracts/package.json ./packages/ethereum-contracts/

# Install production dependencies for ethereum-contracts workspace
RUN yarn workspaces focus --production @flowfusion/ethereum-contracts

# Copy built artifacts and necessary files
COPY --from=builder /app/packages/ethereum-contracts/artifacts ./artifacts
COPY --from=builder /app/packages/ethereum-contracts/cache ./cache
COPY --from=builder /app/packages/ethereum-contracts/contracts ./contracts
COPY --from=builder /app/packages/ethereum-contracts/hardhat.config.js ./hardhat.config.js

# Copy deployment scripts
COPY --from=builder /app/packages/ethereum-contracts/contracts/deploy ./contracts/deploy

# Change ownership
RUN chown -R contractuser:contractgroup /app
USER contractuser

# Expose port for development server
EXPOSE 8545

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
  CMD curl -f http://localhost:8545 || exit 1

# Default command
CMD ["yarn", "hardhat", "node", "--hostname", "0.0.0.0"]
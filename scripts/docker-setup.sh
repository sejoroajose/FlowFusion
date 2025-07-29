#!/bin/bash
set -e

echo "🧶 Setting up FlowFusion Docker Environment with Yarn"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo -e "${RED}❌ Docker is not installed. Please install Docker first.${NC}"
    exit 1
fi

# Check if Docker Compose is installed
if ! command -v docker-compose &> /dev/null; then
    echo -e "${RED}❌ Docker Compose is not installed. Please install Docker Compose first.${NC}"
    exit 1
fi

# Check if Yarn is installed
if ! command -v yarn &> /dev/null; then
    echo -e "${YELLOW}📦 Installing Yarn globally...${NC}"
    npm install -g yarn
fi

# Verify yarn version
YARN_VERSION=$(yarn --version)
echo -e "${BLUE}📦 Using Yarn version: ${YARN_VERSION}${NC}"

# Create .env file if it doesn't exist
if [ ! -f .env ]; then
    echo -e "${YELLOW}📝 Creating .env file from template...${NC}"
    cp .env.example .env
    echo -e "${YELLOW}⚠️  Please edit .env file with your actual configuration values${NC}"
fi

# Create necessary directories
echo -e "${BLUE}📁 Creating necessary directories...${NC}"
mkdir -p logs
mkdir -p deployments
mkdir -p monitoring/grafana/dashboards
mkdir -p monitoring/grafana/datasources

# Install workspace dependencies if using workspaces
if [ -f "package.json" ] && grep -q "workspaces" package.json; then
    echo -e "${BLUE}📦 Installing workspace dependencies...${NC}"
    yarn install
fi

# Generate yarn.lock files for packages
echo -e "${BLUE}📦 Ensuring yarn.lock files exist...${NC}"

if [ -d "packages/ethereum-contracts" ]; then
    cd packages/ethereum-contracts
    if [ ! -f "yarn.lock" ]; then
        echo -e "${YELLOW}📦 Generating yarn.lock for ethereum-contracts...${NC}"
        yarn install
    fi
    cd ../..
fi

if [ -d "packages/cores" ]; then
    cd packages/cores
    if [ ! -f "yarn.lock" ]; then
        echo -e "${YELLOW}📦 Generating yarn.lock for cores...${NC}"
        yarn install
    fi
    cd ../..
fi

# Build images
echo -e "${BLUE}🏗️  Building Docker images...${NC}"
docker-compose build --parallel

# Start services
echo -e "${BLUE}🚀 Starting services...${NC}"
docker-compose up -d postgres redis

# Wait for database to be ready
echo -e "${BLUE}⏳ Waiting for database to be ready...${NC}"
until docker-compose exec postgres pg_isready -U flowfusion -d flowfusion_dev; do
    echo "Waiting for postgres..."
    sleep 2
done

# Start remaining services
echo -e "${BLUE}🚀 Starting remaining services...${NC}"
docker-compose up -d

# Wait for services to be healthy
echo -e "${BLUE}⏳ Waiting for services to be healthy...${NC}"
sleep 10

# Check service health
echo -e "${BLUE}🔍 Checking service health...${NC}"
docker-compose ps

# Show useful information
echo -e "${GREEN}✅ FlowFusion Docker environment is ready!${NC}"
echo ""
echo -e "${BLUE}🌐 Service URLs:${NC}"
echo "  Bridge Orchestrator API: http://localhost:8080"
echo "  Health Check: http://localhost:8080/health"
echo "  API Documentation: http://localhost:8080/api/v1"
echo "  Ethereum Node: http://localhost:8545"
echo "  PostgreSQL: localhost:5432"
echo "  Redis: localhost:6379"
echo ""
echo -e "${YELLOW}📝 Next Steps:${NC}"
echo "1. Edit .env file with your configuration"
echo "2. Deploy smart contracts: docker-compose exec ethereum-contracts yarn deploy:local"
echo "3. Test the API: curl http://localhost:8080/health"
echo ""
echo -e "${BLUE}🛠️  Useful Commands (Yarn):${NC}"
echo "  View logs: docker-compose logs -f [service-name]"
echo "  Stop services: docker-compose down"
echo "  Restart service: docker-compose restart [service-name]"
echo "  Execute command: docker-compose exec [service-name] [command]"
echo "  Deploy contracts: docker-compose exec ethereum-contracts yarn deploy:local"
echo "  Run tests: docker-compose exec ethereum-contracts yarn test"
echo "  Install dependencies: docker-compose exec ethereum-contracts yarn install"

---
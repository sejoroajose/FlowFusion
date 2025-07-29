#!/bin/bash
set -e

echo "🧹 Cleaning up FlowFusion Docker Environment"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Ask for confirmation
read -p "$(echo -e ${YELLOW}Are you sure you want to stop and remove all containers, networks, and volumes? [y/N]: ${NC})" -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${BLUE}Cleanup cancelled.${NC}"
    exit 1
fi

# Stop all services
echo -e "${BLUE}🛑 Stopping all services...${NC}"
docker-compose down --remove-orphans

# Remove volumes (ask for confirmation)
read -p "$(echo -e ${YELLOW}Do you want to remove all data volumes (database, etc.)? [y/N]: ${NC})" -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${BLUE}🗑️  Removing volumes...${NC}"
    docker-compose down --volumes
    docker volume prune -f
fi

# Clean up Docker system
echo -e "${BLUE}🧽 Cleaning up Docker system...${NC}"
docker system prune -f

# Remove built images (optional)
read -p "$(echo -e ${YELLOW}Do you want to remove built images? [y/N]: ${NC})" -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${BLUE}🗑️  Removing images...${NC}"
    docker-compose down --rmi all
fi

echo -e "${GREEN}✅ Cleanup completed!${NC}"

---
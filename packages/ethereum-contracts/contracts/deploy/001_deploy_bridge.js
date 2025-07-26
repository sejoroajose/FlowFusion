const { ethers } = require("hardhat");

module.exports = async ({ getNamedAccounts, deployments, getChainId }) => {
  const { deploy, log } = deployments;
  const { deployer } = await getNamedAccounts();
  const chainId = await getChainId();

  log("🌊 Deploying FlowFusion Bridge...");
  log(`Network: ${chainId}`);
  log(`Deployer: ${deployer}`);

  // Deploy configuration
  const feeCollector = deployer; // Use deployer as initial fee collector
  const protocolFeeRate = 25; // 0.25% protocol fee

  // Deploy the main bridge contract
  const bridgeDeployment = await deploy("FlowFusionBridge", {
    from: deployer,
    args: [feeCollector, protocolFeeRate],
    log: true,
    waitConfirmations: chainId === "1" ? 5 : 1, // Wait more confirmations on mainnet
  });

  log(`✅ FlowFusionBridge deployed to: ${bridgeDeployment.address}`);

  // Get the deployed contract instance
  const bridge = await ethers.getContractAt("FlowFusionBridge", bridgeDeployment.address);

  // Add deployer as authorized executor for testing
  if (chainId !== "1") { // Not on mainnet
    log("🔧 Setting up test configuration...");
    
    try {
      const tx = await bridge.addAuthorizedExecutor(deployer);
      await tx.wait();
      log(`✅ Added ${deployer} as authorized executor`);
    } catch (error) {
      log(`⚠️  Warning: Could not add authorized executor: ${error.message}`);
    }
  }

  // Verify contract on Etherscan (if API key provided)
  if (process.env.ETHERSCAN_API_KEY && chainId !== "31337" && chainId !== "1337") {
    log("📄 Verifying contract on Etherscan...");
    try {
      await run("verify:verify", {
        address: bridgeDeployment.address,
        constructorArguments: [feeCollector, protocolFeeRate],
      });
      log("✅ Contract verified on Etherscan");
    } catch (error) {
      log(`⚠️  Verification failed: ${error.message}`);
    }
  }

  // Log important information
  log("\n📋 Deployment Summary:");
  log(`📍 Bridge Address: ${bridgeDeployment.address}`);
  log(`💰 Fee Collector: ${feeCollector}`);
  log(`📊 Protocol Fee: ${protocolFeeRate / 100}%`);
  log(`⛽ Gas Used: ${bridgeDeployment.receipt.gasUsed}`);
  
  // Save deployment info to file for other services
  const deploymentInfo = {
    network: chainId,
    contractAddress: bridgeDeployment.address,
    feeCollector: feeCollector,
    protocolFeeRate: protocolFeeRate,
    deployedAt: new Date().toISOString(),
    deployer: deployer,
    gasUsed: bridgeDeployment.receipt.gasUsed.toString(),
  };

  const fs = require("fs");
  const path = require("path");
  
  // Ensure deployments directory exists
  const deploymentsDir = path.join(__dirname, "../../../deployments");
  if (!fs.existsSync(deploymentsDir)) {
    fs.mkdirSync(deploymentsDir, { recursive: true });
  }
  
  // Write deployment info
  fs.writeFileSync(
    path.join(deploymentsDir, `ethereum-${chainId}.json`),
    JSON.stringify(deploymentInfo, null, 2)
  );

  log(`📁 Deployment info saved to deployments/ethereum-${chainId}.json`);
  log("🎉 FlowFusion Bridge deployment complete!\n");
};

module.exports.tags = ["FlowFusionBridge", "bridge", "main"];
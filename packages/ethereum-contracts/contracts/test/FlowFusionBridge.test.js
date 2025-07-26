const { expect } = require("chai");
const { ethers } = require("hardhat");
const { loadFixture } = require("@nomicfoundation/hardhat-network-helpers");

describe("FlowFusionBridge", function () {
  // Fixture to deploy the contract and setup initial state
  async function deployBridgeFixture() {
    const [owner, user, executor, feeCollector] = await ethers.getSigners();

    // Deploy the bridge contract
    const FlowFusionBridge = await ethers.getContractFactory("FlowFusionBridge");
    const bridge = await FlowFusionBridge.deploy(feeCollector.address, 25); // 0.25% fee

    // Add executor authorization
    await bridge.addAuthorizedExecutor(executor.address);

    // Deploy a test ERC20 token
    const TestToken = await ethers.getContractFactory("contracts/test/TestERC20.sol:TestERC20");
    const token = await TestToken.deploy("Test Token", "TEST", ethers.parseEther("1000000"));

    // Transfer some tokens to user
    await token.transfer(user.address, ethers.parseEther("10000"));

    return { bridge, token, owner, user, executor, feeCollector };
  }

  describe("Deployment", function () {
    it("Should deploy with correct initial settings", async function () {
      const { bridge, feeCollector } = await loadFixture(deployBridgeFixture);

      expect(await bridge.feeCollector()).to.equal(feeCollector.address);
      expect(await bridge.protocolFeeRate()).to.equal(25);
      expect(await bridge.totalOrders()).to.equal(0);
      expect(await bridge.supportedChains("cosmos")).to.be.true;
      expect(await bridge.supportedChains("stellar")).to.be.true;
    });

    it("Should set owner correctly", async function () {
      const { bridge, owner } = await loadFixture(deployBridgeFixture);
      expect(await bridge.owner()).to.equal(owner.address);
    });
  });

  describe("Order Creation", function () {
    it("Should create a TWAP order with ETH", async function () {
      const { bridge, user } = await loadFixture(deployBridgeFixture);

      const sourceAmount = ethers.parseEther("1");
      const twapConfig = {
        windowMinutes: 60,
        executionIntervals: 6,
        maxSlippage: 100, // 1%
        minFillSize: ethers.parseEther("0.1"),
        enableMEVProtection: true
      };
      const htlcHash = ethers.keccak256(ethers.toUtf8Bytes("secret123"));
      const timeoutHeight = (await ethers.provider.getBlockNumber()) + 1000;

      const tx = await bridge.connect(user).createTWAPOrder(
        ethers.ZeroAddress, // ETH
        sourceAmount,
        "cosmos",
        "uatom",
        "cosmos1recipient",
        twapConfig,
        htlcHash,
        timeoutHeight,
        { value: sourceAmount }
      );

      const receipt = await tx.wait();
      const event = receipt.logs.find(log => log.fragment?.name === "OrderCreated");
      expect(event).to.not.be.undefined;

      const orderId = event.args[0];
      const order = await bridge.getOrder(orderId);

      expect(order.user).to.equal(user.address);
      expect(order.sourceToken).to.equal(ethers.ZeroAddress);
      expect(order.sourceAmount).to.equal(sourceAmount);
      expect(order.targetChain).to.equal("cosmos");
      expect(order.status).to.equal(0); // OrderStatus.Executing
    });

    it("Should create a TWAP order with ERC20 token", async function () {
      const { bridge, token, user } = await loadFixture(deployBridgeFixture);

      const sourceAmount = ethers.parseEther("100");
      
      // Approve token transfer
      await token.connect(user).approve(bridge.target, sourceAmount);

      const twapConfig = {
        windowMinutes: 30,
        executionIntervals: 3,
        maxSlippage: 50,
        minFillSize: ethers.parseEther("10"),
        enableMEVProtection: true
      };
      const htlcHash = ethers.keccak256(ethers.toUtf8Bytes("secret456"));
      const timeoutHeight = (await ethers.provider.getBlockNumber()) + 500;

      await expect(
        bridge.connect(user).createTWAPOrder(
          token.target,
          sourceAmount,
          "stellar",
          "USDC",
          "GABC123RECIPIENT",
          twapConfig,
          htlcHash,
          timeoutHeight
        )
      ).to.emit(bridge, "OrderCreated");

      // Check that tokens were transferred to bridge
      expect(await token.balanceOf(bridge.target)).to.equal(sourceAmount);
    });

    it("Should revert with invalid TWAP config", async function () {
      const { bridge, user } = await loadFixture(deployBridgeFixture);

      const invalidTwapConfig = {
        windowMinutes: 2, // Too small
        executionIntervals: 1, // Too few
        maxSlippage: 2000, // Too high
        minFillSize: 0, // Too small
        enableMEVProtection: true
      };
      const htlcHash = ethers.keccak256(ethers.toUtf8Bytes("secret"));
      const timeoutHeight = (await ethers.provider.getBlockNumber()) + 100;

      await expect(
        bridge.connect(user).createTWAPOrder(
          ethers.ZeroAddress,
          ethers.parseEther("1"),
          "cosmos",
          "uatom",
          "cosmos1recipient",
          invalidTwapConfig,
          htlcHash,
          timeoutHeight,
          { value: ethers.parseEther("1") }
        )
      ).to.be.revertedWith("Window too small");
    });
  });

  describe("TWAP Execution", function () {
    it("Should execute TWAP interval correctly", async function () {
      const { bridge, user, executor } = await loadFixture(deployBridgeFixture);

      // Create order first
      const sourceAmount = ethers.parseEther("1");
      const twapConfig = {
        windowMinutes: 60,
        executionIntervals: 6,
        maxSlippage: 100,
        minFillSize: ethers.parseEther("0.1"),
        enableMEVProtection: true
      };
      const htlcHash = ethers.keccak256(ethers.toUtf8Bytes("secret123"));
      const timeoutHeight = (await ethers.provider.getBlockNumber()) + 1000;

      const tx = await bridge.connect(user).createTWAPOrder(
        ethers.ZeroAddress,
        sourceAmount,
        "cosmos",
        "uatom",
        "cosmos1recipient",
        twapConfig,
        htlcHash,
        timeoutHeight,
        { value: sourceAmount }
      );

      const receipt = await tx.wait();
      const event = receipt.logs.find(log => log.fragment?.name === "OrderCreated");
      const orderId = event.args[0];

      // Execute first interval
      const intervalAmount = ethers.parseEther("0.166"); // ~1/6 of total
      const executionPrice = ethers.parseEther("2000"); // $2000 per ETH
      const priceProof = "0x1234"; // Mock proof

      await expect(
        bridge.connect(executor).executeTWAPInterval(
          orderId,
          intervalAmount,
          executionPrice,
          priceProof
        )
      ).to.emit(bridge, "TWAPExecution");

      // Check order state
      const order = await bridge.getOrder(orderId);
      expect(order.executedAmount).to.equal(intervalAmount);
      expect(order.averagePrice).to.equal(executionPrice);

      // Check execution history
      const history = await bridge.getExecutionHistory(orderId);
      expect(history.length).to.equal(1);
      expect(history[0].amount).to.equal(intervalAmount);
      expect(history[0].price).to.equal(executionPrice);
    });

    it("Should revert if not authorized executor", async function () {
      const { bridge, user } = await loadFixture(deployBridgeFixture);

      // Try to execute without being authorized
      await expect(
        bridge.connect(user).executeTWAPInterval(
          ethers.keccak256(ethers.toUtf8Bytes("fake")),
          ethers.parseEther("0.1"),
          ethers.parseEther("2000"),
          "0x1234"
        )
      ).to.be.revertedWith("Not authorized executor");
    });
  });

  describe("Order Management", function () {
    it("Should allow user to cancel their order", async function () {
      const { bridge, user } = await loadFixture(deployBridgeFixture);

      // Create order
      const sourceAmount = ethers.parseEther("1");
      const twapConfig = {
        windowMinutes: 60,
        executionIntervals: 6,
        maxSlippage: 100,
        minFillSize: ethers.parseEther("0.1"),
        enableMEVProtection: true
      };
      const htlcHash = ethers.keccak256(ethers.toUtf8Bytes("secret123"));
      const timeoutHeight = (await ethers.provider.getBlockNumber()) + 1000;

      const tx = await bridge.connect(user).createTWAPOrder(
        ethers.ZeroAddress,
        sourceAmount,
        "cosmos",
        "uatom",
        "cosmos1recipient",
        twapConfig,
        htlcHash,
        timeoutHeight,
        { value: sourceAmount }
      );

      const receipt = await tx.wait();
      const event = receipt.logs.find(log => log.fragment?.name === "OrderCreated");
      const orderId = event.args[0];

      // Check initial balance
      const initialBalance = await ethers.provider.getBalance(user.address);

      // Cancel order
      await expect(bridge.connect(user).cancelOrder(orderId))
        .to.emit(bridge, "OrderCancelled");

      // Check refund
      const finalBalance = await ethers.provider.getBalance(user.address);
      expect(finalBalance).to.be.gt(initialBalance);

      // Check order status
      const order = await bridge.getOrder(orderId);
      expect(order.status).to.equal(2); // OrderStatus.Cancelled
    });

    it("Should get user orders correctly", async function () {
      const { bridge, user } = await loadFixture(deployBridgeFixture);

      // Initially no orders
      let userOrders = await bridge.getUserOrders(user.address);
      expect(userOrders.length).to.equal(0);

      // Create an order
      const sourceAmount = ethers.parseEther("1");
      const twapConfig = {
        windowMinutes: 60,
        executionIntervals: 6,
        maxSlippage: 100,
        minFillSize: ethers.parseEther("0.1"),
        enableMEVProtection: true
      };
      const htlcHash = ethers.keccak256(ethers.toUtf8Bytes("secret123"));
      const timeoutHeight = (await ethers.provider.getBlockNumber()) + 1000;

      await bridge.connect(user).createTWAPOrder(
        ethers.ZeroAddress,
        sourceAmount,
        "cosmos",
        "uatom",
        "cosmos1recipient",
        twapConfig,
        htlcHash,
        timeoutHeight,
        { value: sourceAmount }
      );

      // Now should have one order
      userOrders = await bridge.getUserOrders(user.address);
      expect(userOrders.length).to.equal(1);
    });
  });

  describe("Admin Functions", function () {
    it("Should allow owner to add/remove supported chains", async function () {
      const { bridge, owner } = await loadFixture(deployBridgeFixture);

      // Add new chain
      await bridge.connect(owner).addSupportedChain("polygon");
      expect(await bridge.supportedChains("polygon")).to.be.true;

      // Remove chain
      await bridge.connect(owner).removeSupportedChain("polygon");
      expect(await bridge.supportedChains("polygon")).to.be.false;
    });

    it("Should allow owner to manage executors", async function () {
      const { bridge, owner, user } = await loadFixture(deployBridgeFixture);

      // Add executor
      await bridge.connect(owner).addAuthorizedExecutor(user.address);
      expect(await bridge.authorizedExecutors(user.address)).to.be.true;

      // Remove executor
      await bridge.connect(owner).removeAuthorizedExecutor(user.address);
      expect(await bridge.authorizedExecutors(user.address)).to.be.false;
    });

    it("Should revert if non-owner tries admin functions", async function () {
      const { bridge, user } = await loadFixture(deployBridgeFixture);

      await expect(
        bridge.connect(user).addSupportedChain("polygon")
      ).to.be.revertedWithCustomError(bridge, "OwnableUnauthorizedAccount");

      await expect(
        bridge.connect(user).addAuthorizedExecutor(user.address)
      ).to.be.revertedWithCustomError(bridge, "OwnableUnauthorizedAccount");
    });
  });
});
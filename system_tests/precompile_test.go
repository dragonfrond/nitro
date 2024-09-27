// Copyright 2021-2023, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE

package arbtest

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/solgen/go/mocksgen"
	"github.com/offchainlabs/nitro/solgen/go/precompilesgen"
	"github.com/offchainlabs/nitro/util/arbmath"
)

func TestPurePrecompileMethodCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	arbosVersion := uint64(31)
	builder := NewNodeBuilder(ctx).
		DefaultConfig(t, false).
		WithArbOSVersion(arbosVersion)
	cleanup := builder.Build(t)
	defer cleanup()

	arbSys, err := precompilesgen.NewArbSys(common.HexToAddress("0x64"), builder.L2.Client)
	Require(t, err, "could not deploy ArbSys contract")
	chainId, err := arbSys.ArbChainID(&bind.CallOpts{})
	Require(t, err, "failed to get the ChainID")
	if chainId.Uint64() != params.ArbitrumDevTestChainConfig().ChainID.Uint64() {
		Fatal(t, "Wrong ChainID", chainId.Uint64())
	}

	arbSysArbosVersion, err := arbSys.ArbOSVersion(&bind.CallOpts{})
	Require(t, err)
	if arbSysArbosVersion.Uint64() != 55+arbosVersion { // Nitro versios start at 56
		Fatal(t, "Expected ArbOSVersion 86, got", arbosVersion)
	}

	storageGasAvailable, err := arbSys.GetStorageGasAvailable(&bind.CallOpts{})
	Require(t, err)
	if storageGasAvailable.Cmp(big.NewInt(0)) != 0 {
		Fatal(t, "Expected 0 storage gas available, got", storageGasAvailable)
	}
}

func TestViewLogReverts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	arbDebug, err := precompilesgen.NewArbDebug(common.HexToAddress("0xff"), builder.L2.Client)
	Require(t, err, "could not deploy ArbSys contract")

	err = arbDebug.EventsView(nil)
	if err == nil {
		Fatal(t, "unexpected success")
	}
}

func TestCustomSolidityErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	callOpts := &bind.CallOpts{Context: ctx}
	arbDebug, err := precompilesgen.NewArbDebug(common.HexToAddress("0xff"), builder.L2.Client)
	Require(t, err, "could not bind ArbDebug contract")
	customError := arbDebug.CustomRevert(callOpts, 1024)
	if customError == nil {
		Fatal(t, "customRevert call should have errored")
	}
	observedMessage := customError.Error()
	expectedError := "Custom(1024, This spider family wards off bugs: /\\oo/\\ //\\(oo)//\\ /\\oo/\\, true)"
	// The first error is server side. The second error is client side ABI decoding.
	expectedMessage := fmt.Sprintf("execution reverted: error %v: %v", expectedError, expectedError)
	if observedMessage != expectedMessage {
		Fatal(t, observedMessage)
	}

	arbSys, err := precompilesgen.NewArbSys(arbos.ArbSysAddress, builder.L2.Client)
	Require(t, err, "could not bind ArbSys contract")
	_, customError = arbSys.ArbBlockHash(callOpts, big.NewInt(1e9))
	if customError == nil {
		Fatal(t, "out of range ArbBlockHash call should have errored")
	}
	observedMessage = customError.Error()
	expectedError = "InvalidBlockNumber(1000000000, 1)"
	expectedMessage = fmt.Sprintf("execution reverted: error %v: %v", expectedError, expectedError)
	if observedMessage != expectedMessage {
		Fatal(t, observedMessage)
	}
}

func TestPrecompileErrorGasLeft(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Faucet", ctx)
	_, _, simple, err := mocksgen.DeploySimple(&auth, builder.L2.Client)
	Require(t, err)

	assertNotAllGasConsumed := func(to common.Address, input []byte) {
		gas, err := simple.CheckGasUsed(&bind.CallOpts{Context: ctx}, to, input)
		Require(t, err, "Failed to call CheckGasUsed to precompile", to)
		maxGas := big.NewInt(100_000)
		if arbmath.BigGreaterThan(gas, maxGas) {
			Fatal(t, "Precompile", to, "used", gas, "gas reverting, greater than max expected", maxGas)
		}
	}

	arbSys, err := precompilesgen.ArbSysMetaData.GetAbi()
	Require(t, err)

	arbBlockHash := arbSys.Methods["arbBlockHash"]
	data, err := arbBlockHash.Inputs.Pack(big.NewInt(1e9))
	Require(t, err)
	input := append([]byte{}, arbBlockHash.ID...)
	input = append(input, data...)
	assertNotAllGasConsumed(arbos.ArbSysAddress, input)

	arbDebug, err := precompilesgen.ArbDebugMetaData.GetAbi()
	Require(t, err)
	assertNotAllGasConsumed(common.HexToAddress("0xff"), arbDebug.Methods["legacyError"].ID)
}

func TestArbGasInfoAndArbOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)

	arbOwner, err := precompilesgen.NewArbOwner(common.HexToAddress("0x70"), builder.L2.Client)
	Require(t, err)
	arbGasInfo, err := precompilesgen.NewArbGasInfo(common.HexToAddress("0x6c"), builder.L2.Client)
	Require(t, err)

	// GetL1BaseFeeEstimateInertia test
	inertia := uint64(11)
	tx, err := arbOwner.SetL1BaseFeeEstimateInertia(&auth, inertia)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoInertia, err := arbGasInfo.GetL1BaseFeeEstimateInertia(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoInertia != inertia {
		Fatal(t, "expected inertia to be", inertia, "got", arbGasInfoInertia)
	}

	// GetL1BaseFeeEstimateInertia test, but using a different setter from ArbOwner
	inertia = uint64(12)
	tx, err = arbOwner.SetL1PricingInertia(&auth, inertia)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoInertia, err = arbGasInfo.GetL1BaseFeeEstimateInertia(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoInertia != inertia {
		Fatal(t, "expected inertia to be", inertia, "got", arbGasInfoInertia)
	}

	// GetL1RewardRate test
	perUnitReward := uint64(13)
	tx, err = arbOwner.SetL1PricingRewardRate(&auth, perUnitReward)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoPerUnitReward, err := arbGasInfo.GetL1RewardRate(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoPerUnitReward != perUnitReward {
		Fatal(t, "expected per unit reward to be", perUnitReward, "got", arbGasInfoPerUnitReward)
	}

	// GetL1RewardRecipient test
	rewardRecipient := common.BytesToAddress(crypto.Keccak256([]byte{})[:20])
	tx, err = arbOwner.SetL1PricingRewardRecipient(&auth, rewardRecipient)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoRewardRecipient, err := arbGasInfo.GetL1RewardRecipient(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoRewardRecipient.Cmp(rewardRecipient) != 0 {
		Fatal(t, "expected reward recipient to be", rewardRecipient, "got", arbGasInfoRewardRecipient)
	}

	// GetPricingInertia
	inertia = uint64(14)
	tx, err = arbOwner.SetL2GasPricingInertia(&auth, inertia)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoInertia, err = arbGasInfo.GetPricingInertia(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoInertia != inertia {
		Fatal(t, "expected inertia to be", inertia, "got", arbGasInfoInertia)
	}

	// GetGasBacklogTolerance
	gasTolerance := uint64(15)
	tx, err = arbOwner.SetL2GasBacklogTolerance(&auth, gasTolerance)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoGasTolerance, err := arbGasInfo.GetGasBacklogTolerance(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoGasTolerance != gasTolerance {
		Fatal(t, "expected gas tolerance to be", gasTolerance, "got", arbGasInfoGasTolerance)
	}

	// GetPerBatchGasCharge
	perBatchGasCharge := int64(16)
	tx, err = arbOwner.SetPerBatchGasCharge(&auth, perBatchGasCharge)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoPerBatchGasCharge, err := arbGasInfo.GetPerBatchGasCharge(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoPerBatchGasCharge != perBatchGasCharge {
		Fatal(t, "expected per batch gas charge to be", perBatchGasCharge, "got", arbGasInfoPerBatchGasCharge)
	}

	// GetL1PricingEquilibrationUnits
	equilUnits := big.NewInt(17)
	tx, err = arbOwner.SetL1PricingEquilibrationUnits(&auth, equilUnits)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoEquilUnits, err := arbGasInfo.GetL1PricingEquilibrationUnits(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoEquilUnits.Cmp(equilUnits) != 0 {
		Fatal(t, "expected equilibration units to be", equilUnits, "got", arbGasInfoEquilUnits)
	}

	// GetGasAccountingParams
	speedLimit := uint64(18)
	txGasLimit := uint64(19)
	tx, err = arbOwner.SetSpeedLimit(&auth, speedLimit)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	tx, err = arbOwner.SetMaxTxGasLimit(&auth, txGasLimit)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)
	arbGasInfoSpeedLimit, arbGasInfoPoolSize, arbGasInfoTxGasLimit, err := arbGasInfo.GetGasAccountingParams(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if arbGasInfoSpeedLimit.Cmp(big.NewInt(int64(speedLimit))) != 0 {
		Fatal(t, "expected speed limit to be", speedLimit, "got", arbGasInfoSpeedLimit)
	}
	if arbGasInfoPoolSize.Cmp(big.NewInt(int64(txGasLimit))) != 0 {
		Fatal(t, "expected pool size to be", txGasLimit, "got", arbGasInfoPoolSize)
	}
	if arbGasInfoTxGasLimit.Cmp(big.NewInt(int64(txGasLimit))) != 0 {
		Fatal(t, "expected tx gas limit to be", txGasLimit, "got", arbGasInfoTxGasLimit)
	}

	currTxL1GasFees, err := arbGasInfo.GetCurrentTxL1GasFees(&bind.CallOpts{Context: ctx})
	Require(t, err)
	if currTxL1GasFees == nil {
		Fatal(t, "currTxL1GasFees is nil")
	}
	if currTxL1GasFees.Cmp(big.NewInt(0)) != 1 {
		Fatal(t, "expected currTxL1GasFees to be greater than 0, got", currTxL1GasFees)
	}
}

func TestGetBrotliCompressionLevel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)

	arbOwnerPublic, err := precompilesgen.NewArbOwnerPublic(common.HexToAddress("0x6b"), builder.L2.Client)
	Require(t, err, "could not bind ArbOwner contract")

	arbOwner, err := precompilesgen.NewArbOwner(common.HexToAddress("0x70"), builder.L2.Client)
	Require(t, err, "could not bind ArbOwner contract")

	brotliCompressionLevel := uint64(11)

	// sets brotli compression level
	tx, err := arbOwner.SetBrotliCompressionLevel(&auth, brotliCompressionLevel)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	// retrieves brotli compression level
	callOpts := &bind.CallOpts{Context: ctx}
	retrievedBrotliCompressionLevel, err := arbOwnerPublic.GetBrotliCompressionLevel(callOpts)
	Require(t, err, "failed to call GetBrotliCompressionLevel")
	if retrievedBrotliCompressionLevel != brotliCompressionLevel {
		Fatal(t, "expected brotli compression level to be", brotliCompressionLevel, "got", retrievedBrotliCompressionLevel)
	}
}

func TestScheduleArbosUpgrade(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	builder := NewNodeBuilder(ctx).DefaultConfig(t, false)
	cleanup := builder.Build(t)
	defer cleanup()

	auth := builder.L2Info.GetDefaultTransactOpts("Owner", ctx)

	arbOwnerPublic, err := precompilesgen.NewArbOwnerPublic(common.HexToAddress("0x6b"), builder.L2.Client)
	Require(t, err, "could not bind ArbOwner contract")

	arbOwner, err := precompilesgen.NewArbOwner(common.HexToAddress("0x70"), builder.L2.Client)
	Require(t, err, "could not bind ArbOwner contract")

	callOpts := &bind.CallOpts{Context: ctx}
	scheduled, err := arbOwnerPublic.GetScheduledUpgrade(callOpts)
	Require(t, err, "failed to call GetScheduledUpgrade before scheduling upgrade")
	if scheduled.ArbosVersion != 0 || scheduled.ScheduledForTimestamp != 0 {
		t.Errorf("expected no upgrade to be scheduled, got version %v timestamp %v", scheduled.ArbosVersion, scheduled.ScheduledForTimestamp)
	}

	// Schedule a noop upgrade, which should test GetScheduledUpgrade in the same way an already completed upgrade would.
	tx, err := arbOwner.ScheduleArbOSUpgrade(&auth, 1, 1)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	scheduled, err = arbOwnerPublic.GetScheduledUpgrade(callOpts)
	Require(t, err, "failed to call GetScheduledUpgrade after scheduling noop upgrade")
	if scheduled.ArbosVersion != 0 || scheduled.ScheduledForTimestamp != 0 {
		t.Errorf("expected completed scheduled upgrade to be ignored, got version %v timestamp %v", scheduled.ArbosVersion, scheduled.ScheduledForTimestamp)
	}

	// TODO: Once we have an ArbOS 30, test a real upgrade with it
	// We can't test 11 -> 20 because 11 doesn't have the GetScheduledUpgrade method we want to test
	var testVersion uint64 = 100
	var testTimestamp uint64 = 1 << 62
	tx, err = arbOwner.ScheduleArbOSUpgrade(&auth, 100, 1<<62)
	Require(t, err)
	_, err = builder.L2.EnsureTxSucceeded(tx)
	Require(t, err)

	scheduled, err = arbOwnerPublic.GetScheduledUpgrade(callOpts)
	Require(t, err, "failed to call GetScheduledUpgrade after scheduling upgrade")
	if scheduled.ArbosVersion != testVersion || scheduled.ScheduledForTimestamp != testTimestamp {
		t.Errorf("expected upgrade to be scheduled for version %v timestamp %v, got version %v timestamp %v", testVersion, testTimestamp, scheduled.ArbosVersion, scheduled.ScheduledForTimestamp)
	}
}

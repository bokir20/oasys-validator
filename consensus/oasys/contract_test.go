package oasys

import (
	"context"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/tests"
)

var (
	_environmentAddress  = common.HexToAddress("0x0000000000000000000000000000000000001000")
	_stakeManagerAddress = common.HexToAddress("0x0000000000000000000000000000000000001001")
)

func TestInitializeSystemContracts(t *testing.T) {
	wallets, accounts, err := makeWallets(1)
	if err != nil {
		t.Fatalf("failed to create test wallets: %v", err)
	}

	env, err := makeEnv(*wallets[0], *accounts[0])
	if err != nil {
		t.Fatalf("failed to create test env: %v", err)
	}

	header := &types.Header{
		Number:     big.NewInt(50),
		Coinbase:   accounts[0].Address,
		Difficulty: diffInTurn,
	}
	cx := env.chain
	txs := make([]*types.Transaction, 0)
	receipts := make([]*types.Receipt, 0)
	systemTxs := make([]*types.Transaction, 0)
	usedGas := uint64(0)
	mining := true

	err = env.engine.initializeSystemContracts(env.statedb, header, cx, &txs, &receipts, &systemTxs, &usedGas, mining)
	if err != nil {
		t.Fatalf("failed to call initializeSystemContracts method: %v", err)
	}
	if len(receipts) != 2 {
		t.Errorf("len(receipts), got %v, want 2", len(receipts))
	}
	if usedGas == 0 {
		t.Error("Block.GasUsed is zero")
	}

	environmentInitialized := env.statedb.GetState(_environmentAddress, common.HexToHash("0x00"))
	stakeManagerInitialized := env.statedb.GetState(_stakeManagerAddress, common.HexToHash("0x00"))
	if environmentInitialized.Big().Uint64() != 1 {
		t.Errorf("Environment.initialize not called")
	}
	if stakeManagerInitialized.Big().Uint64() != 1 {
		t.Errorf("StakeManager.initialize not called")
	}

	for _, receipt := range []*types.Receipt{receipts[0], receipts[1]} {
		if receipt.TxHash == (common.Hash{0x00}) {
			t.Error("receipt.TxHash is empty")
		}
		if receipt.GasUsed == 0 {
			t.Error("receipt.GasUsed is zero")
		}
		if len(receipt.Logs) != 1 {
			t.Errorf("len(receipt.Logs), got %v, want 1", len(receipt.Logs))
		}
		if receipt.Bloom == (types.Bloom{}) {
			t.Error("receipt.Bloom is empty")
		}
		if receipt.BlockNumber.Uint64() != 50 {
			t.Errorf("receipt.BlockNumber, got %v, want 50", receipt.BlockNumber)
		}
	}
	if env.statedb.GetNonce(env.engine.signer) != 2 {
		t.Errorf("account nonce value, got %v, want 2", env.statedb.GetNonce(env.engine.signer))
	}
}

func TestSlash(t *testing.T) {
	wallets, accounts, err := makeWallets(1)
	if err != nil {
		t.Fatalf("failed to create test wallets: %v", err)
	}

	env, err := makeEnv(*wallets[0], *accounts[0])
	if err != nil {
		t.Fatalf("failed to create test env: %v", err)
	}

	validator := accounts[0].Address
	schedule := map[uint64]common.Address{}
	header := &types.Header{
		Number:     big.NewInt(50),
		Coinbase:   accounts[0].Address,
		Difficulty: diffInTurn,
	}
	cx := env.chain
	txs := make([]*types.Transaction, 0)
	receipts := make([]*types.Receipt, 0)
	systemTxs := make([]*types.Transaction, 0)
	usedGas := uint64(0)
	mining := true

	err = env.engine.slash(validator, schedule, env.statedb, header, cx, &txs, &receipts, &systemTxs, &usedGas, mining)
	if err != nil {
		t.Fatalf("failed to call slash method: %v", err)
	}
	if len(receipts) != 1 {
		t.Errorf("len(receipts), got %v, want 1", len(receipts))
	}
	if usedGas == 0 {
		t.Error("Block.GasUsed is zero")
	}

	slashed := env.statedb.GetState(_stakeManagerAddress, common.HexToHash("0x01"))
	if slashed.Big().Uint64() != 2 {
		t.Errorf("StakeManager.slash not called")
	}

	receipt := receipts[0]
	if receipt.TxHash == (common.Hash{0x00}) {
		t.Error("receipt.TxHash is empty")
	}
	if receipt.GasUsed == 0 {
		t.Error("receipt.GasUsed is zero")
	}
	if len(receipt.Logs) != 1 {
		t.Errorf("len(receipt.Logs), got %v, want 1", len(receipt.Logs))
	}
	if receipt.Bloom == (types.Bloom{}) {
		t.Error("receipt.Bloom is empty")
	}
	if receipt.BlockNumber.Uint64() != 50 {
		t.Errorf("receipt.BlockNumber, got %v, want 50", receipt.BlockNumber)
	}
	if env.statedb.GetNonce(env.engine.signer) != 1 {
		t.Errorf("account nonce value, got %v, want 1", env.statedb.GetNonce(env.engine.signer))
	}
}

func TestGetNextValidators(t *testing.T) {
	addressArrTy, _ := abi.NewType("address[]", "", nil)
	uint256ArrTy, _ := abi.NewType("uint256[]", "", nil)
	boolArrTy, _ := abi.NewType("bool[]", "", nil)
	uint256Ty, _ := abi.NewType("uint256", "", nil)
	arguments := abi.Arguments{
		{Type: addressArrTy},
		{Type: addressArrTy},
		{Type: uint256ArrTy},
		{Type: boolArrTy},
		{Type: uint256Ty},
	}

	var (
		wantOwners = []common.Address{
			common.HexToAddress("0x01"),
			common.HexToAddress("0x02"),
			common.HexToAddress("0x03"),
		}
		wantOperators = []common.Address{
			common.HexToAddress("0x04"),
			common.HexToAddress("0x05"),
			common.HexToAddress("0x06"),
		}
		wantStakes = []*big.Int{
			big.NewInt(0),
			big.NewInt(1),
			big.NewInt(2),
		}
	)
	var (
		rbytes    = make([][]byte, 7)
		howMany   = 100
		newCursor = howMany
	)
	for i := 0; i < len(rbytes); i++ {
		var (
			owners     = make([]common.Address, howMany)
			operators  = make([]common.Address, howMany)
			stakes     = make([]*big.Int, howMany)
			candidates = make([]bool, howMany)
		)
		for j := 0; j < howMany; j++ {
			if i%len(wantOwners) == 0 && j == howMany/2 {
				idx := i / len(wantOwners)
				owners[j] = wantOwners[idx]
				operators[j] = wantOperators[idx]
				stakes[j] = wantStakes[idx]
				candidates[j] = true
			} else {
				owners[j] = common.Address{}
				operators[j] = common.Address{}
				stakes[j] = big.NewInt(0)
				candidates[j] = false
			}
		}

		rbyte, _ := arguments.Pack(owners, operators, stakes, candidates, big.NewInt(int64(newCursor)))
		rbytes[i] = rbyte

		if i == len(rbytes)-1 {
			rbyte, _ := arguments.Pack([]common.Address{}, []common.Address{}, []*big.Int{}, []bool{}, big.NewInt(int64(newCursor)))
			rbytes = append(rbytes, rbyte)
			break
		}

		newCursor += howMany
	}

	ethapi := &testBlockchainAPI{rbytes: rbytes}
	got, _ := getNextValidators(ethapi, common.Hash{}, 1)
	if len(got.Owners) != len(wantOwners) {
		t.Errorf("invalid owners length, got: %d, want: %d", len(got.Owners), len(wantOwners))
	}
	if len(got.Operators) != len(wantOperators) {
		t.Errorf("invalid operators length, got: %d, want: %d", len(got.Operators), len(wantOperators))
	}
	if len(got.Stakes) != len(wantStakes) {
		t.Errorf("invalid stakes length, got: %d, want: %d", len(got.Stakes), len(wantStakes))
	}
	for i, want := range wantOwners {
		got := got.Owners[i]
		if got != want {
			t.Errorf("invalid owner, got %v, want: %v", got, want)
		}
	}
	for i, want := range wantOperators {
		got := got.Operators[i]
		if got != want {
			t.Errorf("invalid operator, got %v, want: %v", got, want)
		}
	}
	for i, want := range wantStakes {
		got := got.Stakes[i]
		if got.Cmp(want) != 0 {
			t.Errorf("invalid stake, got %v, want: %v", got, want)
		}
	}
}

func TestGetRewards(t *testing.T) {
	want := big.NewInt(1902587519025875190)

	addressArrTy, _ := abi.NewType("address[]", "", nil)
	uint256Ty, _ := abi.NewType("uint256", "", nil)
	rbytes := make([][]byte, 2)

	// mocking to getValidatorOwners method
	rbyte, _ := abi.Arguments{{Type: addressArrTy}, {Type: uint256Ty}}.
		Pack([]common.Address{}, common.Big0)
	rbytes[0] = rbyte

	// mocking to getTotalRewards method
	rbyte, _ = abi.Arguments{{Type: uint256Ty}}.Pack(want)
	rbytes[1] = rbyte

	ethapi := &testBlockchainAPI{rbytes: rbytes}
	got, _ := getRewards(ethapi, common.Hash{})
	if got.Cmp(want) != 0 {
		t.Errorf("got %v, want: %v", got, want)
	}
}

func TestGetNextEnvironmentValue(t *testing.T) {
	want := &environmentValue{
		StartBlock:         common.Big0,
		StartEpoch:         common.Big1,
		BlockPeriod:        big.NewInt(3),
		EpochPeriod:        big.NewInt(20),
		RewardRate:         big.NewInt(10),
		CommissionRate:     big.NewInt(15),
		ValidatorThreshold: new(big.Int).Mul(big.NewInt(params.Ether), big.NewInt(10_000_000)),
		JailThreshold:      big.NewInt(500),
		JailPeriod:         big.NewInt(2),
	}

	uint256Ty, _ := abi.NewType("uint256", "", nil)
	arguments := abi.Arguments{
		{Type: uint256Ty},
		{Type: uint256Ty},
		{Type: uint256Ty},
		{Type: uint256Ty},
		{Type: uint256Ty},
		{Type: uint256Ty},
		{Type: uint256Ty},
		{Type: uint256Ty},
		{Type: uint256Ty},
	}

	rbyte, _ := arguments.Pack(
		want.StartBlock,
		want.StartEpoch,
		want.BlockPeriod,
		want.EpochPeriod,
		want.RewardRate,
		want.CommissionRate,
		want.ValidatorThreshold,
		want.JailThreshold,
		want.JailPeriod,
	)

	ethapi := &testBlockchainAPI{rbytes: [][]byte{rbyte}}
	got, _ := getNextEnvironmentValue(ethapi, common.Hash{})

	if got.StartBlock.Cmp(want.StartBlock) != 0 {
		t.Errorf("StartBlock, got %v, want: %v", got.StartBlock, want.StartBlock)
	}

	if got.StartEpoch.Cmp(want.StartEpoch) != 0 {
		t.Errorf("StartEpoch, got %v, want: %v", got.StartEpoch, want.StartEpoch)
	}

	if got.BlockPeriod.Cmp(want.BlockPeriod) != 0 {
		t.Errorf("BlockPeriod, got %v, want: %v", got.BlockPeriod, want.BlockPeriod)
	}

	if got.EpochPeriod.Cmp(want.EpochPeriod) != 0 {
		t.Errorf("EpochPeriod, got %v, want: %v", got.EpochPeriod, want.EpochPeriod)
	}

	if got.RewardRate.Cmp(want.RewardRate) != 0 {
		t.Errorf("RewardRate, got %v, want: %v", got.RewardRate, want.RewardRate)
	}

	if got.CommissionRate.Cmp(want.CommissionRate) != 0 {
		t.Errorf("CommissionRate, got %v, want: %v", got.CommissionRate, want.CommissionRate)
	}

	if got.ValidatorThreshold.Cmp(want.ValidatorThreshold) != 0 {
		t.Errorf("ValidatorThreshold, got %v, want: %v", got.ValidatorThreshold, want.ValidatorThreshold)
	}

	if got.JailThreshold.Cmp(want.JailThreshold) != 0 {
		t.Errorf("JailThreshold, got %v, want: %v", got.JailThreshold, want.JailThreshold)
	}

	if got.JailPeriod.Cmp(want.JailPeriod) != 0 {
		t.Errorf("JailPeriod, got %v, want: %v", got.JailPeriod, want.JailPeriod)
	}
}

type testBlockchainAPI struct {
	rbytes [][]byte
	count  int
}

func (p *testBlockchainAPI) Call(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, overrides *ethapi.StateOverride) (hexutil.Bytes, error) {
	defer func() { p.count++ }()
	return p.rbytes[p.count], nil
}

type testEnv struct {
	engine  *Oasys
	chain   *core.BlockChain
	statedb *state.StateDB
}

func makeKeyStore() (*keystore.KeyStore, func(), error) {
	d, err := ioutil.TempDir("", "tmp-keystore")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		os.RemoveAll(d)
	}
	return keystore.NewPlaintextKeyStore(d), cleanup, nil
}

func makeWallets(count int) ([]*accounts.Wallet, []*accounts.Account, error) {
	keystore, cleanup, err := makeKeyStore()
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()

	var (
		wallets  []*accounts.Wallet
		accounts []*accounts.Account
	)
	for i := 0; i < count; i++ {
		account, err := keystore.NewAccount("")
		if err != nil {
			return nil, nil, err
		}
		keystore.Unlock(account, "")
		wallets = append(wallets, &keystore.Wallets()[0])
		accounts = append(accounts, &account)
	}
	return wallets, accounts, err
}

func makeEnv(wallet accounts.Wallet, account accounts.Account) (*testEnv, error) {
	var (
		db          = rawdb.NewMemoryDatabase()
		chainConfig = &params.ChainConfig{
			ChainID:             big.NewInt(12345),
			HomesteadBlock:      common.Big0,
			EIP150Block:         common.Big0,
			EIP155Block:         common.Big0,
			EIP158Block:         common.Big0,
			ByzantiumBlock:      common.Big0,
			ConstantinopleBlock: common.Big0,
			PetersburgBlock:     common.Big0,
			IstanbulBlock:       common.Big0,
			MuirGlacierBlock:    common.Big0,
			BerlinBlock:         common.Big0,
			LondonBlock:         common.Big0,
			Oasys: &params.OasysConfig{
				Period: 0,
				Epoch:  100,
			},
		}
		genspec = &core.Genesis{
			ExtraData: make([]byte, extraVanity+common.AddressLength+extraSeal),
			BaseFee:   big.NewInt(params.InitialBaseFee),
			Alloc: map[common.Address]core.GenesisAccount{
				account.Address: {
					Balance: big.NewInt(params.Ether),
				},
				_environmentAddress: {
					/*
						pragma solidity ^0.8.2;

						contract Environment {
						    struct EnvironmentValue {
						        uint256 startBlock;
						        uint256 startEpoch;
						        uint256 blockPeriod;
						        uint256 epochPeriod;
						        uint256 rewardRate;
								uint256 commissionRate;
						        uint256 validatorThreshold;
						        uint256 jailThreshold;
						        uint256 jailPeriod;
						    }

						    event Initialized();

						    address public initialized;

						    function initialize(Environment.EnvironmentValue memory initialValue) external {
						        initialized = address(1);
						        emit Initialized();
						    }
						}
					*/
					Code:    common.FromHex("0x608060405234801561001057600080fd5b50600436106100365760003560e01c806308a543561461003b578063158ef93e14610050575b600080fd5b61004e6100493660046100f4565b61007f565b005b600054610063906001600160a01b031681565b6040516001600160a01b03909116815260200160405180910390f35b600080546001600160a01b03191660011781556040517f5daa87a0e9463431830481fd4b6e3403442dfb9a12b9c07597e9f61d50b633c89190a150565b604051610120810167ffffffffffffffff811182821017156100ee57634e487b7160e01b600052604160045260246000fd5b60405290565b6000610120828403121561010757600080fd5b61010f6100bc565b823581526020830135602082015260408301356040820152606083013560608201526080830135608082015260a083013560a082015260c083013560c082015260e083013560e0820152610100808401358183015250809150509291505056fea264697066735822122041f32989a5b45778808b251b74c621d5d19de8c37be0b731a89cb63b775dd07b64736f6c634300080c0033"),
					Balance: common.Big0,
				},
				_stakeManagerAddress: {
					/*
						pragma solidity ^0.8.2;

						contract StakeManager {
						    event Initialized();
						    event Slashed();

						    address public initialized;
						    address public slashed;

						    function initialize(address _environment, address _allowlist) external {
						        initialized = address(1);
						        emit Initialized();
						    }

						    function slash(address operator, uint256 blocks) external {
						        slashed = address(2);
						        emit Slashed();
						    }
						}
					*/
					Code:    common.FromHex("0x608060405234801561001057600080fd5b506004361061004c5760003560e01c806302fb4d85146100515780630a1ca63214610066578063158ef93e14610095578063485cc955146100a8575b600080fd5b61006461005f366004610155565b6100bb565b005b600154610079906001600160a01b031681565b6040516001600160a01b03909116815260200160405180910390f35b600054610079906001600160a01b031681565b6100646100b636600461017f565b6100fb565b600180546001600160a01b03191660021790556040517f47b2ee6ee7941903015ab048b24cfc914794d0faf7eefa9af7f7db11da1e5ec090600090a15050565b600080546001600160a01b03191660011781556040517f5daa87a0e9463431830481fd4b6e3403442dfb9a12b9c07597e9f61d50b633c89190a15050565b80356001600160a01b038116811461015057600080fd5b919050565b6000806040838503121561016857600080fd5b61017183610139565b946020939093013593505050565b6000806040838503121561019257600080fd5b61019b83610139565b91506101a960208401610139565b9050925092905056fea2646970667358221220e483794b8d74ebc03d106484a9089b470a42aabbba5e8d9481ba5c28eb76f2fb64736f6c634300080c0033"),
					Balance: common.Big0,
				},
			},
		}
	)

	// Generate genesis block
	copy(genspec.ExtraData[extraVanity:], account.Address[:])
	genspec.MustCommit(db)

	// Generate consensus engine
	engine := New(chainConfig, chainConfig.Oasys, db, nil)
	engine.Authorize(account.Address, wallet.SignData, wallet.SignTx)

	// Generate a batch of blocks, each properly signed
	chain, err := core.NewBlockChain(db, nil, chainConfig, engine, vm.Config{}, nil, nil)
	if err != nil {
		return nil, err
	}

	// Generate StateDB
	_, statedb := tests.MakePreState(db, genspec.Alloc, false)

	// Replace artifact bytecode
	environment.artifact.DeployedBytecode = fmt.Sprintf("0x%s", hex.EncodeToString(genspec.Alloc[_environmentAddress].Code))
	stakeManager.artifact.DeployedBytecode = fmt.Sprintf("0x%s", hex.EncodeToString(genspec.Alloc[_stakeManagerAddress].Code))

	return &testEnv{engine, chain, statedb}, nil
}

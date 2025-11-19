package ipc

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// TestTransactionRLPRoundtrip tests encoding and decoding a transaction
func TestTransactionRLPRoundtrip(t *testing.T) {
	// Create a dummy EIP-1559 transaction
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     1,
		GasTipCap: big.NewInt(1000000000), // 1 gwei
		GasFeeCap: big.NewInt(2000000000), // 2 gwei
		Gas:       21000,
		To:        &common.Address{0x12, 0x34, 0x56},
		Value:     big.NewInt(1000000000000000000), // 1 ETH
		Data:      []byte{},
	})

	// Encode the transaction using MarshalBinary (EIP-2718 format)
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		t.Fatalf("Failed to marshal transaction: %v", err)
	}

	t.Logf("Encoded transaction: %d bytes, first 32 bytes: %x", len(txBytes), txBytes[:min(32, len(txBytes))])

	// Decode the transaction back
	decodedTx := new(types.Transaction)
	if err := decodedTx.UnmarshalBinary(txBytes); err != nil {
		t.Fatalf("Failed to unmarshal transaction: %v", err)
	}

	// Verify the transaction matches
	if decodedTx.Hash() != tx.Hash() {
		t.Errorf("Transaction hash mismatch: expected %s, got %s", tx.Hash().Hex(), decodedTx.Hash().Hex())
	}

	t.Logf("✓ Transaction roundtrip successful: hash=%s", tx.Hash().Hex())
}

// TestEthTxPoolIpcTxMatchesRustEncoding tests that Go encoding matches Rust alloy_rlp exactly
// This test uses known-good output from Rust's alloy_rlp::encode(EthTxPoolIpcTx)
func TestEthTxPoolIpcTxMatchesRustEncoding(t *testing.T) {
	// Expected output from Rust alloy_rlp::encode(EthTxPoolIpcTx)
	// Created with:
	// - Legacy tx: chain_id=1, nonce=0, gas_price=1000000000, gas_limit=21000,
	//   to=0x4242...42 (20 bytes of 0x42), value=100, data=[]
	// - Signature: r=0x840cfc..., s=0x25e710..., parity=false (Signature::test_signature())
	// - Priority: 0x8000000000000000000000000000000000000000000000002386f26fc10000 (TOB bid)
	// - ExtraData: [] (empty)
	expectedHex := "f886f86380843b9aca00825208944242424242424242424242424242424242424242648025a0840cfc572845f5786e702984c2a582528cad4b49b2a10b9db1be7fca90058565a025e7109ceb98168d95b09b18bbf6b685130e0562f233877d492b94eee0c5b6d19f8000000000000000000000000000000000000000000000002386f26fc10000c0"

	expectedBytes := common.Hex2Bytes(expectedHex)

	// Create the same transaction in Go
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &common.Address{0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42},
		Value:    big.NewInt(100),
		Data:     []byte{},
	})

	// Sign with test signature (same as Rust Signature::test_signature())
	r, _ := new(big.Int).SetString("840cfc572845f5786e702984c2a582528cad4b49b2a10b9db1be7fca90058565", 16)
	s, _ := new(big.Int).SetString("25e7109ceb98168d95b09b18bbf6b685130e0562f233877d492b94eee0c5b6d1", 16)
	v := big.NewInt(0) // parity = 0 (false), EIP-155 signer will add chainId*2 + 35

	signer := types.NewEIP155Signer(big.NewInt(1))
	sig := make([]byte, 65)
	copy(sig[32-len(r.Bytes()):32], r.Bytes())
	copy(sig[64-len(s.Bytes()):64], s.Bytes())
	sig[64] = byte(v.Uint64())

	signedTx, err := tx.WithSignature(signer, sig)
	if err != nil {
		t.Fatalf("Failed to sign transaction: %v", err)
	}

	// Get transaction bytes
	txBytes, err := signedTx.MarshalBinary()
	if err != nil {
		t.Fatalf("Failed to marshal transaction: %v", err)
	}

	// TOB priority (same as Rust)
	priority := new(big.Int)
	priority.SetString("8000000000000000000000000000000000000000000000002386f26fc10000", 16)

	// Create IPC message
	ipcTx := &EthTxPoolIpcTx{
		TxRLP:     txBytes,
		Priority:  priority,
		ExtraData: []byte{}, // empty
	}

	// Encode
	encoded, err := rlp.EncodeToBytes(ipcTx)
	if err != nil {
		t.Fatalf("Failed to encode EthTxPoolIpcTx: %v", err)
	}

	// Compare byte-for-byte
	if len(encoded) != len(expectedBytes) {
		t.Errorf("Length mismatch: expected %d bytes, got %d bytes", len(expectedBytes), len(encoded))
		t.Logf("Expected: %x", expectedBytes)
		t.Logf("Got:      %x", encoded)
		t.FailNow()
	}

	for i := range expectedBytes {
		if encoded[i] != expectedBytes[i] {
			t.Errorf("Byte mismatch at index %d: expected %02x, got %02x", i, expectedBytes[i], encoded[i])
			t.Logf("Expected: %x", expectedBytes)
			t.Logf("Got:      %x", encoded)
			t.FailNow()
		}
	}

	t.Logf("✓ Go encoding matches Rust alloy_rlp exactly!")
	t.Logf("  Length: %d bytes", len(encoded))
	t.Logf("  Hex: %x", encoded)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

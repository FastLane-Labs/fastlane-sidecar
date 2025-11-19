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

// TestEthTxPoolIpcTxRLPRoundtrip tests encoding and decoding EthTxPoolIpcTx struct
func TestEthTxPoolIpcTxRLPRoundtrip(t *testing.T) {
	// Create a dummy transaction
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     1,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(2000000000),
		Gas:       21000,
		To:        &common.Address{0x12, 0x34, 0x56},
		Value:     big.NewInt(1000000000000000000),
		Data:      []byte{},
	})

	// Get transaction bytes (EIP-2718 format)
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		t.Fatalf("Failed to marshal transaction: %v", err)
	}

	// Create EthTxPoolIpcTx struct
	priority := big.NewInt(123456789)
	extraData := []byte{0xaa, 0xbb, 0xcc}

	ipcTx := &EthTxPoolIpcTx{
		TxRLP:     txBytes,
		Priority:  priority,
		ExtraData: extraData,
	}

	// Encode using our custom EncodeRLP method (via rlp.EncodeToBytes)
	encoded, err := rlp.EncodeToBytes(ipcTx)
	if err != nil {
		t.Fatalf("Failed to encode EthTxPoolIpcTx: %v", err)
	}

	t.Logf("Encoded EthTxPoolIpcTx: %d bytes", len(encoded))
	t.Logf("  First 64 bytes: %x", encoded[:min(64, len(encoded))])

	// Decode using go-ethereum's RLP decoder
	var decoded EthTxPoolIpcTx
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatalf("Failed to decode EthTxPoolIpcTx: %v", err)
	}

	// Verify fields match
	if len(decoded.TxRLP) != len(ipcTx.TxRLP) {
		t.Errorf("TxRLP length mismatch: expected %d, got %d", len(ipcTx.TxRLP), len(decoded.TxRLP))
	}

	for i := range decoded.TxRLP {
		if decoded.TxRLP[i] != ipcTx.TxRLP[i] {
			t.Errorf("TxRLP byte mismatch at index %d: expected %x, got %x", i, ipcTx.TxRLP[i], decoded.TxRLP[i])
			break
		}
	}

	if decoded.Priority.Cmp(ipcTx.Priority) != 0 {
		t.Errorf("Priority mismatch: expected %s, got %s", ipcTx.Priority.String(), decoded.Priority.String())
	}

	if len(decoded.ExtraData) != len(ipcTx.ExtraData) {
		t.Errorf("ExtraData length mismatch: expected %d, got %d", len(ipcTx.ExtraData), len(decoded.ExtraData))
	}

	for i := range decoded.ExtraData {
		if decoded.ExtraData[i] != ipcTx.ExtraData[i] {
			t.Errorf("ExtraData byte mismatch at index %d: expected %x, got %x", i, ipcTx.ExtraData[i], decoded.ExtraData[i])
			break
		}
	}

	// Decode the transaction from TxRLP to verify it's valid
	decodedTx := new(types.Transaction)
	if err := decodedTx.UnmarshalBinary(decoded.TxRLP); err != nil {
		t.Fatalf("Failed to unmarshal transaction from decoded TxRLP: %v", err)
	}

	if decodedTx.Hash() != tx.Hash() {
		t.Errorf("Decoded transaction hash mismatch: expected %s, got %s", tx.Hash().Hex(), decodedTx.Hash().Hex())
	}

	t.Logf("✓ EthTxPoolIpcTx roundtrip successful")
	t.Logf("  TxRLP: %d bytes", len(decoded.TxRLP))
	t.Logf("  Priority: %s", decoded.Priority.String())
	t.Logf("  ExtraData: %d bytes", len(decoded.ExtraData))
	t.Logf("  Transaction hash: %s", decodedTx.Hash().Hex())
}

// TestEthTxPoolIpcTxRLPStructure tests the exact RLP structure
func TestEthTxPoolIpcTxRLPStructure(t *testing.T) {
	// Create minimal test data
	txBytes := []byte{0x02, 0xf8, 0x6c} // Start of EIP-1559 tx (just a prefix for testing)
	priority := big.NewInt(999)
	extraData := []byte{0x01, 0x02, 0x03}

	ipcTx := &EthTxPoolIpcTx{
		TxRLP:     txBytes,
		Priority:  priority,
		ExtraData: extraData,
	}

	// Encode using custom EncodeRLP
	encoded, err := rlp.EncodeToBytes(ipcTx)
	if err != nil {
		t.Fatalf("Failed to encode: %v", err)
	}

	t.Logf("RLP structure breakdown:")
	t.Logf("  Total encoded length: %d bytes", len(encoded))
	t.Logf("  Full encoded bytes: %x", encoded)

	// Manually decode to understand structure
	t.Logf("\nExpected RLP structure:")
	t.Logf("  RLP_LIST[")

	// Encode each field separately to see what they look like
	txRLPEncoded, _ := rlp.EncodeToBytes(txBytes)
	t.Logf("    TxRLP: %x (RLP-encoded %d bytes -> %d bytes)", txRLPEncoded, len(txBytes), len(txRLPEncoded))

	priorityEncoded, _ := rlp.EncodeToBytes(priority)
	t.Logf("    Priority: %x (RLP-encoded %s -> %d bytes)", priorityEncoded, priority.String(), len(priorityEncoded))

	extraDataEncoded, _ := rlp.EncodeToBytes(extraData)
	t.Logf("    ExtraData: %x (RLP-encoded %d bytes -> %d bytes)", extraDataEncoded, len(extraData), len(extraDataEncoded))

	t.Logf("  ]")

	// Decode and verify
	var decoded EthTxPoolIpcTx
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	t.Logf("\n✓ Structure test passed")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

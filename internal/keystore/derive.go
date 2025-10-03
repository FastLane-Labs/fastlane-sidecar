package keystore

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/crypto/secp256k1"
)

const (
	// Domain separation tag used by Monad for ECDSA key generation
	monadDST = "monad-ecdsa-keygen"
)

// DeriveSecp256k1Key derives a secp256k1 private key from IKM (Input Keying Material)
// using the hash_to_scalar algorithm with expand_message_xmd (RFC 9380)
// This matches the monad-bft implementation in monad-secp/src/secp.rs
func DeriveSecp256k1Key(ikm []byte) ([]byte, error) {
	// Use expand_message_xmd to generate 48 bytes (enough for reduction mod curve order)
	expanded := expandMessageXMD(ikm, []byte(monadDST), 48)

	// Reduce modulo secp256k1 curve order to get the private key
	curveOrder := secp256k1.S256().N
	scalar := new(big.Int).SetBytes(expanded)
	scalar.Mod(scalar, curveOrder)

	// Ensure the scalar is non-zero
	if scalar.Sign() == 0 {
		return nil, fmt.Errorf("derived scalar is zero")
	}

	// Convert to 32-byte private key
	privateKey := make([]byte, 32)
	scalarBytes := scalar.Bytes()
	copy(privateKey[32-len(scalarBytes):], scalarBytes)

	return privateKey, nil
}

// expandMessageXMD implements expand_message_xmd from RFC 9380 Section 5.3.1
// https://datatracker.ietf.org/doc/html/rfc9380#name-expand_message_xmd
func expandMessageXMD(msg, dst []byte, lenInBytes int) []byte {
	// Parameters for SHA-256
	bInBytes := 32 // output size of SHA-256
	rInBytes := 64 // input block size of SHA-256

	// Step 1: ell = ceil(len_in_bytes / b_in_bytes)
	ell := (lenInBytes + bInBytes - 1) / bInBytes
	if ell > 255 {
		panic("len_in_bytes too large for expand_message_xmd")
	}

	// Step 2: DST_prime = DST || I2OSP(len(DST), 1)
	dstPrime := append(dst, byte(len(dst)))

	// Step 3: Z_pad = I2OSP(0, r_in_bytes)
	zPad := make([]byte, rInBytes)

	// Step 4: l_i_b_str = I2OSP(len_in_bytes, 2)
	libStr := make([]byte, 2)
	binary.BigEndian.PutUint16(libStr, uint16(lenInBytes))

	// Step 5: msg_prime = Z_pad || msg || l_i_b_str || I2OSP(0, 1) || DST_prime
	h := sha256.New()
	h.Write(zPad)
	h.Write(msg)
	h.Write(libStr)
	h.Write([]byte{0})
	h.Write(dstPrime)
	b0 := h.Sum(nil)

	// Step 6: b_1 = H(b_0 || I2OSP(1, 1) || DST_prime)
	h.Reset()
	h.Write(b0)
	h.Write([]byte{1})
	h.Write(dstPrime)
	b1 := h.Sum(nil)

	// Step 7-9: Compute b_i for i = 2 to ell
	uniformBytes := make([]byte, 0, lenInBytes)
	uniformBytes = append(uniformBytes, b1...)

	bPrev := b1
	for i := 2; i <= ell; i++ {
		h.Reset()
		// b_i = H(strxor(b_0, b_(i-1)) || I2OSP(i, 1) || DST_prime)
		xored := make([]byte, bInBytes)
		for j := 0; j < bInBytes; j++ {
			xored[j] = b0[j] ^ bPrev[j]
		}
		h.Write(xored)
		h.Write([]byte{byte(i)})
		h.Write(dstPrime)
		bi := h.Sum(nil)
		uniformBytes = append(uniformBytes, bi...)
		bPrev = bi
	}

	// Step 10: Return the first len_in_bytes bytes
	return uniformBytes[:lenInBytes]
}

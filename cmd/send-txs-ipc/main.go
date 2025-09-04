package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"log"
	"math/big"
	"net"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

func readFrame(conn net.Conn) ([]byte, error) {
	var lenBuf [4]byte
	if _, err := conn.Read(lenBuf[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	buf := make([]byte, n)
	_, err := readFull(conn, buf)
	return buf, err
}

func writeFrame(conn net.Conn, payload []byte) error {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	if _, err := conn.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func mustHexToPriv(hexKey string) *ecdsa.PrivateKey {
	key, err := gethcrypto.HexToECDSA(hexKey)
	if err != nil {
		log.Fatalf("invalid private key: %v", err)
	}
	return key
}

func buildSignedLegacyTx(chainID *big.Int, priv *ecdsa.PrivateKey, nonce uint64, to common.Address, gasLimit uint64, gasPriceWei *big.Int, valueWei *big.Int, data []byte) []byte {
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       &to,
		Value:    valueWei,
		Gas:      gasLimit,
		GasPrice: gasPriceWei,
		Data:     data,
	})

	signer := types.LatestSignerForChainID(chainID)
	signed, err := types.SignTx(tx, signer, priv)
	if err != nil {
		log.Fatalf("SignTx failed: %v", err)
	}
	bin, err := signed.MarshalBinary()
	if err != nil {
		log.Fatalf("MarshalBinary failed: %v", err)
	}
	return bin
}

func main() {
	var (
		ipcPath      = flag.String("ipc", "/tmp/monad.mempool.ipc", "path to mempool IPC unix socket")
		chainID      = flag.Uint64("chain-id", 10143, "chain ID for signing")
		privHex      = flag.String("priv", "59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d", "hex secp256k1 private key (dev key)")
		toHex        = flag.String("to", "0x0000000000000000000000000000000000000000", "recipient address")
		count        = flag.Uint64("count", 5, "number of transactions to send")
		startNonce   = flag.Uint64("start-nonce", 0, "starting nonce")
		gasPriceGwei = flag.Uint64("gas-price", 1, "gas price in gwei")
		gasLimit     = flag.Uint64("gas-limit", 21000, "gas limit")
		delayMs      = flag.Int("delay-ms", 100, "delay between tx sends (ms)")
		valueWei     = flag.String("value-wei", "0", "value in wei")
		dataHex      = flag.String("data", "", "optional data hex (without 0x)")
	)
	flag.Parse()

	// Connect to the UNIX socket
	conn, err := net.Dial("unix", *ipcPath)
	if err != nil {
		log.Fatalf("dial unix %s: %v", *ipcPath, err)
	}
	defer conn.Close()
	log.Printf("connected to IPC: %s", *ipcPath)

	// 1) Read the initial snapshot frame (bincode). We don't decode; just log size.
	snap, err := readFrame(conn)
	if err != nil {
		log.Fatalf("read snapshot: %v", err)
	}
	log.Printf("received snapshot frame: %d bytes", len(snap))

	// 2) Start a goroutine to continuously read back event frames and log their sizes.
	var eventCount atomic.Uint64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			frame, err := readFrame(conn)
			if err != nil {
				log.Printf("event reader: %v", err)
				return
			}
			n := eventCount.Add(1)
			// We don't have Rust bincode types here, so just print the raw length and a short prefix
			prefix := 16
			if len(frame) < prefix {
				prefix = len(frame)
			}
			log.Printf("event[%d]: %d bytes, prefix=%s", n, len(frame), hex.EncodeToString(frame[:prefix]))
		}
	}()

	// 3) Build and send transactions
	priv := mustHexToPriv(*privHex)
	to := common.HexToAddress(*toHex)
	chain := new(big.Int).SetUint64(*chainID)

	gp := new(big.Int).Mul(new(big.Int).SetUint64(*gasPriceGwei), big.NewInt(1_000_000_000))
	val := new(big.Int)
	if _, ok := val.SetString(*valueWei, 10); !ok {
		log.Fatalf("invalid value-wei")
	}
	var data []byte
	if *dataHex != "" {
		b, err := hex.DecodeString(strip0x(*dataHex))
		if err != nil {
			log.Fatalf("bad data hex: %v", err)
		}
		data = b
	}

	for i := uint64(0); i < *count; i++ {
		nonce := *startNonce + i
		raw := buildSignedLegacyTx(chain, priv, nonce, to, *gasLimit, gp, val, data)

		if err := writeFrame(conn, raw); err != nil {
			log.Fatalf("write tx frame: %v", err)
		}
		log.Printf("sent tx nonce=%d, size=%d bytes", nonce, len(raw))

		time.Sleep(time.Duration(*delayMs) * time.Millisecond)
	}

	// Wait for a bit to receive events; quit on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	log.Printf("waiting for events (Ctrl-C to exit)…")
	<-sigCh
	cancel()
	log.Printf("bye")
}

func strip0x(s string) string {
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		return s[2:]
	}
	return s
}

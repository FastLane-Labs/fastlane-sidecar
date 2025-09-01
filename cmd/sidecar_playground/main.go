package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"flag"
	"log"
	"math/big"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

func main() {
	ipcPath := flag.String("ipc-path", "/tmp/monad.fastlane.ipc", "Unix domain socket path for Fastlane IPC")
	pretty := flag.Bool("pretty", true, "Attempt to RLP-decode tx and print fields")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("connecting to Fastlane IPC: %s", *ipcPath)
	conn, err := net.Dial("unix", *ipcPath)
	if err != nil {
		log.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Buffered reader for length-delimited frames (tokio LengthDelimitedCodec default: 4-byte big-endian length)
	r := bufio.NewReader(conn)

	log.Printf("connected. waiting for tx frames… (Ctrl-C to exit)")
	var count uint64

	for {
		select {
		case <-ctx.Done():
			log.Printf("signal caught, exiting")
			return
		default:
		}

		lenBuf := make([]byte, 4)
		if _, err := r.Read(lenBuf); err != nil {
			log.Fatalf("read length failed: %v", err)
		}
		n := binary.BigEndian.Uint32(lenBuf)
		if n == 0 {
			log.Printf("got empty frame (len=0), ignoring")
			continue
		}
		if n > 16*1024*1024 {
			log.Fatalf("frame too large: %d bytes (refusing)", n)
		}

		// Read payload
		payload := make([]byte, n)
		if _, err := r.Read(payload); err != nil {
			log.Fatalf("read payload failed: %v", err)
		}

		count++
		keccak := crypto.Keccak256Hash(payload)
		if !*pretty {
			log.Printf("tx[%d]: %d bytes, keccak=%s, prefix=%s",
				count, n, keccak.Hex(), hexPrefix(payload, 16))
			continue
		}

		var tx types.Transaction
		if err := rlp.DecodeBytes(payload, &tx); err != nil {
			log.Printf("tx[%d]: %d bytes, keccak=%s (RLP decode failed: %v) prefix=%s",
				count, n, keccak.Hex(), err, hexPrefix(payload, 16))
			continue
		}

		var from string
		chainID := tx.ChainId()
		if chainID == nil {
			chainID = big.NewInt(0) // unknown
		}
		signer := types.LatestSignerForChainID(chainID)
		if addr, err := types.Sender(signer, &tx); err == nil {
			from = addr.Hex()
		} else {
			from = "<unknown>"
		}

		to := "<contract-creation>"
		if tx.To() != nil {
			to = tx.To().Hex()
		}

		log.Printf(
			"tx[%d]: %d bytes | hash=%s | from=%s -> to=%s | nonce=%d | value=%s | gas=%d | tip=%s | feeCap=%s | chainId=%s",
			count,
			n,
			keccak.Hex(),
			from,
			to,
			tx.Nonce(),
			tx.Value().String(),
			tx.Gas(),
			tx.GasTipCap().String(),
			tx.GasFeeCap().String(),
			chainID.String(),
		)
	}
}

func hexPrefix(b []byte, k int) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) > k {
		return hexutil.Encode(b[:k])
	}
	return hexutil.Encode(b)
}

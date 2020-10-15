package dcrlibwallet

import (
	"bytes"
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/decred/dcrwallet/errors/v2"
)

func (wallet *Wallet) SignMessage(passphrase []byte, address string, message string) ([]byte, error) {
	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{}
	}()

	err := wallet.internal.Unlock(passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	addr, err := btcutil.DecodeAddress(address, wallet.chainParams)
	if err != nil {
		return nil, translateError(err)
	}

	privKey, err := wallet.internal.PrivKeyForAddress(addr)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	wire.WriteVarString(&buf, 0, "Bitcoin Signed Message:\n")
	wire.WriteVarString(&buf, 0, message)
	messageHash := chainhash.DoubleHashB(buf.Bytes())

	sigbytes, err := btcec.SignCompact(btcec.S256(), privKey,
		messageHash, true)
	if err != nil {
		return nil, err
	}

	return sigbytes, nil
}

func (mw *MultiWallet) VerifyMessage(address string, message string, signatureBase64 string) (bool, error) {
	addr, err := btcutil.DecodeAddress(address, mw.chainParams)
	if err != nil {
		return false, translateError(err)
	}

	sig, err := DecodeBase64(signatureBase64)
	if err != nil {
		return false, err
	}

	// Validate the signature - this just shows that it was valid at all.
	// we will compare it with the key next.
	var buf bytes.Buffer
	wire.WriteVarString(&buf, 0, "Bitcoin Signed Message:\n")
	wire.WriteVarString(&buf, 0, message)
	expectedMessageHash := chainhash.DoubleHashB(buf.Bytes())
	pk, wasCompressed, err := btcec.RecoverCompact(btcec.S256(), sig,
		expectedMessageHash)
	if err != nil {
		return false, err
	}

	var serializedPubKey []byte
	if wasCompressed {
		serializedPubKey = pk.SerializeCompressed()
	} else {
		serializedPubKey = pk.SerializeUncompressed()
	}
	// Verify that the signed-by address matches the given address
	switch checkAddr := addr.(type) {
	case *btcutil.AddressPubKeyHash: // ok
		return bytes.Equal(btcutil.Hash160(serializedPubKey), checkAddr.Hash160()[:]), nil
	case *btcutil.AddressPubKey: // ok
		return string(serializedPubKey) == checkAddr.String(), nil
	default:
		return false, errors.New("address type not supported")
	}
}

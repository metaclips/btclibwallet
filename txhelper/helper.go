package txhelper

import (
	"encoding/hex"
	"math"
	"strings"

	"github.com/btcsuite/btcd/wire"
)

func MsgTxSize(transactionHex string) (msgTx *wire.MsgTx, size int, err error) {
	msgTx = wire.NewMsgTx(wire.TxVersion)
	if err = msgTx.Deserialize(hex.NewDecoder(strings.NewReader(transactionHex))); err != nil {
		return
	}

	size = msgTx.SerializeSize()
	return
}

// FeeRate computes the fee rate in atoms/kB for a transaction provided the
// total amount of the transaction's inputs, the total amount of the
// transaction's outputs, and the size of the transaction in bytes. Note that a
// kB refers to 1000 bytes, not a kiB. If the size is 0, the returned fee is -1.
func FeeRate(amtIn, amtOut, sizeBytes int64) int64 {
	if sizeBytes == 0 {
		return -1
	}
	return 1000 * (amtIn - amtOut) / sizeBytes
}

func TransactionAmountAndDirection(inputTotal, outputTotal, fee int64) (amount int64, direction int32) {
	amountDifference := outputTotal - inputTotal

	if amountDifference < 0 && float64(fee) == math.Abs(float64(amountDifference)) {
		// transferred internally, the only real amount spent was transaction fee
		direction = TxDirectionTransferred
		amount = fee
	} else if amountDifference > 0 {
		// received
		direction = TxDirectionReceived
		amount = outputTotal
	} else {
		// sent
		direction = TxDirectionSent
		amount = inputTotal - outputTotal - fee
	}

	return
}

func FormatTransactionType(isGenerated bool) string {
	switch isGenerated {
	case true:
		return TxTypeCoinBase
	default:
		return TxTypeRegular
	}
}

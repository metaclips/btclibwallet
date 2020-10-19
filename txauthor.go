package btclibwallet

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/btcsuite/btcwallet/wallet/txauthor"
	"github.com/btcsuite/btcwallet/wallet/txrules"
	"github.com/btcsuite/btcwallet/wallet/txsizes"
	"github.com/c-ollins/btclibwallet/txhelper"
	"github.com/decred/dcrwallet/errors/v2"
)

type TxAuthor struct {
	sourceWallet        *Wallet
	sourceAccountNumber uint32
	destinations        []TransactionDestination
	changeAddress       string
}

func (mw *MultiWallet) NewUnsignedTx(sourceWallet *Wallet, sourceAccountNumber int32) *TxAuthor {
	return &TxAuthor{
		sourceWallet:        sourceWallet,
		sourceAccountNumber: uint32(sourceAccountNumber),
		destinations:        make([]TransactionDestination, 0),
	}
}

func (tx *TxAuthor) AddSendDestination(address string, atomAmount int64, sendMax bool) error {
	_, err := btcutil.DecodeAddress(address, tx.sourceWallet.chainParams)
	if err != nil {
		return translateError(err)
	}

	tx.destinations = append(tx.destinations, TransactionDestination{
		Address:    address,
		AtomAmount: atomAmount,
		SendMax:    sendMax,
	})

	return nil
}

func (tx *TxAuthor) UpdateSendDestination(index int, address string, atomAmount int64, sendMax bool) {
	tx.destinations[index] = TransactionDestination{
		Address:    address,
		AtomAmount: atomAmount,
		SendMax:    sendMax,
	}
}

func (tx *TxAuthor) RemoveSendDestination(index int) {
	if len(tx.destinations) > index {
		tx.destinations = append(tx.destinations[:index], tx.destinations[index+1:]...)
	}
}

func (tx *TxAuthor) SendDestination(atIndex int) *TransactionDestination {
	return &tx.destinations[atIndex]
}

func (tx *TxAuthor) TotalSendAmount() *Amount {
	var totalSendAmountAtom int64 = 0
	for _, destination := range tx.destinations {
		totalSendAmountAtom += destination.AtomAmount
	}

	return &Amount{
		AtomValue: totalSendAmountAtom,
		BtcValue:  btcutil.Amount(totalSendAmountAtom).ToBTC(),
	}
}

func (tx *TxAuthor) EstimateFeeAndSize() (*TxFeeAndSize, error) {
	unsignedTx, err := tx.constructTransaction(true)
	if err != nil {
		return nil, translateError(err)
	}

	maxSignedSize := txsizes.EstimateSerializeSize(len(unsignedTx.PrevScripts), unsignedTx.Tx.TxOut, unsignedTx.ChangeIndex >= 0)

	feeToSendTx := txrules.FeeForSerializeSize(txrules.DefaultRelayFeePerKb, maxSignedSize)
	feeAmount := &Amount{
		AtomValue: int64(feeToSendTx),
		BtcValue:  feeToSendTx.ToBTC(),
	}

	return &TxFeeAndSize{
		EstimatedSignedSize: maxSignedSize,
		Fee:                 feeAmount,
	}, nil
}

func (tx *TxAuthor) EstimateMaxSendAmount() (*Amount, error) {
	txFeeAndSize, err := tx.EstimateFeeAndSize()
	if err != nil {
		return nil, err
	}

	spendableAccountBalance, err := tx.sourceWallet.SpendableForAccount(int32(tx.sourceAccountNumber))
	if err != nil {
		return nil, err
	}

	maxSendableAmount := spendableAccountBalance - txFeeAndSize.Fee.AtomValue

	return &Amount{
		AtomValue: maxSendableAmount,
		BtcValue:  btcutil.Amount(maxSendableAmount).ToBTC(),
	}, nil
}

func (tx *TxAuthor) Broadcast(privatePassphrase []byte) ([]byte, error) {
	defer func() {
		for i := range privatePassphrase {
			privatePassphrase[i] = 0
		}
	}()

	chainClient := tx.sourceWallet.internal.ChainClient()
	if chainClient == nil {
		return nil, fmt.Errorf("chain client is nil")
	}

	unsignedTx, err := tx.constructTransaction(false)
	if err != nil {
		return nil, translateError(err)
	}

	if unsignedTx.ChangeIndex >= 0 {
		unsignedTx.RandomizeChangePosition()
	}

	var txBuf bytes.Buffer
	txBuf.Grow(unsignedTx.Tx.SerializeSize())
	err = unsignedTx.Tx.Serialize(&txBuf)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	var msgTx wire.MsgTx
	err = msgTx.Deserialize(bytes.NewReader(txBuf.Bytes()))
	if err != nil {
		log.Error(err)
		//Bytes do not represent a valid raw transaction
		return nil, err
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{}
	}()

	err = tx.sourceWallet.internal.Unlock(privatePassphrase, lock)
	if err != nil {
		log.Error(err)
		return nil, errors.New(ErrInvalidPassphrase)
	}

	var additionalPkScripts map[wire.OutPoint][]byte

	invalidSigs, err := tx.sourceWallet.internal.SignTransaction(&msgTx, txscript.SigHashAll, additionalPkScripts, nil, nil)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	invalidInputIndexes := make([]uint32, len(invalidSigs))
	for i, e := range invalidSigs {
		invalidInputIndexes[i] = e.InputIndex
	}

	var serializedTransaction bytes.Buffer
	serializedTransaction.Grow(msgTx.SerializeSize())
	err = msgTx.Serialize(&serializedTransaction)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	err = msgTx.Deserialize(bytes.NewReader(serializedTransaction.Bytes()))
	if err != nil {
		//Invalid tx
		log.Error(err)
		return nil, err
	}

	err = tx.sourceWallet.internal.PublishTransaction(&msgTx)
	if err != nil {
		return nil, translateError(err)
	}

	txHash := msgTx.TxHash()
	return txHash[:], nil
}

func (tx *TxAuthor) constructTransaction(dryRun bool) (*txauthor.AuthoredTx, error) {
	var err error
	var outputs = make([]*wire.TxOut, 0)
	var changeSource txauthor.ChangeSource

	ctx := tx.sourceWallet.shutdownContext()

	for _, destination := range tx.destinations {
		// validate the amount to send to this destination address
		if !destination.SendMax && (destination.AtomAmount <= 0 || destination.AtomAmount > MaxAmountAtom) {
			return nil, errors.E(errors.Invalid, "invalid amount")
		}

		// check if multiple destinations are set to receive max amount
		if destination.SendMax && changeSource != nil {
			return nil, fmt.Errorf("cannot send max amount to multiple recipients")
		}

		if destination.SendMax {
			// Use this destination address to make a changeSource rather than a tx output.
			changeSource = txhelper.MakeTxChangeSource(destination.Address, tx.sourceWallet.chainParams)
		} else {
			output, err := txhelper.MakeTxOutput(destination.Address, destination.AtomAmount, tx.sourceWallet.chainParams)
			if err != nil {
				log.Errorf("constructTransaction: error preparing tx output: %v", err)
				return nil, fmt.Errorf("make tx output error: %v", err)
			}

			outputs = append(outputs, output)
		}
	}

	if changeSource == nil {
		// dcrwallet should ordinarily handle cases where a nil changeSource
		// is passed to `wallet.NewUnsignedTransaction` but the changeSource
		// generated there errors on internal gap address limit exhaustion
		// instead of wrapping around to a previously returned address.
		//
		// Generating a changeSource manually here, ensures that the gap address
		// limit exhaustion error is avoided.
		changeSource, err = tx.changeSource(ctx)
		if err != nil {
			return nil, err
		}
	}

	requiredConfirmations := tx.sourceWallet.RequiredConfirmations()
	return tx.sourceWallet.internal.CreateSimpleTx(tx.sourceAccountNumber, outputs, requiredConfirmations, txrules.DefaultRelayFeePerKb, dryRun)
}

// changeSource derives an internal address from the source wallet and account
// for this unsigned tx, if a change address had not been previously derived.
// The derived (or previously derived) address is used to prepare a
// change source for receiving change from this tx back into the wallet.
func (tx *TxAuthor) changeSource(ctx context.Context) (func() ([]byte, error), error) {
	if tx.changeAddress == "" {
		address, err := tx.sourceWallet.internal.NewChangeAddress(tx.sourceAccountNumber, waddrmgr.KeyScopeBIP0044)
		if err != nil {
			return nil, fmt.Errorf("change address error: %v", err)
		}

		tx.changeAddress = address.String()
	}

	return txhelper.MakeTxChangeSource(tx.changeAddress, tx.sourceWallet.chainParams), nil
}

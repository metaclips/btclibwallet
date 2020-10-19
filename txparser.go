package btclibwallet

import (
	"fmt"

	w "github.com/btcsuite/btcwallet/wallet"
)

const BlockHeightInvalid int32 = -1

func (wallet *Wallet) decodeTransactionWithTxSummary(txSummary w.TransactionSummary, blockHeight int32) (*Transaction, error) {

	walletInputs := make([]*WalletInput, len(txSummary.MyInputs))
	for i, input := range txSummary.MyInputs {
		accountNumber := int32(input.PreviousAccount)
		walletInputs[i] = &WalletInput{
			Index:    int32(input.Index),
			AmountIn: int64(input.PreviousAmount),
			WalletAccount: &WalletAccount{
				AccountNumber: accountNumber,
				AccountName:   wallet.AccountName(accountNumber),
			},
		}
	}

	walletOutputs := make([]*WalletOutput, len(txSummary.MyOutputs))
	for i, output := range txSummary.MyOutputs {
		accountNumber := int32(output.Account)
		walletOutputs[i] = &WalletOutput{
			Index: int32(output.Index),
			// Amount & Address is added in DecodeTransaction
			Internal: output.Internal,
			WalletAccount: &WalletAccount{
				AccountNumber: accountNumber,
				AccountName:   wallet.AccountName(accountNumber),
			},
		}
	}

	walletTx := &TxInfoFromWallet{
		WalletID:    wallet.ID,
		BlockHeight: blockHeight,
		Fee:         int64(txSummary.Fee),
		Timestamp:   txSummary.Timestamp,
		Hex:         fmt.Sprintf("%x", txSummary.Transaction),
		Inputs:      walletInputs,
		Outputs:     walletOutputs,
	}

	decodedTx, err := DecodeTransaction(walletTx, wallet.chainParams)
	if err != nil {
		return nil, err
	}

	return decodedTx, nil
}

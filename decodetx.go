package btclibwallet

import (
	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/c-ollins/btclibwallet/txhelper"
)

const BlockValid = 1 << 0

// DecodeTransaction uses `walletTx.Hex` to retrieve detailed information for a transaction.
func DecodeTransaction(walletTx *TxInfoFromWallet, netParams *chaincfg.Params) (*Transaction, error) {

	msgTx, txSize, err := txhelper.MsgTxSize(walletTx.Hex)
	if err != nil {
		return nil, err
	}

	// only use input/output amounts relating to wallet to correctly determine tx direction
	var totalWalletInput, totalWalletOutput int64
	for _, input := range walletTx.Inputs {
		totalWalletInput += input.AmountIn
	}

	//TODO: Might remove block
	for index, txOut := range msgTx.TxOut {
		for _, output := range walletTx.Outputs {
			if int(output.Index) == index {
				output.AmountOut = txOut.Value
				// totalWalletOutput += output.AmountOut

				_, addrs, _, err := txscript.ExtractPkScriptAddrs(txOut.PkScript, netParams)
				if err != nil {
					return nil, err
				}
				if len(addrs) > 0 {
					output.Address = addrs[0].String()
				}

				break
			}
		}
	}

	for _, output := range walletTx.Outputs {
		totalWalletOutput += output.AmountOut
	}

	txType := blockchain.IsCoinBaseTx(msgTx)

	amount, direction := txhelper.TransactionAmountAndDirection(totalWalletInput, totalWalletOutput, walletTx.Fee)

	inputs := decodeTxInputs(msgTx, walletTx.Inputs)
	outputs := decodeTxOutputs(msgTx, netParams, walletTx.Outputs)

	return &Transaction{
		WalletID:    walletTx.WalletID,
		Hash:        msgTx.TxHash().String(),
		Type:        txhelper.FormatTransactionType(txType),
		Hex:         walletTx.Hex,
		Timestamp:   walletTx.Timestamp,
		BlockHeight: walletTx.BlockHeight,

		Version:  msgTx.Version,
		LockTime: int32(msgTx.LockTime),
		Fee:      walletTx.Fee,
		Size:     txSize,

		Direction: direction,
		Amount:    amount,
		Inputs:    inputs,
		Outputs:   outputs,
	}, nil
}

func decodeTxInputs(mtx *wire.MsgTx, walletInputs []*WalletInput) (inputs []*TxInput) {
	inputs = make([]*TxInput, len(mtx.TxIn))

	for i, txIn := range mtx.TxIn {
		input := &TxInput{
			PreviousTransactionHash:  txIn.PreviousOutPoint.Hash.String(),
			PreviousTransactionIndex: int32(txIn.PreviousOutPoint.Index),
			PreviousOutpoint:         txIn.PreviousOutPoint.String(),
			AccountName:              "external", // correct account name and number set below if this is a wallet output
			AccountNumber:            -1,
		}

		// override account details if this is wallet input
		for _, walletInput := range walletInputs {
			if walletInput.Index == int32(i) {
				input.AccountName = walletInput.AccountName
				input.AccountNumber = walletInput.AccountNumber
				input.Amount = walletInput.AmountIn
				break
			}
		}

		inputs[i] = input
	}

	return
}

func decodeTxOutputs(mtx *wire.MsgTx, netParams *chaincfg.Params, walletOutputs []*WalletOutput) (outputs []*TxOutput) {
	outputs = make([]*TxOutput, len(mtx.TxOut))

	for i, txOut := range mtx.TxOut {
		// get address and script type for output
		var address string
		// Ignore the error here since an error means the script
		// couldn't parse and there is no additional information
		// about it anyways.
		scriptClass, addrs, _, _ := txscript.ExtractPkScriptAddrs(txOut.PkScript, netParams)
		if len(addrs) > 0 {
			address = addrs[0].String()
		}
		scriptType := scriptClass.String()

		output := &TxOutput{
			Index:         int32(i),
			Amount:        txOut.Value,
			ScriptType:    scriptType,
			Address:       address, // correct address, account name and number set below if this is a wallet output
			AccountName:   "external",
			AccountNumber: -1,
		}

		// override address and account details if this is wallet output
		for _, walletOutput := range walletOutputs {
			if walletOutput.Index == output.Index {
				output.Internal = walletOutput.Internal
				output.Address = walletOutput.Address
				output.AccountName = walletOutput.AccountName
				output.AccountNumber = walletOutput.AccountNumber
				break
			}
		}

		outputs[i] = output
	}

	return
}

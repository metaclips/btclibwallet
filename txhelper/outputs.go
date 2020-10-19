package txhelper

import (
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	"github.com/c-ollins/btclibwallet/addresshelper"
)

func MakeTxOutput(address string, amountInAtom int64, chainParams *chaincfg.Params) (output *wire.TxOut, err error) {
	pkScript, err := addresshelper.PkScript(address, chainParams)
	if err != nil {
		return
	}

	output = &wire.TxOut{
		Value:    amountInAtom,
		PkScript: pkScript,
	}
	return
}

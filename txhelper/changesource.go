package txhelper

import (
	"github.com/c-ollins/btclibwallet/addresshelper"
	"github.com/btcsuite/btcd/chaincfg"
)

func MakeTxChangeSource(destAddr string, chainParams *chaincfg.Params) func() ([]byte, error) {
	f := func() ([]byte, error) {
		return addresshelper.PkScript(destAddr, chainParams)
	}
	return f
}

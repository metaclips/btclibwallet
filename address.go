package dcrlibwallet

import (
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/decred/dcrwallet/errors/v2"
)

// AddressInfo holds information about an address
// If the address belongs to the querying wallet, IsMine will be true and the AccountNumber and AccountName values will be populated
type AddressInfo struct {
	Address       string
	IsMine        bool
	AccountNumber uint32
	AccountName   string
}

func (mw *MultiWallet) IsAddressValid(address string) bool {
	_, err := btcutil.DecodeAddress(address, mw.chainParams)
	return err == nil
}

func (wallet *Wallet) HaveAddress(address string) bool {
	addr, err := btcutil.DecodeAddress(address, wallet.chainParams)
	if err != nil {
		return false
	}

	have, err := wallet.internal.HaveAddress(addr)
	if err != nil {
		return false
	}

	return have
}

func (wallet *Wallet) AccountOfAddress(address string) string {
	addr, err := btcutil.DecodeAddress(address, wallet.chainParams)
	if err != nil {
		return err.Error()
	}

	info, _ := wallet.internal.AddressInfo(addr)
	return wallet.AccountName(int32(info.Account()))
}

func (wallet *Wallet) AddressInfo(address string) (*AddressInfo, error) {
	addr, err := btcutil.DecodeAddress(address, wallet.chainParams)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	addressInfo := &AddressInfo{
		Address: address,
	}

	info, _ := wallet.internal.AddressInfo(addr)
	if info != nil {
		addressInfo.IsMine = true
		addressInfo.AccountNumber = info.Account()
		addressInfo.AccountName = wallet.AccountName(int32(info.Account()))
	}

	return addressInfo, nil
}

func (wallet *Wallet) CurrentAddress(account int32) (string, error) {
	if wallet.IsRestored && !wallet.HasDiscoveredAccounts {
		return "", errors.E(ErrAddressDiscoveryNotDone)
	}

	addr, err := wallet.internal.CurrentAddress(uint32(account), waddrmgr.KeyScopeBIP0044)
	if err != nil {
		log.Error(err)
		return "", err
	}
	return addr.String(), nil
}

func (wallet *Wallet) NextAddress(account int32) (string, error) {
	if wallet.IsRestored && !wallet.HasDiscoveredAccounts {
		return "", errors.E(ErrAddressDiscoveryNotDone)
	}

	addr, err := wallet.internal.NewAddress(uint32(account), waddrmgr.KeyScopeBIP0044)
	if err != nil {
		log.Error(err)
		return "", err
	}
	return addr.String(), nil
}

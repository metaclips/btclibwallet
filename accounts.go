package dcrlibwallet

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/decred/dcrwallet/errors/v2"
)

func (wallet *Wallet) GetAccounts() (string, error) {
	accountsResponse, err := wallet.GetAccountsRaw()
	if err != nil {
		return "", nil
	}

	result, _ := json.Marshal(accountsResponse)
	return string(result), nil
}

func (wallet *Wallet) GetAccountsRaw() (*Accounts, error) {
	resp, err := wallet.internal.Accounts(waddrmgr.KeyScopeBIP0044)
	if err != nil {
		return nil, err
	}
	accounts := make([]*Account, len(resp.Accounts))
	for i, a := range resp.Accounts {
		account, err := wallet.GetAccount(int32(a.AccountNumber))
		if err != nil {
			return nil, err
		}

		accounts[i] = account
	}

	return &Accounts{
		Count:              len(resp.Accounts),
		CurrentBlockHash:   resp.CurrentBlockHash[:],
		CurrentBlockHeight: resp.CurrentBlockHeight,
		Acc:                accounts,
	}, nil
}

func (wallet *Wallet) AccountsIterator() (*AccountsIterator, error) {
	accounts, err := wallet.GetAccountsRaw()
	if err != nil {
		return nil, err
	}

	return &AccountsIterator{
		currentIndex: 0,
		accounts:     accounts.Acc,
	}, nil
}

func (accountsInterator *AccountsIterator) Next() *Account {
	if accountsInterator.currentIndex < len(accountsInterator.accounts) {
		account := accountsInterator.accounts[accountsInterator.currentIndex]
		accountsInterator.currentIndex++
		return account
	}

	return nil
}

func (accountsInterator *AccountsIterator) Reset() {
	accountsInterator.currentIndex = 0
}

func (wallet *Wallet) GetAccount(accountNumber int32) (*Account, error) {
	props, err := wallet.internal.AccountProperties(waddrmgr.KeyScopeBIP0044, uint32(accountNumber))
	if err != nil {
		return nil, err
	}

	balance, err := wallet.GetAccountBalance(accountNumber)
	if err != nil {
		return nil, err
	}

	account := &Account{
		WalletID:         wallet.ID,
		Number:           accountNumber,
		Name:             props.AccountName,
		TotalBalance:     balance.Total,
		Balance:          balance,
		ExternalKeyCount: int32(props.ExternalKeyCount),
		InternalKeyCount: int32(props.InternalKeyCount),
		ImportedKeyCount: int32(props.ImportedKeyCount),
	}

	return account, nil
}

func (wallet *Wallet) GetAccountBalance(accountNumber int32) (*Balance, error) {
	balance, err := wallet.internal.CalculateAccountBalances(uint32(accountNumber), wallet.RequiredConfirmations())
	if err != nil {
		return nil, err
	}

	return &Balance{
		Total:          int64(balance.Total),
		Spendable:      int64(balance.Spendable),
		ImmatureReward: int64(balance.ImmatureReward),
	}, nil
}

func (wallet *Wallet) SpendableForAccount(account int32) (int64, error) {
	bals, err := wallet.internal.CalculateAccountBalances(uint32(account), wallet.RequiredConfirmations())
	if err != nil {
		log.Error(err)
		return 0, translateError(err)
	}
	return int64(bals.Spendable), nil
}

func (wallet *Wallet) NextAccount(accountName string, privPass []byte) (int32, error) {
	lock := make(chan time.Time, 1)
	defer func() {
		for i := range privPass {
			privPass[i] = 0
		}
		lock <- time.Time{} // send matters, not the value
	}()

	err := wallet.internal.Unlock(privPass, lock)
	if err != nil {
		log.Error(err)
		return 0, errors.New(ErrInvalidPassphrase)
	}

	accountNumber, err := wallet.internal.NextAccount(waddrmgr.KeyScopeBIP0044, accountName)

	return int32(accountNumber), err
}

func (wallet *Wallet) RenameAccount(accountNumber int32, newName string) error {
	err := wallet.internal.RenameAccount(waddrmgr.KeyScopeBIP0044, uint32(accountNumber), newName)
	if err != nil {
		return translateError(err)
	}

	return nil
}

func (wallet *Wallet) AccountName(accountNumber int32) string {
	name, err := wallet.AccountNameRaw(uint32(accountNumber))
	if err != nil {
		log.Error(err)
		return "Account not found"
	}
	return name
}

func (wallet *Wallet) AccountNameRaw(accountNumber uint32) (string, error) {
	return wallet.internal.AccountName(waddrmgr.KeyScopeBIP0044, accountNumber)
}

func (wallet *Wallet) AccountNumber(accountName string) (uint32, error) {
	return wallet.internal.AccountNumber(waddrmgr.KeyScopeBIP0044, accountName)
}

func (wallet *Wallet) HDPathForAccount(accountNumber int32) (string, error) {
	var hdPath string
	if wallet.chainParams.Name == chaincfg.MainNetParams.Name {
		hdPath = MainnetHDPath
	} else {
		hdPath = TestnetHDPath
	}

	return hdPath + strconv.Itoa(int(accountNumber)), nil
}

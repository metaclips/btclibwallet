package btclibwallet

import (
	"encoding/json"

	"github.com/decred/dcrwallet/errors/v2"
)

func (mw *MultiWallet) listenForTransactions(walletID int) {
	go func() {

		wallet := mw.wallets[walletID]
		n := wallet.internal.NtfnServer.TransactionNotifications()

		for {
			select {
			case v := <-n.C:
				if v == nil {
					return
				}
				for _, transaction := range v.UnminedTransactions {
					tempTransaction, err := wallet.decodeTransactionWithTxSummary(transaction, -1)
					if err != nil {
						log.Errorf("[%d] Error ntfn parse tx: %v", wallet.ID, err)
						return
					}

					overwritten, err := wallet.txDB.SaveOrUpdate(&Transaction{}, tempTransaction)
					if err != nil {
						log.Errorf("[%d] New Tx save err: %v", wallet.ID, err)
						return
					}

					if !overwritten {
						log.Infof("[%d] New Transaction %s", wallet.ID, tempTransaction.Hash)

						result, err := json.Marshal(tempTransaction)
						if err != nil {
							log.Error(err)
						} else {
							mw.mempoolTransactionNotification(string(result))
						}
					}
				}

				for _, block := range v.AttachedBlocks {
					for _, transaction := range block.Transactions {
						tempTransaction, err := wallet.decodeTransactionWithTxSummary(transaction, block.Height)
						if err != nil {
							log.Errorf("[%d] Error ntfn parse tx: %v", wallet.ID, err)
							return
						}

						overwritten, err := wallet.txDB.SaveOrUpdate(&Transaction{}, tempTransaction)
						if err != nil {
							log.Errorf("[%d] Incoming block replace tx error :%v", wallet.ID, err)
							return
						}

						if !overwritten {
							log.Infof("[%d] New Transaction %s", wallet.ID, tempTransaction.Hash)

							result, err := json.Marshal(tempTransaction)
							if err != nil {
								log.Error(err)
							} else {
								mw.mempoolTransactionNotification(string(result))
							}

							log.Infof("[%d] New confirmed transaction", wallet.ID, transaction.Hash)
							mw.publishTransactionConfirmed(wallet.ID, transaction.Hash.String(), int32(block.Height))
						}
					}

					mw.publishBlockAttached(wallet.ID, int32(block.Height))
				}

			case <-mw.syncData.syncCanceled:
				n.Done()
			}
		}
	}()
}

func (mw *MultiWallet) AddTxAndBlockNotificationListener(txAndBlockNotificationListener TxAndBlockNotificationListener, uniqueIdentifier string) error {
	mw.notificationListenersMu.Lock()
	defer mw.notificationListenersMu.Unlock()

	_, ok := mw.txAndBlockNotificationListeners[uniqueIdentifier]
	if ok {
		return errors.New(ErrListenerAlreadyExist)
	}

	mw.txAndBlockNotificationListeners[uniqueIdentifier] = txAndBlockNotificationListener

	return nil
}

func (mw *MultiWallet) RemoveTxAndBlockNotificationListener(uniqueIdentifier string) {
	mw.notificationListenersMu.Lock()
	defer mw.notificationListenersMu.Unlock()

	delete(mw.txAndBlockNotificationListeners, uniqueIdentifier)
}

func (mw *MultiWallet) mempoolTransactionNotification(transaction string) {
	mw.notificationListenersMu.RLock()
	defer mw.notificationListenersMu.RUnlock()

	for _, txAndBlockNotifcationListener := range mw.txAndBlockNotificationListeners {
		txAndBlockNotifcationListener.OnTransaction(transaction)
	}
}

func (mw *MultiWallet) publishTransactionConfirmed(walletID int, transactionHash string, blockHeight int32) {
	mw.notificationListenersMu.RLock()
	defer mw.notificationListenersMu.RUnlock()

	for _, txAndBlockNotifcationListener := range mw.txAndBlockNotificationListeners {
		txAndBlockNotifcationListener.OnTransactionConfirmed(walletID, transactionHash, blockHeight)
	}
}

func (mw *MultiWallet) publishBlockAttached(walletID int, blockHeight int32) {
	mw.notificationListenersMu.RLock()
	defer mw.notificationListenersMu.RUnlock()

	for _, txAndBlockNotifcationListener := range mw.txAndBlockNotificationListeners {
		txAndBlockNotifcationListener.OnBlockAttached(walletID, blockHeight)
	}
}

package btclibwallet

import (
	w "github.com/btcsuite/btcwallet/wallet"
	"github.com/c-ollins/btclibwallet/txindex"
)

func (wallet *Wallet) IndexTransactions() error {
	var txEndHeight int32
	beginHeight, err := wallet.txDB.ReadIndexingStartBlock()
	if err != nil {
		log.Errorf("[%d] Get tx indexing start point error: %v", wallet.ID, err)
		return err
	}

	endHeight := wallet.GetBestBlock()

	if beginHeight <= 0 {
		beginHeight = endHeight - 400 // TODO: confirm this logic
	}

	startBlock := w.NewBlockIdentifierFromHeight(beginHeight)
	endBlock := w.NewBlockIdentifierFromHeight(endHeight)

	log.Infof("[%d] Indexing transactions start height: %d, end height: %d", wallet.ID, beginHeight, endHeight)

	cancel := make(<-chan struct{})
	results, err := wallet.internal.GetTransactions(startBlock, endBlock, cancel)
	if err != nil {
		return err
	}

	for _, block := range results.MinedTransactions {
		for _, transaction := range block.Transactions {

			tx, err := wallet.decodeTransactionWithTxSummary(transaction, block.Height)
			if err != nil {
				return err
			}

			_, err = wallet.txDB.SaveOrUpdate(&Transaction{}, tx)
			if err != nil {
				log.Errorf("[%d] Index tx replace tx err : %v", wallet.ID, err)
				return err
			}
		}

		txEndHeight = block.Height
		err := wallet.txDB.SaveLastIndexPoint(int32(txEndHeight))
		if err != nil {
			log.Errorf("[%d] Set tx index end block height error: ", wallet.ID, err)
			return err
		}

		log.Infof("[%d] Index saved for transactions in block %d", wallet.ID, txEndHeight)
	}

	for _, transaction := range results.UnminedTransactions {
		tx, err := wallet.decodeTransactionWithTxSummary(transaction, -1)
		if err != nil {
			return err
		}

		_, err = wallet.txDB.SaveOrUpdate(&Transaction{}, tx)
		if err != nil {
			log.Errorf("[%d] Index tx replace tx err : %v", wallet.ID, err)
			return err
		}
	}

	count, err := wallet.txDB.Count(txindex.TxFilterAll, &Transaction{})
	if err != nil {
		log.Errorf("[%d] Post-indexing tx count error :%v", wallet.ID, err)
	} else if count > 0 {
		log.Infof("[%d] Transaction index finished at %d, %d transaction(s) indexed in total", wallet.ID, txEndHeight, count)
	}

	log.Info("Indexing did nothing: ", wallet.IsSynced())

	return nil
}

func (wallet *Wallet) reindexTransactions() error {
	err := wallet.txDB.ClearSavedTransactions(&Transaction{})
	if err != nil {
		return err
	}

	return wallet.IndexTransactions()
}

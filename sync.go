package dcrlibwallet

import (
	"context"
	"golang.org/x/sync/errgroup"
	"path/filepath"
	"strings"
	"sync"

	"github.com/btcsuite/btcwallet/chain"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/lightninglabs/neutrino"
)

// reading/writing of properties of this struct are protected by mutex.x
type syncData struct {
	mu sync.RWMutex

	syncProgressListeners map[string]SyncProgressListener
	showLogs              bool

	synced       bool
	syncing      bool
	chainClient  chain.Interface
	syncCanceled chan struct{}
	cancelSync   context.CancelFunc
	cancelRescan context.CancelFunc

	// Flag to notify syncCanceled callback if the sync was canceled so as to be restarted.
	restartSyncRequested bool

	rescanning     bool
	connectedPeers int32

	*activeSyncData
}

// reading/writing of properties of this struct are protected by syncData.mu.
type activeSyncData struct {
	syncStage int32

	cFiltersFetchProgress CFiltersFetchProgressReport

	beginFetchTimeStamp   int64
	startHeaderHeight     int32
	headersFetchTimeSpent int64

	addressDiscoveryStartTime int64
	totalDiscoveryTimeSpent   int64

	addressDiscoveryCompletedOrCanceled chan bool

	rescanStartTime int64

	totalInactiveSeconds     int64
	totalFetchedHeadersCount int32
}

const (
	InvalidSyncStage          = -1
	HeadersFetchSyncStage     = 0
	AddressDiscoverySyncStage = 1
	HeadersRescanSyncStage    = 2
)

func (mw *MultiWallet) initActiveSyncData() {
	mw.syncData.mu.Lock()

	cFiltersFetchProgress := CFiltersFetchProgressReport{}
	cFiltersFetchProgress.GeneralSyncProgress = &GeneralSyncProgress{}

	mw.syncData.activeSyncData = &activeSyncData{
		syncStage: InvalidSyncStage,

		cFiltersFetchProgress: cFiltersFetchProgress,

		beginFetchTimeStamp:       -1,
		headersFetchTimeSpent:     -1,
		addressDiscoveryStartTime: -1,
		totalDiscoveryTimeSpent:   -1,
		totalFetchedHeadersCount:  0,
	}
	mw.syncData.mu.Unlock()
}

func (mw *MultiWallet) IsSyncProgressListenerRegisteredFor(uniqueIdentifier string) bool {
	mw.syncData.mu.RLock()
	_, exists := mw.syncData.syncProgressListeners[uniqueIdentifier]
	mw.syncData.mu.RUnlock()
	return exists
}

func (mw *MultiWallet) AddSyncProgressListener(syncProgressListener SyncProgressListener, uniqueIdentifier string) error {
	if mw.IsSyncProgressListenerRegisteredFor(uniqueIdentifier) {
		return errors.New(ErrListenerAlreadyExist)
	}

	mw.syncData.mu.Lock()
	mw.syncData.syncProgressListeners[uniqueIdentifier] = syncProgressListener
	mw.syncData.mu.Unlock()

	// If sync is already on, notify this newly added listener of the current progress report.
	// return mw.PublishLastSyncProgress(uniqueIdentifier)
	return nil
}

func (mw *MultiWallet) RemoveSyncProgressListener(uniqueIdentifier string) {
	mw.syncData.mu.Lock()
	delete(mw.syncData.syncProgressListeners, uniqueIdentifier)
	mw.syncData.mu.Unlock()
}

func (mw *MultiWallet) syncProgressListeners() []SyncProgressListener {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()

	listeners := make([]SyncProgressListener, 0, len(mw.syncData.syncProgressListeners))
	for _, listener := range mw.syncData.syncProgressListeners {
		listeners = append(listeners, listener)
	}

	return listeners
}

// func (mw *MultiWallet) PublishLastSyncProgress(uniqueIdentifier string) error {
// 	mw.syncData.mu.RLock()
// 	defer mw.syncData.mu.RUnlock()

// 	syncProgressListener, exists := mw.syncData.syncProgressListeners[uniqueIdentifier]
// 	if !exists {
// 		return errors.New(ErrInvalid)
// 	}

// 	if mw.syncData.syncing && mw.syncData.activeSyncData != nil {
// 		switch mw.syncData.activeSyncData.syncStage {
// 		case HeadersFetchSyncStage:
// 			syncProgressListener.OnHeadersFetchProgress(&mw.syncData.headersFetchProgress)
// 		case AddressDiscoverySyncStage:
// 			syncProgressListener.OnAddressDiscoveryProgress(&mw.syncData.addressDiscoveryProgress)
// 		case HeadersRescanSyncStage:
// 			syncProgressListener.OnHeadersRescanProgress(&mw.syncData.headersRescanProgress)
// 		}
// 	}

// 	return nil
// }

func (mw *MultiWallet) EnableSyncLogs() {
	mw.syncData.mu.Lock()
	mw.syncData.showLogs = true
	mw.syncData.mu.Unlock()
}

func (mw *MultiWallet) SyncInactiveForPeriod(totalInactiveSeconds int64) {
	mw.syncData.mu.Lock()
	defer mw.syncData.mu.Unlock()

	if !mw.syncData.syncing || mw.syncData.activeSyncData == nil {
		log.Debug("Not accounting for inactive time, wallet is not syncing.")
		return
	}

	mw.syncData.totalInactiveSeconds += totalInactiveSeconds
	if mw.syncData.connectedPeers == 0 {
		// assume it would take another 60 seconds to reconnect to peers
		mw.syncData.totalInactiveSeconds += 60
	}
}

func (mw *MultiWallet) SpvSync() error {
	// prevent an attempt to sync when the previous syncing has not been canceled
	if mw.IsSyncing() || mw.IsSynced() {
		return errors.New(ErrSyncAlreadyInProgress)
	}

	var validPeerAddresses []string
	peerAddresses := mw.ReadStringConfigValueForKey(SpvPersistentPeerAddressesConfigKey)
	if peerAddresses != "" {
		addresses := strings.Split(peerAddresses, ";")
		for _, address := range addresses {
			peerAddress, err := NormalizeAddress(address, mw.chainParams.DefaultPort)
			if err != nil {
				log.Errorf("SPV peer address(%s) is invalid: %v", peerAddress, err)
			} else {
				validPeerAddresses = append(validPeerAddresses, peerAddress)
			}
		}

		if len(validPeerAddresses) == 0 {
			return errors.New(ErrInvalidPeers)
		}
	}

	// init activeSyncData to be used to hold data used
	// to calculate sync estimates only during sync
	mw.initActiveSyncData()

	wallet := mw.wallets[1]
	wallet.waiting = true
	wallet.syncing = true

	go func() {
		neutrino.MaxPeers = neutrino.MaxPeers
		neutrino.BanDuration = neutrino.BanDuration
		neutrino.BanThreshold = neutrino.BanThreshold

		var (
			chainService *neutrino.ChainService
			spvdb        walletdb.DB
		)
		spvdb, err := walletdb.Create("bdb",
			filepath.Join(mw.rootDir, "neutrino.db"), true)
		defer spvdb.Close()
		if err != nil {
			log.Errorf("Unable to create Neutrino DB: %s", err)
			return
		}

		chainService, err = neutrino.NewChainService(
			neutrino.Config{
				DataDir:      mw.rootDir,
				Database:     spvdb,
				ChainParams:  *mw.chainParams,
				ConnectPeers: validPeerAddresses,
				AddPeers:     validPeerAddresses, //TODO
			}, mw.spvSyncNotificationCallbacks())
		if err != nil {
			log.Errorf("Couldn't create Neutrino ChainService: %s", err)
			return
		}

		log.Info("Starting spv")
		chainClient := chain.NewNeutrinoClient(mw.chainParams, chainService)
		err = chainClient.Start()
		if err != nil {
			log.Errorf("Couldn't start Neutrino client: %s", err)
			return
		}

		mw.setNetworkBackend(chainClient)
		wallet.internal.SynchronizeRPC(chainClient)

		var restartSyncRequested bool

		mw.syncData.mu.Lock()
		restartSyncRequested = mw.syncData.restartSyncRequested
		mw.syncData.restartSyncRequested = false
		mw.syncData.syncing = true
		mw.syncData.syncCanceled = make(chan struct{})
		mw.syncData.mu.Unlock()

		for _, listener := range mw.syncProgressListeners() {
			listener.OnSyncStarted(restartSyncRequested)
		}

		chainClient.WaitForShutdown()
		chainClient.Stop()
		log.Info("Chainclient stopped")
		close(mw.syncData.syncCanceled)

		mw.resetSyncData()
	}()
	return nil
}

func (mw *MultiWallet) RestartSpvSync() error {
	mw.syncData.mu.Lock()
	mw.syncData.restartSyncRequested = true
	mw.syncData.mu.Unlock()

	mw.CancelSync() // necessary to unset the network backend.
	return mw.SpvSync()
}

func (mw *MultiWallet) CancelSync() {
	mw.syncData.mu.RLock()
	chainClient := mw.syncData.chainClient
	mw.syncData.mu.RUnlock()

	if chainClient != nil {
		log.Info("Canceling sync. May take a while for sync to fully cancel.")

		chainClient.Stop()

		// When sync terminates and syncer.Run returns `err == context.Canceled`,
		// we will get notified on this channel.
		<-mw.syncData.syncCanceled

		log.Info("Sync fully canceled.")
	}

	mw.setNetworkBackend(nil)
}

func (mw *MultiWallet) resetSyncData() {

	mw.syncData.mu.Lock()
	mw.syncData.syncing = false
	mw.syncData.synced = false
	mw.syncData.cancelSync = nil
	mw.syncData.syncCanceled = nil
	mw.syncData.activeSyncData = nil
	mw.syncData.mu.Unlock()

	for _, wallet := range mw.wallets {
		wallet.waiting = true
		wallet.LockWallet() // lock wallet if previously unlocked to perform account discovery.
	}
}

func (mw *MultiWallet) synced(walletID int, synced bool) {
	mw.syncData.mu.RLock()
	allWalletsSynced := mw.syncData.synced
	mw.syncData.mu.RUnlock()

	if allWalletsSynced && synced {
		return
	}

	wallet := mw.wallets[walletID]
	wallet.synced = synced
	wallet.syncing = false
	mw.listenForTransactions(wallet.ID)

	if !wallet.internal.Locked() {
		wallet.LockWallet() // lock wallet if previously unlocked to perform account discovery.
		err := mw.markWalletAsDiscoveredAccounts(walletID)
		if err != nil {
			log.Error(err)
		}
	}

	if mw.OpenedWalletsCount() == mw.SyncedWalletsCount() {
		mw.syncData.mu.Lock()
		mw.syncData.syncing = false
		mw.syncData.synced = true
		mw.syncData.mu.Unlock()

		// begin indexing transactions after sync is completed,
		// syncProgressListeners.OnSynced() will be invoked after transactions are indexed
		var txIndexing errgroup.Group
		for _, wallet := range mw.wallets {
			txIndexing.Go(wallet.IndexTransactions)
		}

		go func() {
			err := txIndexing.Wait()
			if err != nil {
				log.Errorf("Tx Index Error: %v", err)
			}

			for _, syncProgressListener := range mw.syncProgressListeners() {
				if synced {
					syncProgressListener.OnSyncCompleted()
				} else {
					syncProgressListener.OnSyncCanceled(false)
				}
			}
		}()
	}
}

func (wallet *Wallet) IsWaiting() bool {
	return wallet.waiting
}

func (wallet *Wallet) IsSynced() bool {
	return wallet.synced
}

func (wallet *Wallet) IsSyncing() bool {
	return wallet.syncing
}

func (mw *MultiWallet) IsConnectedToDecredNetwork() bool {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()
	return mw.syncData.syncing || mw.syncData.synced
}

func (mw *MultiWallet) IsSynced() bool {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()
	return mw.syncData.synced
}

func (mw *MultiWallet) IsSyncing() bool {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()
	return mw.syncData.syncing
}

func (mw *MultiWallet) CurrentSyncStage() int32 {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()

	if mw.syncData != nil && mw.syncData.syncing {
		return mw.syncData.syncStage
	}
	return InvalidSyncStage
}

func (mw *MultiWallet) ConnectedPeers() int32 {
	mw.syncData.mu.RLock()
	defer mw.syncData.mu.RUnlock()
	return mw.syncData.connectedPeers
}

func (mw *MultiWallet) GetBestBlock() *BlockInfo {
	var bestBlock int32 = -1
	var blockInfo *BlockInfo
	for _, wallet := range mw.wallets {
		if !wallet.WalletOpened() {
			continue
		}

		walletBestBLock := wallet.GetBestBlock()
		if walletBestBLock > bestBlock || bestBlock == -1 {
			bestBlock = walletBestBLock
			blockInfo = &BlockInfo{Height: bestBlock, Timestamp: wallet.GetBestBlockTimeStamp()}
		}
	}

	return blockInfo
}

func (mw *MultiWallet) GetLowestBlock() *BlockInfo {
	var lowestBlock int32 = -1
	var blockInfo *BlockInfo
	for _, wallet := range mw.wallets {
		if !wallet.WalletOpened() {
			continue
		}
		walletBestBLock := wallet.GetBestBlock()
		if walletBestBLock < lowestBlock || lowestBlock == -1 {
			lowestBlock = walletBestBLock
			blockInfo = &BlockInfo{Height: lowestBlock, Timestamp: wallet.GetBestBlockTimeStamp()}
		}
	}

	return blockInfo
}

func (wallet *Wallet) GetBestBlock() int32 {
	if wallet.internal == nil {
		// This method is sometimes called after a wallet is deleted and causes crash.
		log.Error("Attempting to read best block height without a loaded wallet.")
		return 0
	}

	blk := wallet.internal.Manager.SyncedTo()
	return blk.Height
}

func (wallet *Wallet) GetBestBlockTimeStamp() int64 {
	if wallet.internal == nil {
		// This method is sometimes called after a wallet is deleted and causes crash.
		log.Error("Attempting to read best block timestamp without a loaded wallet.")
		return 0
	}

	blk := wallet.internal.Manager.SyncedTo()
	return blk.Timestamp.UnixNano()
}

func (mw *MultiWallet) GetLowestBlockTimestamp() int64 {
	var timestamp int64 = -1
	for _, wallet := range mw.wallets {
		bestBlockTimestamp := wallet.GetBestBlockTimeStamp()
		if bestBlockTimestamp < timestamp || timestamp == -1 {
			timestamp = bestBlockTimestamp
		}
	}
	return timestamp
}

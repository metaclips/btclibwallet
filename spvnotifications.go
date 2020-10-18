package dcrlibwallet

import (
	// "github.com/btcsuite/btcutil"
	// "github.com/btcsuite/btcwallet/wtxmgr"
	"math"
	"time"

	"github.com/lightninglabs/neutrino"
)

func (mw *MultiWallet) spvSyncNotificationCallbacks() *neutrino.Notifications {
	// sync uupdate mu
	return &neutrino.Notifications{
		PeerConnected: func(peerCount int32, addr string) {
			mw.handlePeerCountUpdate(peerCount)
		},
		PeerDisconnected: func(peerCount int32, addr string) {
			mw.handlePeerCountUpdate(peerCount)
		},
		Synced: func(synced bool) {
			log.Infof("Synced: %v", synced)
		},
		FetchMissingCFiltersStarted:  mw.fetchCFiltersStarted,
		FetchMissingCFiltersProgress: mw.fetchCFiltersProgress,
		FetchMissingCFiltersFinished: func() {

			wallet := mw.WalletWithID(1)
			wallet.internal.SetChainSynced(true)

			log.Infof("FetchMissingCFiltersFinished Synced: %v, Syncing: %v", wallet.internal.ChainSynced(), wallet.internal.SynchronizingToNetwork())
			mw.synced()

			// balance, err := wallet.GetAccountBalance(0)
			// if err != nil {
			// 	log.Error(err)
			// } else {
			// 	// for i := 0; i < 200; i++ {
			// 	// 	_, err = wallet.NextAddress(0)
			// 	// 	if err != nil {
			// 	// 		log.Error(err)
			// 	// 	}
			// 	// }
			// 	// log.Info("Generated 200 addresses")

			// 	// addr, err := wallet.NextAddress(0)
			// 	// if err != nil {
			// 	// 	log.Error(err)
			// 	// }

			// 	addrs, err := wallet.internal.AccountAddresses(0)
			// 	if err != nil {
			// 		log.Error(err)
			// 	}

			// 	unspent, err := wallet.internal.ListUnspent(0, 100*100, make(map[string]struct{}))
			// 	if err != nil {
			// 		log.Error(err)
			// 	}

			// 	for _, u := range unspent {
			// 		log.Infof("Amount: %f BTC Tx: %s", u.Amount, u.TxID)
			// 	}
			// 	// log.Info("starting rescan")
			// 	// err = wallet.internal.Rescan(addrs, []wtxmgr.Credit{})
			// 	// if err != nil {
			// 	// 	log.Error(err)
			// 	// }
			// 	log.Infof("Address: %s, Balance: %s, Addrs: %d, Synced: %v", "addr", btcutil.Amount(balance.Total), len(addrs), wallet.IsSynced())
			// }
		},
	}
}

func (mw *MultiWallet) handlePeerCountUpdate(peerCount int32) {
	log.Infof("Peer count: %d", peerCount)
	mw.syncData.mu.RLock()
	mw.syncData.connectedPeers = peerCount
	mw.syncData.mu.RUnlock()

}

func (mw *MultiWallet) fetchCFiltersStarted() {
	log.Info("FetchMissingCFiltersStarted Syncing:", mw.IsSyncing())
	if !mw.IsSyncing() {
		return
	}

	mw.syncData.mu.RLock()
	cFiltersFetchingStarted := mw.syncData.beginFetchTimeStamp != -1
	// showLogs := mw.syncData.showLogs
	mw.syncData.mu.RUnlock()

	if cFiltersFetchingStarted {
		return
	}

	wallet := mw.WalletWithID(1)
	bestBlock := wallet.GetBestBlock()
	log.Info("Wallet best block:", bestBlock, wallet.IsSynced())

	mw.syncData.mu.Lock()
	mw.syncData.activeSyncData.syncStage = CFiltersFetchSyncStage
	mw.syncData.activeSyncData.beginFetchTimeStamp = time.Now().Unix()
	mw.syncData.activeSyncData.startCFiltersHeight = bestBlock
	mw.syncData.mu.Unlock()
}

func (mw *MultiWallet) fetchCFiltersProgress(missingCFiltersStart, missingCFiltersEnd int32, lastFetchedCFiltersTime int64) {
	sessionFetchedCFilters := missingCFiltersEnd - missingCFiltersStart
	log.Infof("FetchMissingCFiltersProgress: %d of %d  Syncing: %v", sessionFetchedCFilters, missingCFiltersEnd+mw.estimateBlockHeadersCountAfter(lastFetchedCFiltersTime), mw.IsSyncing())

	if !mw.IsSyncing() {
		return
	}

	mw.syncData.mu.RLock()
	cFiltersFetchingCompleted := mw.syncData.activeSyncData.cFiltersFetchTimeSpent != -1
	mw.syncData.mu.RUnlock()

	if cFiltersFetchingCompleted {
		log.Info("Progress returning:", cFiltersFetchingCompleted)
		// This function gets called for each newly connected peer so ignore
		// this call if the headers fetching phase was previously completed.
		return
	}

	wallet := mw.WalletWithID(1)
	wallet.waiting = false

	// lock the mutex before reading and writing to mw.syncData.*
	mw.syncData.mu.Lock()

	// cFiltersLeftToFetch := mw.estimateBlockHeadersCountAfter(lastFetchedCFiltersTime)
	estimatedRemainingCFilters := mw.estimateBlockHeadersCountAfter(lastFetchedCFiltersTime)
	log.Infof("Start: %d Fetched: %d, End: %d, Estimate Left: %d", missingCFiltersStart, sessionFetchedCFilters, missingCFiltersEnd, estimatedRemainingCFilters)
	totalFetchedCFilters := sessionFetchedCFilters
	sessionTotalCfilters := (missingCFiltersEnd - missingCFiltersStart) + estimatedRemainingCFilters
	cFiltersFetchProgress := float64(totalFetchedCFilters) / float64(sessionTotalCfilters)
	// todo: above equation is potentially flawed because `lastFetchedHeaderHeight`
	// may not be the total number of headers fetched so far in THIS sync operation.
	// it may include headers previously fetched.
	// probably better to compare number of headers fetched so far in THIS sync operation
	// against the estimated number of headers left to fetch in THIS sync operation
	// in order to determine the headers fetch progress so far in THIS sync operation.

	// If there was some period of inactivity,
	// assume that this process started at some point in the future,
	// thereby accounting for the total reported time of inactivity.
	mw.syncData.activeSyncData.beginFetchTimeStamp += mw.syncData.activeSyncData.totalInactiveSeconds
	mw.syncData.activeSyncData.totalInactiveSeconds = 0

	timeTakenSoFar := time.Now().Unix() - mw.syncData.activeSyncData.beginFetchTimeStamp
	if timeTakenSoFar < 1 {
		timeTakenSoFar = 1
	}
	estimatedTotalCFiltersFetchTime := float64(timeTakenSoFar) / cFiltersFetchProgress

	// For some reason, the actual total headers fetch time is more than the predicted/estimated time.
	// Account for this difference by multiplying the estimatedTotalHeadersFetchTime by an incrementing factor.
	// The incrementing factor is inversely proportional to the headers fetch progress,
	// ranging from 0.5 to 0 as headers fetching progress increases from 0 to 1.
	// todo, the above noted (mal)calculation may explain this difference.
	supposed := estimatedTotalCFiltersFetchTime
	adjustmentFactor := 0.5 * (1 - cFiltersFetchProgress)
	estimatedTotalCFiltersFetchTime += estimatedTotalCFiltersFetchTime * adjustmentFactor

	mw.syncData.activeSyncData.cFiltersFetchProgress.FetchedCFilters = sessionFetchedCFilters
	mw.syncData.activeSyncData.cFiltersFetchProgress.TotalCFiltersToFetch = estimatedRemainingCFilters
	mw.syncData.activeSyncData.cFiltersFetchProgress.LastCFiltersTimestamp = lastFetchedCFiltersTime
	mw.syncData.activeSyncData.cFiltersFetchProgress.TotalSyncProgress = roundUp(cFiltersFetchProgress * 100.0)
	mw.syncData.activeSyncData.cFiltersFetchProgress.TotalTimeRemainingSeconds = int64(estimatedTotalCFiltersFetchTime) - timeTakenSoFar

	log.Infof("Syncing %d%% Time taken: %d, remaining: %d(was %d)", roundUp(cFiltersFetchProgress*100.0), timeTakenSoFar,
		mw.syncData.activeSyncData.cFiltersFetchProgress.TotalTimeRemainingSeconds, int64(supposed)-timeTakenSoFar)
	// unlock the mutex before issuing notification callbacks to prevent potential deadlock
	// if any invoked callback takes a considerable amount of time to execute.
	mw.syncData.mu.Unlock()

	mw.publishCFiltersFetchProgress()
}

func (mw *MultiWallet) publishCFiltersFetchProgress() {
	for _, syncProgressListener := range mw.syncProgressListeners() {
		syncProgressListener.OnCFiltersFetchProgress(&mw.syncData.activeSyncData.cFiltersFetchProgress)
	}
}

func (mw *MultiWallet) estimateBlockHeadersCountAfter(lastHeaderTime int64) int32 {
	// Use the difference between current time (now) and last reported block time,
	// to estimate total headers to fetch.
	timeDifferenceInSeconds := float64(time.Now().Unix() - lastHeaderTime)
	targetTimePerBlockInSeconds := mw.chainParams.TargetTimePerBlock.Seconds()
	estimatedHeadersDifference := timeDifferenceInSeconds / targetTimePerBlockInSeconds

	log.Infof("lastHeaderTime: %d, timeDifferenceInSeconds: %f, estimatedHeadersDifference: %d", lastHeaderTime, timeDifferenceInSeconds, int32(math.Ceil(estimatedHeadersDifference)))
	// return next integer value (upper limit) if estimatedHeadersDifference is a fraction
	return int32(math.Ceil(estimatedHeadersDifference))
}

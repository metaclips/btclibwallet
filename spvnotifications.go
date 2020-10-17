package dcrlibwallet

import (
	"math"
	"time"

	"github.com/lightninglabs/neutrino"
)

func (mw *MultiWallet) spvSyncNotificationCallbacks() *neutrino.Notifications {
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
			log.Info("FetchMissingCFiltersFinished")
			for _, syncProgressListener := range mw.syncProgressListeners() {
				syncProgressListener.OnSyncCompleted()
			}
		},
		TipChanged: func(timestamp int64, height int32) {
			wallet := mw.WalletWithID(1)
			_, bestBlock, _ := mw.chainClient.GetBestBlock()
			log.Infof("Tip changed: %d, Syncing: %v, Synced: %v, Chain: %d", height, wallet.internal.SynchronizingToNetwork(), wallet.IsSynced(), bestBlock)
		},
	}
}

func (mw *MultiWallet) handlePeerCountUpdate(peerCount int32) {
	log.Infof("Peer count: %d", peerCount)
	wallet := mw.WalletWithID(1)
	log.Info("Wallet best block:", wallet.GetBestBlock(), wallet.IsSynced())
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
	mw.syncData.totalFetchedCFiltersCount = 0
	mw.syncData.mu.Unlock()
}

func (mw *MultiWallet) fetchCFiltersProgress(missingCFiltersStart, fetchedCFilters, missingCFiltersEnd int32, lastFetchedCFiltersTime int64) {
	log.Infof("FetchMissingCFiltersProgress: %d of %d  Syncing: %v", fetchedCFilters, missingCFiltersEnd+mw.estimateBlockHeadersCountAfter(lastFetchedCFiltersTime), mw.IsSyncing())

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

	if fetchedCFilters > 0 { // might remove check
		mw.syncData.activeSyncData.totalFetchedCFiltersCount = fetchedCFilters
	}

	// cFiltersLeftToFetch := mw.estimateBlockHeadersCountAfter(lastFetchedCFiltersTime)
	totalCFiltersToFetch := missingCFiltersEnd - (missingCFiltersStart + fetchedCFilters)
	cFiltersFetchProgress := float64(mw.syncData.activeSyncData.totalFetchedCFiltersCount) / float64(totalCFiltersToFetch)
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
	adjustmentFactor := 0.5 * (1 - cFiltersFetchProgress)
	estimatedTotalCFiltersFetchTime += estimatedTotalCFiltersFetchTime * adjustmentFactor

	mw.syncData.activeSyncData.cFiltersFetchProgress.FetchedCFilters = fetchedCFilters
	mw.syncData.activeSyncData.cFiltersFetchProgress.TotalCFiltersToFetch = totalCFiltersToFetch
	mw.syncData.activeSyncData.cFiltersFetchProgress.LastCFiltersTimestamp = lastFetchedCFiltersTime
	mw.syncData.activeSyncData.cFiltersFetchProgress.TotalSyncProgress = roundUp(cFiltersFetchProgress * 100.0)
	mw.syncData.activeSyncData.cFiltersFetchProgress.TotalTimeRemainingSeconds = int64(estimatedTotalCFiltersFetchTime) - timeTakenSoFar

	log.Infof("Syncing %d Time remaining: %d", mw.syncData.activeSyncData.cFiltersFetchProgress.TotalSyncProgress, mw.syncData.activeSyncData.cFiltersFetchProgress.TotalTimeRemainingSeconds)

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

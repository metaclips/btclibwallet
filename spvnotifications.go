package btclibwallet

import (
	"math"
	"time"

	"golang.org/x/sync/errgroup"
)

func (mw *MultiWallet) PeerConnected(peerCount int32, addr string) {
	mw.handlePeerCountUpdate(peerCount)
}

func (mw *MultiWallet) PeerDisconnected(peerCount int32, addr string) {
	mw.handlePeerCountUpdate(peerCount)
}

func (mw *MultiWallet) handlePeerCountUpdate(peerCount int32) {
	log.Infof("Peer count: %d", peerCount)
	mw.syncData.mu.RLock()
	mw.syncData.connectedPeers = peerCount
	mw.syncData.mu.RUnlock()
}

func (mw *MultiWallet) FetchMissingCFiltersStarted() {
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

func (mw *MultiWallet) FetchMissingCFiltersProgress(missingCFiltersStart, missingCFiltersEnd int32, lastFetchedCFiltersTime int64) {
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

func (mw *MultiWallet) FetchMissingCFiltersFinished() {
	wallet := mw.WalletWithID(1)

	_, bestBlock, _ := mw.chainClient.GetBestBlock()
	log.Infof("Wallet best block: %d, Chain bestBlock: %d", wallet.GetBestBlock(), bestBlock)
	log.Infof("FetchMissingCFiltersFinished")

	log.Info("Starting sync")

	err := mw.chainClient.StartupSync()
	if err != nil {
		log.Info("Error occurred sync startup:", err)
	}
}

func (mw *MultiWallet) estimateBlockHeadersCountAfter(lastHeaderTime int64) int32 {
	// Use the difference between current time (now) and last reported block time,
	// to estimate total headers to fetch.
	timeDifferenceInSeconds := float64(time.Now().Unix() - lastHeaderTime)
	targetTimePerBlockInSeconds := mw.chainParams.TargetTimePerBlock.Seconds()
	estimatedHeadersDifference := timeDifferenceInSeconds / targetTimePerBlockInSeconds

	// return next integer value (upper limit) if estimatedHeadersDifference is a fraction
	return int32(math.Ceil(estimatedHeadersDifference))
}

func (mw *MultiWallet) RescanStarted() {
	log.Info("Rescan started")
}

func (mw *MultiWallet) RescanProgress(rescannedThrough int32) {
	wallet := mw.WalletWithID(1)

	_, bestBlock, _ := mw.chainClient.GetBestBlock()

	rescanProgressReport := &RescanProgressReport{
		TotalHeadersToScan:  bestBlock,
		CurrentRescanHeight: rescannedThrough,
		RescanProgress:      (rescannedThrough / bestBlock) * 100,
	}

	for _, syncProgressListener := range mw.syncProgressListeners() {
		syncProgressListener.OnRescanProgress(rescanProgressReport)
	}

	log.Infof("rescan progress %d of %d, Synced: %v",
		rescannedThrough, bestBlock, wallet.internal.ChainSynced())
}

func (mw *MultiWallet) RescanFinished() {
	_, bestBlock, _ := mw.chainClient.GetBestBlock()
	wallet := mw.WalletWithID(1)
	log.Infof("Wallet best block: %d, Chain bestBlock: %d", wallet.GetBestBlock(), bestBlock)
	log.Info("Rescan done, Synced: ", wallet.internal.ChainSynced())
}

func (mw *MultiWallet) Synced() {

	wallet := mw.wallets[1]
	mw.listenForTransactions(wallet.ID)
	log.Info("Synced")

	mw.syncData.mu.Lock()
	mw.syncData.syncing = false
	mw.syncData.synced = true
	mw.syncData.mu.Unlock()

	// begin indexing transactions after sync is completed,
	// syncProgressListeners.OnSynced() will be invoked after transactions are indexed
	var txIndexing errgroup.Group
	txIndexing.Go(wallet.IndexTransactions)
	go func() {
		err := txIndexing.Wait()
		if err != nil {
			log.Errorf("Tx Index Error: %v", err)
		}

		for _, syncProgressListener := range mw.syncProgressListeners() {
			syncProgressListener.OnSyncCompleted()
		}
	}()
}

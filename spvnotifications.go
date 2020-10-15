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
		FetchMissingCFiltersStarted: func() {
			log.Info("FetchMissingCFiltersStarted")
		},
		FetchMissingCFiltersProgress: func(missingCFitlersStart, fetchedCfiltlers, missingCFiltlersEnd int32, endCFiltlersTime int64) {
			log.Infof("FetchMissingCFiltersProgress: %d of %d", fetchedCfiltlers, missingCFiltlersEnd+mw.estimateBlockHeadersCountAfter(endCFiltlersTime))

			mw.syncData.activeSyncData.cFiltersFetchProgress = CFiltersFetchProgressReport{
				FetchedCfiltlers:      fetchedCfiltlers,
				TotalCFitlersToFetch:  missingCFiltlersEnd - missingCFitlersStart,
				LastCFiltersTimestamp: endCFiltlersTime,
			}
			mw.publishCFiltersFetchProgress()
		},
		FetchMissingCFiltersFinished: func() {
			log.Info("FetchMissingCFiltersFinished")
			for _, syncProgressListener := range mw.syncProgressListeners() {
				syncProgressListener.OnSyncCompleted()
			}
		},
	}
}

func (mw *MultiWallet) handlePeerCountUpdate(peerCount int32) {
	log.Infof("Peer count: %d", peerCount)
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

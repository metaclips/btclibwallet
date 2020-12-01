package wallettest

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/btcsuite/btcutil"

	blw "github.com/c-ollins/btclibwallet"
)

type wallet struct {
	mw           *blw.MultiWallet
	seed         string
	seeWalletID  uint
	syncComplete chan struct{}
	syncErrored  chan error
}

const charset = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func stringWithCharset(length int) string {
	var seededRand *rand.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func generateAddresses(t *testing.T, wallet *blw.Wallet) {
	if wallet == nil {
		t.Error("invalid wallet generated")
	}

	addr, err := wallet.CurrentAddress(0)
	if err != nil {
		t.Error("error getting current address, error:", err)
	}

	newAddr, err := wallet.NextAddress(1)
	if err != nil {
		t.Error("error getting new address, error:", err)
	}

	if addr == newAddr {
		t.Error("error creating a new address")
	}

	nextNewAddr, err := wallet.NextAddress(1)

	if nextNewAddr == newAddr {
		t.Error("error creating a multiple new address")
	}

	if _, err := wallet.NextAddress(math.MaxInt32); err == nil {
		t.Error("wallet generated address on an invalid account")
	}
}

func TestRunWallet(t *testing.T) {
	seed, err := blw.GenerateSeed()
	if err != nil {
		t.Error("error generating wallet seed")
	}

	m, err := blw.NewMultiWallet(btcutil.AppDataDir("BTC", false), "testnet3")
	if err != nil {
		t.Error("error creating multiwallet")
	}

	mw := wallet{
		seed:         seed,
		mw:           m,
		syncComplete: make(chan struct{}),
		syncErrored:  make(chan error),
	}

	mw.createNewWallet(t)
	mw.waitForSyncComplete(t)

	generateAddresses(t, mw.mw.WalletWithID(1))
}

func (w *wallet) createNewWallet(t *testing.T) {

	if err := w.mw.OpenWallets(nil); err != nil {
		t.Error("error opening old wallets, error:", err)
	}

	t.Log("opened all wallets")

	ww, err := w.mw.RestoreWallet(stringWithCharset(5), w.seed, "Test", 0)
	if err != nil {
		t.Error("error creating new wallet, error:", err, ww)
	}

	w.seeWalletID = uint(ww.ID)

	if len(w.mw.AllWallets()) == 0 {
		t.Error("error saving wallet")
	}

	if w.mw.WalletWithID(math.MaxInt64) != nil {
		t.Error("invalid wallet generation")
	}
}

// ToDo: Test for cancel sync.
func (w *wallet) waitForSyncComplete(t *testing.T) {
	if err := w.mw.SpvSync(); err != nil {
		t.Error("error syncing using spv, error:", err)
	}

	w.mw.EnableSyncLogs()
	if err := w.mw.AddSyncProgressListener(w, "Test"); err != nil {
		t.Error("error creating sync listener")
	}

	select {
	case <-w.syncComplete:
		t.Log("sync completed")

	case err := <-w.syncErrored:
		t.Error("error syncing wallets, error:", err)
	}
}

func (w *wallet) addExistingWallet(t *testing.T) {

}

func (w *wallet) sendTransaction(t *testing.T) {}

func (w *wallet) waitReceiveTransaction(t *testing.T) {}

func (w *wallet) OnSyncCompleted() {
	w.syncComplete <- struct{}{}
}

func (w *wallet) OnSyncEndedWithError(err error) {
	w.syncErrored <- err
}

func (w *wallet) OnSyncStarted(wasRestarted bool)                                                {}
func (w *wallet) OnPeerConnectedOrDisconnected(numberOfConnectedPeers int32)                     {}
func (w *wallet) OnCFiltersFetchProgress(cFiltersFetchProgress *blw.CFiltersFetchProgressReport) {}
func (w *wallet) OnRescanProgress(rescanProgressReport *blw.RescanProgressReport)                {}
func (w *wallet) OnSyncCanceled(willRestart bool)                                                {}

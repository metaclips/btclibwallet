package dcrlibwallet

type WalletsIterator struct {
	currentIndex int
	wallets      []*Wallet
}

type BlockInfo struct {
	Height    int32
	Timestamp int64
}

type Amount struct {
	AtomValue int64
	BtcValue  float64
}

type TxFeeAndSize struct {
	Fee                 *Amount
	EstimatedSignedSize int
}

type UnsignedTransaction struct {
	UnsignedTransaction       []byte
	EstimatedSignedSize       int
	ChangeIndex               int
	TotalOutputAmount         int64
	TotalPreviousOutputAmount int64
}

type Balance struct {
	Total          int64
	Spendable      int64
	ImmatureReward int64
}

type Account struct {
	WalletID         int
	Number           int32
	Name             string
	Balance          *Balance
	TotalBalance     int64
	ExternalKeyCount int32
	InternalKeyCount int32
	ImportedKeyCount int32
}

type AccountsIterator struct {
	currentIndex int
	accounts     []*Account
}

type Accounts struct {
	Count              int
	Acc                []*Account
	CurrentBlockHash   []byte
	CurrentBlockHeight int32
}

/** begin sync-related types */

type SyncProgressListener interface {
	OnSyncStarted(wasRestarted bool)
	OnPeerConnectedOrDisconnected(numberOfConnectedPeers int32)
	OnCFiltersFetchProgress(cFiltersFetchProgress *CFiltersFetchProgressReport)
	OnSyncCompleted()
	OnSyncCanceled(willRestart bool)
	OnSyncEndedWithError(err error)
}

type GeneralSyncProgress struct {
	TotalSyncProgress         int32 `json:"totalSyncProgress"`
	TotalTimeRemainingSeconds int64 `json:"totalTimeRemainingSeconds"`
}

type CFiltersFetchProgressReport struct {
	*GeneralSyncProgress
	FetchedCFilters      int32
	TotalCFiltersToFetch  int32
	LastCFiltersTimestamp int64
}

/** end sync-related types */

/** begin tx-related types */

type TxAndBlockNotificationListener interface {
	OnTransaction(transaction string)
	OnBlockAttached(walletID int, blockHeight int32)
	OnTransactionConfirmed(walletID int, hash string, blockHeight int32)
}

// Transaction is used with storm for tx indexing operations.
// For faster queries, the `Hash`, `Type` and `Direction` fields are indexed.
type Transaction struct {
	WalletID    int    `json:"walletID"`
	Hash        string `storm:"id,unique" json:"hash"`
	Type        string `storm:"index" json:"type"`
	Hex         string `json:"hex"`
	Timestamp   int64  `json:"timestamp"`
	BlockHeight int32  `json:"block_height"`

	Version  int32 `json:"version"`
	LockTime int32 `json:"lock_time"`
	Fee      int64 `json:"fee"`
	Size     int   `json:"size"`

	Direction int32       `storm:"index" json:"direction"`
	Amount    int64       `json:"amount"`
	Inputs    []*TxInput  `json:"inputs"`
	Outputs   []*TxOutput `json:"outputs"`
}

type TxInput struct {
	PreviousTransactionHash  string `json:"previous_transaction_hash"`
	PreviousTransactionIndex int32  `json:"previous_transaction_index"`
	PreviousOutpoint         string `json:"previous_outpoint"`
	Amount                   int64  `json:"amount"`
	AccountName              string `json:"account_name"`
	AccountNumber            int32  `json:"account_number"`
}

type TxOutput struct {
	Index         int32  `json:"index"`
	Amount        int64  `json:"amount"`
	ScriptType    string `json:"script_type"`
	Address       string `json:"address"`
	Internal      bool   `json:"internal"`
	AccountName   string `json:"account_name"`
	AccountNumber int32  `json:"account_number"`
}

// TxInfoFromWallet contains tx data that relates to the querying wallet.
// This info is used with `DecodeTransaction` to compose the entire details of a transaction.
type TxInfoFromWallet struct {
	WalletID    int
	Hex         string
	Fee         int64
	Timestamp   int64
	BlockHeight int32
	Inputs      []*WalletInput
	Outputs     []*WalletOutput
}

type WalletInput struct {
	Index    int32 `json:"index"`
	AmountIn int64 `json:"amount_in"`
	*WalletAccount
}

type WalletOutput struct {
	Index     int32  `json:"index"`
	AmountOut int64  `json:"amount_out"`
	Internal  bool   `json:"internal"`
	Address   string `json:"address"`
	*WalletAccount
}

type WalletAccount struct {
	AccountNumber int32  `json:"account_number"`
	AccountName   string `json:"account_name"`
}

type TransactionDestination struct {
	Address    string
	AtomAmount int64
	SendMax    bool
}

/** end tx-related types */

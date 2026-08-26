package transaction

import (
	"fmt"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	concurrencymanager "github.com/JunNishimura/GoSQL/concurrency_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
	recoverymanager "github.com/JunNishimura/GoSQL/recovery_manager"
)

// Transaction is one unit of work against the database. It ties together the
// three things a unit of work needs: a recovery manager to log what it changed,
// a concurrency manager to keep other transactions out of what it is using, and
// a list of the buffers it has pinned.
//
// The buffer pool and the lock table behind those are shared by every
// transaction, which is how they coordinate. What this type holds is what
// belongs to this transaction alone.
type Transaction struct {
	fileManager        *filemanager.FileManager
	bufferManager      *buffermanager.BufferManager
	recoveryManager    *recoverymanager.RecoveryManager
	concurrencyManager *concurrencymanager.ConcurrencyManager
	buffers            *buffermanager.BufferList
	// txNum identifies this transaction in the log and on the buffers it
	// modifies. The recovery manager keeps its own copy for the records it
	// writes; this one is for stamping buffers.
	txNum int
}

// NewTransaction starts transaction txNum, which puts a start record on the log.
//
// The log manager and the lock table are only needed to build the recovery and
// concurrency managers, so they are not kept. txNum comes from the caller
// because handing out numbers belongs to whatever owns the database as a whole,
// alongside the one lock table every transaction shares.
func NewTransaction(
	fm *filemanager.FileManager,
	lm *logmanager.LogManager,
	bm *buffermanager.BufferManager,
	lt *concurrencymanager.LockTable,
	txNum int,
) (*Transaction, error) {
	rm, err := recoverymanager.NewRecoveryManager(lm, bm, txNum)
	if err != nil {
		return nil, fmt.Errorf("start tx %d: %w", txNum, err)
	}

	return &Transaction{
		fileManager:        fm,
		bufferManager:      bm,
		recoveryManager:    rm,
		concurrencyManager: concurrencymanager.NewConcurrencyManager(lt),
		buffers:            buffermanager.NewBufferList(bm),
		txNum:              txNum,
	}, nil
}

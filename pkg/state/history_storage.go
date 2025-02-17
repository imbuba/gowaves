package state

import (
	"encoding/binary"

	"github.com/pkg/errors"
	"github.com/wavesplatform/gowaves/pkg/keyvalue"
)

var errEmptyHist = errors.New("empty history for this record")

type blockchainEntity byte

const (
	alias blockchainEntity = iota
	asset
	lease
	wavesBalance
	assetBalance
	featureVote
	approvedFeature
	activatedFeature

	idSize = 4
)

var recordSizes = map[blockchainEntity]int{
	alias:            aliasRecordSize,
	asset:            assetRecordSize,
	lease:            leasingRecordSize,
	wavesBalance:     wavesBalanceRecordSize,
	assetBalance:     assetBalanceRecordSize,
	featureVote:      votesFeaturesRecordSize,
	approvedFeature:  approvedFeaturesRecordSize,
	activatedFeature: activatedFeaturesRecordSize,
}

type historyStorage struct {
	db         keyvalue.IterableKeyVal
	dbBatch    keyvalue.Batch
	stor       *localStorage
	stateDB    *stateDB
	rw         *blockReadWriter
	formatters map[blockchainEntity]historyFormatter
}

func newHistoryStorage(
	db keyvalue.IterableKeyVal,
	dbBatch keyvalue.Batch,
	rw *blockReadWriter,
	stateDB *stateDB,
) (*historyStorage, error) {
	stor, err := newLocalStorage()
	if err != nil {
		return nil, err
	}
	formatters := make(map[blockchainEntity]historyFormatter)
	for entity, size := range recordSizes {
		fmt, err := newHistoryFormatter(size, idSize, stateDB, rw)
		if err != nil {
			return nil, err
		}
		formatters[entity] = *fmt
	}
	return &historyStorage{db, dbBatch, stor, stateDB, rw, formatters}, nil
}

func (hs *historyStorage) set(entityType blockchainEntity, key, value []byte) error {
	history, err := hs.stor.get(key)
	if err != nil && err != errNotFound {
		return err
	}
	fmt, ok := hs.formatters[entityType]
	if !ok {
		return errors.Errorf("unknown entity type %v\n", entityType)
	}
	history, err = fmt.addRecord(history, value)
	if err != nil {
		return err
	}
	if err = hs.stor.set(entityType, key, history); err != nil {
		return err
	}
	return nil
}

func (hs *historyStorage) getFresh(entityType blockchainEntity, key []byte, filter bool) ([]byte, error) {
	fmt, ok := hs.formatters[entityType]
	if !ok {
		return nil, errors.Errorf("unknown entity type %v\n", entityType)
	}
	history, err := hs.fullHistory(key, fmt, filter)
	if err != nil {
		return nil, err
	}
	if len(history) == 0 {
		return nil, errEmptyHist
	}
	return fmt.getLatest(history)
}

func (hs *historyStorage) cleanDbRecord(key []byte) error {
	// If the history is empty after normalizing, it means that all the records were removed due to rollback.
	// In this case, it should be removed from the DB as well.
	return hs.db.Delete(key)
}

func (hs *historyStorage) get(entityType blockchainEntity, key []byte, filter bool) ([]byte, error) {
	history, err := hs.db.Get(key)
	if err != nil {
		return nil, err
	}
	fmt, ok := hs.formatters[entityType]
	if !ok {
		return nil, errors.Errorf("unknown entity type %v\n", entityType)
	}
	history, err = fmt.normalize(history, filter)
	if err != nil {
		return nil, err
	}
	if len(history) == 0 {
		if err := hs.cleanDbRecord(key); err != nil {
			return nil, err
		}
		return nil, errEmptyHist
	}
	return fmt.getLatest(history)
}

func (hs *historyStorage) combineHistories(key, newHist []byte, fmt historyFormatter, filter bool) ([]byte, error) {
	prevHist, err := hs.db.Get(key)
	if err == keyvalue.ErrNotFound {
		// New history.
		return newHist, nil
	}
	if err != nil {
		return nil, err
	}
	lenBeforeNormalization := len(prevHist)
	prevHist, err = fmt.normalize(prevHist, filter)
	if err != nil {
		return nil, err
	}
	if len(prevHist) != lenBeforeNormalization {
		if err := hs.db.Put(key, prevHist); err != nil {
			return nil, err
		}
	}
	return append(prevHist, newHist...), nil
}

// fullHistory returns combination of history from DB and the local storage (if any).
func (hs *historyStorage) fullHistory(key []byte, fmt historyFormatter, filter bool) ([]byte, error) {
	newHist, err := hs.stor.get(key)
	if err != errNotFound && err != nil {
		return nil, err
	}
	return hs.combineHistories(key, newHist, fmt, filter)
}

func (hs *historyStorage) recordsInHeightRange(entityType blockchainEntity, key []byte, startHeight, endHeight uint64, filter bool) ([][]byte, error) {
	fmt, ok := hs.formatters[entityType]
	if !ok {
		return nil, errors.Errorf("unknown entity type %v\n", entityType)
	}
	history, err := hs.fullHistory(key, fmt, filter)
	if err != nil {
		return nil, err
	}
	recordSize, ok := recordSizes[entityType]
	if !ok {
		return nil, errors.Errorf("unknown entity type %v\n", entityType)
	}
	foundAtLeastOne := false
	var records [][]byte
	for i := len(history); i >= recordSize; i -= recordSize {
		recordBytes := history[i-recordSize : i]
		idBytes, err := fmt.getID(recordBytes)
		if err != nil {
			return nil, err
		}
		blockNum := binary.BigEndian.Uint32(idBytes)
		blockID, err := hs.stateDB.blockNumToId(blockNum)
		if err != nil {
			return nil, err
		}
		height, err := hs.rw.newestHeightByBlockID(blockID)
		if err != nil {
			return nil, err
		}
		if height > endHeight {
			continue
		}
		if height < startHeight && foundAtLeastOne {
			break
		}
		foundAtLeastOne = true
		records = append(records, recordBytes)
	}
	return records, nil
}

func (hs *historyStorage) reset() {
	hs.stor.reset()
}

func (hs *historyStorage) flush(filter bool) error {
	entries := hs.stor.getEntries()
	sortEntries(entries)
	for _, entry := range entries {
		fmt, ok := hs.formatters[entry.entityType]
		if !ok {
			return errors.Errorf("unknown entity type %v\n", entry.entityType)
		}
		newEntry, err := hs.combineHistories(entry.key, entry.value, fmt, filter)
		if err != nil {
			return err
		}
		hs.dbBatch.Put(entry.key, newEntry)
	}
	hs.stor.reset()
	return nil
}

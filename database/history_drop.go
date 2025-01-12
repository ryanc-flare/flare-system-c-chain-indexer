package database

import (
	"context"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/logger"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/cenkalti/backoff/v4"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

func DropHistory(
	ctx context.Context, db *gorm.DB, intervalSeconds, checkInterval uint64, client ethclient.Client,
) {
	for {
		err := dropHistoryIteration(ctx, db, intervalSeconds, checkInterval, client)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				logger.Error(err.Error())
			}
		}

		time.Sleep(time.Duration(checkInterval) * time.Second)
	}
}

func dropHistoryIteration(
	ctx context.Context, db *gorm.DB, intervalSeconds, checkInterval uint64, client ethclient.Client,
) error {
	lastBlockTime, _, err := getBlockTimestamp(ctx, nil, client)
	if err != nil {
		return errors.Wrap(err, "Failed to get the latest time")
	}

	deleteStart := lastBlockTime - intervalSeconds

	return db.Transaction(func(tx *gorm.DB) error {
		lastTx := new(Transaction)
		err = tx.Where("timestamp < ?", deleteStart).Order("block_number desc").First(lastTx).Error
		if err != nil {
			return errors.Wrap(err, "Failed to check historic data in the DB")
		}

		// delete in reverse to not break foreign keys
		for i := len(entities) - 1; i >= 1; i-- {
			entity := entities[i]
			err = tx.Where("timestamp < ?", deleteStart).Delete(&entity).Error
			if err != nil {
				return errors.Wrap(err, "Failed to delete historic data in the DB")
			}
		}

		firstTx := new(Transaction)
		err = tx.Where("timestamp >= ?", deleteStart).Order("block_number").First(firstTx).Error
		if err != nil {
			return errors.Wrap(err, "Failed to get first transaction in the DB: %s")
		}

		err = globalStates.Update(tx, FirstDatabaseIndexState, firstTx.BlockNumber, firstTx.Timestamp)
		if err != nil {
			return errors.Wrap(err, "Failed to update state in the DB")
		}

		logger.Info("Deleted blocks up to index %d", lastTx.BlockNumber)
		return nil
	})
}

func GetMinBlockWithHistoryDrop(
	ctx context.Context, firstIndex, intervalSeconds uint64, client ethclient.Client,
) (uint64, error) {
	firstTime, _, err := getBlockTimestamp(ctx, new(big.Int).SetUint64(firstIndex), client)
	if err != nil {
		return 0, errors.Wrap(err, "GetMinBlockWithHistoryDrop")
	}

	lastTime, endIndex, err := getBlockTimestamp(ctx, nil, client)
	if err != nil {
		return 0, errors.Wrap(err, "GetMinBlockWithHistoryDrop")
	}

	if lastTime-firstTime < intervalSeconds {
		return firstIndex, nil
	}

	for endIndex-firstIndex > 1 {
		newIndex := (firstIndex + endIndex) / 2

		newTime, _, err := getBlockTimestamp(ctx, new(big.Int).SetUint64(newIndex), client)
		if err != nil {
			return 0, errors.Wrap(err, "GetMinBlockWithHistoryDrop")
		}

		if lastTime-newTime < intervalSeconds {
			endIndex = newIndex
		} else {
			firstIndex = newIndex
		}
	}

	return firstIndex, nil
}

func getBlockTimestamp(ctx context.Context, index *big.Int, client ethclient.Client) (uint64, uint64, error) {
	bOff := backoff.NewExponentialBackOff()
	bOff.MaxElapsedTime = config.BackoffMaxElapsedTime

	var block *types.Block
	err := backoff.RetryNotify(
		func() (err error) {
			ctx, cancelFunc := context.WithTimeout(ctx, config.Timeout)
			defer cancelFunc()

			block, err = client.BlockByNumber(ctx, index)
			return err
		},
		bOff,
		func(err error, _ time.Duration) {
			logger.Error("getBlockTimestamp error: %s", err)
		},
	)

	if err != nil {
		return 0, 0, fmt.Errorf("getBlockTimestamp: %w", err)
	}

	return block.Time(), block.Number().Uint64(), nil
}

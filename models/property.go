package models

import (
	"context"
	"github.com/shopspring/decimal"
	"time"

	"github.com/MixinNetwork/supergroup/session"
	"github.com/jackc/pgx/v4"
)

const properties_DDL = `
CREATE TABLE IF NOT EXISTS properties (
	key         VARCHAR(512) PRIMARY KEY,
	value       VARCHAR(8192) NOT NULL,
	updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
`

const (
	MainnetSnapshotsCheckpoint = "service-mainnet-snapshots-checkpoint"
)

type Property struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

func ReadProperty(ctx context.Context, key string) (string, error) {
	var val string
	query := "SELECT value FROM properties WHERE key=$1"
	err := session.Database(ctx).ConnQueryRow(ctx, query, func(row pgx.Row) error {
		return row.Scan(&val)
	})
	return val, err
}

func WriteProperty(ctx context.Context, key, value string) error {
	query := "INSERT INTO properties (key,value,updated_at) VALUES($1,$2,$3) ON CONFLICT (key) DO UPDATE SET (value,updated_at)=(EXCLUDED.value, EXCLUDED.updated_at)"
	return session.Database(ctx).ConnExec(ctx, query, key, value, time.Now())
}

func CleanModelCache() {
	cacheClientAssetLevel = make(map[string]*ClientAssetLevel)
	cacheAssets = make(map[string]*Asset)
	cacheClientLpCheckList = make(map[string]map[string]decimal.Decimal)
	cacheClient = make(map[string]*Client)
	cacheHostClientMap = make(map[string]*MixinClient)
	cacheIdClientMap = make(map[string]*MixinClient)
	cacheManagerMap = make(map[string][]string)
	cacheQuoteMsgID = make(map[string]map[string]string)
	cacheOriginMsgID = make(map[string]string)
	cacheClientReplay = make(map[string]*ClientReplay)
	cacheFirstClient = nil
	cacheClientIDLastMsgMap = make(map[string]*Message)
	cacheBlockClientUserIDMap = make(map[string]map[string]bool)
}

func cleanCache() {
	for {
		time.Sleep(time.Minute * 15)
		CleanModelCache()
	}
}

func init() {
	go cleanCache()
}

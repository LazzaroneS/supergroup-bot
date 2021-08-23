package models

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/MixinNetwork/supergroup/tools"

	"github.com/MixinNetwork/supergroup/config"
	"github.com/MixinNetwork/supergroup/durable"
	"github.com/MixinNetwork/supergroup/session"
	"github.com/fox-one/mixin-sdk-go"
	"github.com/jackc/pgx/v4"
	"github.com/shopspring/decimal"
)

const client_DDL = `
-- 机器人信息
CREATE TABLE IF NOT EXISTS client (
  client_id          VARCHAR(36) NOT NULL PRIMARY KEY,
  client_secret      VARCHAR NOT NULL,
  session_id         VARCHAR(36) NOT NULL,
  pin_token          VARCHAR NOT NULL,
  private_key        VARCHAR NOT NULL,
  pin                VARCHAR(6) DEFAULT '',
  name               VARCHAR NOT NULL,
  icon_url           VARCHAR NOT NULL DEFAULT '',
  description        VARCHAR NOT NULL,
  host               VARCHAR NOT NULL, -- 前端部署的 host
  asset_id           VARCHAR(36) NOT NULL,
	owner_id					 VARCHAR(36) NOT NULL,
  speak_status       SMALLINT NOT NULL DEFAULT 1, -- 1 正常发言 2 持仓发言
	pay_status				 SMALLINT NOT NULL DEFAULT 0, -- 0 关闭 1 开启
	pay_amount			   VARCHAR NOT NULL DEFAULT '', -- 付费入群开启的金额
  created_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
)
`

type Client struct {
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	PinToken     string    `json:"pin_token,omitempty"`
	PrivateKey   string    `json:"private_key,omitempty"`
	Pin          string    `json:"pin,omitempty"`
	Name         string    `json:"name,omitempty"`
	Description  string    `json:"description,omitempty"`
	Host         string    `json:"host,omitempty"`
	AssetID      string    `json:"asset_id,omitempty"`
	OwnerID      string    `json:"owner_id,omitempty"`
	SpeakStatus  int       `json:"speak_status,omitempty"`
	PayStatus    int       `json:"pay_status,omitempty"`
	PayAmount    string    `json:"pay_amount,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`

	IconURL string `json:"icon_url,omitempty"`
	Symbol  string `json:"symbol,omitempty"`
	Welcome string `json:"welcome,omitempty"`
	Amount  string `json:"amount,omitempty"`
}

const (
	ClientSpeckStatusOpen  = 1 // 持仓发言打开，
	ClientSpeckStatusClose = 2 // 持仓发言关闭

	ClientPayStatusOpen = 1 // 入群开启，

)

func UpdateClientSetting(ctx context.Context, u *ClientUser, desc, welcome string) error {
	if !checkIsAdmin(ctx, u.ClientID, u.UserID) {
		return session.ForbiddenError(ctx)
	}
	if desc != "" {
		if _, err := session.Database(ctx).Exec(ctx, `
UPDATE client SET description=$2 WHERE client_id=$1
`, u.ClientID, desc); err != nil {
			return err
		}
	}
	if welcome != "" {
		if err := updateClientWelcome(ctx, u.ClientID, welcome); err != nil {
			return err
		}
		go func() {
			// 给管理员发两条消息
			SendToClientManager(u.ClientID, &mixin.MessageView{
				ConversationID: mixin.UniqueConversationID(u.ClientID, u.UserID),
				UserID:         u.UserID,
				MessageID:      tools.GetUUID(),
				Category:       mixin.MessageCategoryPlainText,
				Data:           tools.Base64Encode([]byte(config.Text.WelcomeUpdate)),
				CreatedAt:      time.Now(),
			}, false, false)
			SendToClientManager(u.ClientID, &mixin.MessageView{
				ConversationID: mixin.UniqueConversationID(u.ClientID, u.UserID),
				UserID:         u.UserID,
				MessageID:      tools.GetUUID(),
				Category:       mixin.MessageCategoryPlainText,
				Data:           tools.Base64Encode([]byte(welcome)),
				CreatedAt:      time.Now(),
			}, false, false)
		}()
	}
	cacheClient = make(map[string]Client)
	cacheClientReplay = make(map[string]ClientReplay)
	return nil
}

func UpdateClient(ctx context.Context, c *Client) error {
	if strings.HasSuffix(c.Host, "/") {
		c.Host = c.Host[:len(c.Host)-1]
	}
	query := durable.InsertQueryOrUpdate("client", "client_id", "client_secret,session_id,pin_token,private_key,pin,name,icon_url,description,asset_id,host,speak_status,owner_id")
	_, err := session.Database(ctx).Exec(ctx, query, c.ClientID, c.ClientSecret, c.SessionID, c.PinToken, c.PrivateKey, c.Pin, c.Name, c.IconURL, c.Description, c.AssetID, c.Host, c.SpeakStatus, c.OwnerID)
	return err
}

var cacheClient = make(map[string]Client)
var nilClient = Client{}

func GetClientByID(ctx context.Context, clientID string) (Client, error) {
	if cacheClient[clientID] == nilClient {
		var c Client
		if err := session.Database(ctx).QueryRow(ctx, `
SELECT client_id,session_id,pin_token,private_key,pin,name,description,speak_status,host,asset_id,icon_url,owner_id,pay_status,pay_amount
FROM client 
WHERE client.client_id=$1`,
			clientID).Scan(&c.ClientID, &c.SessionID, &c.PinToken, &c.PrivateKey, &c.Pin, &c.Name, &c.Description, &c.SpeakStatus, &c.Host, &c.AssetID, &c.IconURL, &c.OwnerID, &c.PayStatus, &c.PayAmount); err != nil {
			return c, err
		}
		cacheClient[clientID] = c
	}
	return cacheClient[clientID], nil
}

func GetClientList(ctx context.Context) ([]*Client, error) {
	clientList := make([]*Client, 0)
	err := session.Database(ctx).ConnQuery(ctx, `
SELECT client_id, session_id,pin_token,private_key,pin,speak_status,asset_id,created_at
FROM client
WHERE client_id=ANY($1)
`, func(rows pgx.Rows) error {
		for rows.Next() {
			var c Client
			if err := rows.Scan(&c.ClientID, &c.SessionID, &c.PinToken, &c.PrivateKey, &c.Pin, &c.SpeakStatus, &c.AssetID, &c.CreatedAt); err != nil {
				return err
			}
			clientList = append(clientList, &c)
		}
		return nil
	}, config.Config.ClientList)
	return clientList, err
}

var cacheFirstClient *mixin.Client

func GetFirstClient(ctx context.Context) *mixin.Client {
	if cacheFirstClient == nil {
		c, err := getAllClient(ctx)
		if err != nil {
			return nil
		}
		cacheFirstClient = GetMixinClientByID(ctx, c[0]).Client
	}
	return cacheFirstClient
}

type MixinClient struct {
	*mixin.Client
	Secret      string
	AssetID     string
	SpeakStatus int
	Host        string
}

var cacheHostClientMap = make(map[string]MixinClient)
var nilHostClientMap = MixinClient{}

func GetMixinClientByHost(ctx context.Context, host string) MixinClient {
	if cacheHostClientMap[host] == nilHostClientMap {
		var keystore mixin.Keystore
		var secret, assetID string
		var speakStatus int
		err := session.Database(ctx).QueryRow(ctx, `
SELECT client_id,client_secret,session_id,pin_token,private_key,speak_status,asset_id
FROM client WHERE host=$1
`, host).Scan(&keystore.ClientID, &secret, &keystore.SessionID, &keystore.PinToken, &keystore.PrivateKey, &speakStatus, &assetID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				session.Logger(ctx).Println(host, "...Host NOT FOUND")
			}
			return MixinClient{}
		}
		client, err := mixin.NewFromKeystore(&keystore)
		if err != nil {
			session.Logger(ctx).Println(err)
			return MixinClient{}
		}
		cacheHostClientMap[host] = MixinClient{Client: client, Secret: secret, SpeakStatus: speakStatus, AssetID: assetID}
	}
	return cacheHostClientMap[host]
}

var cacheIdClientMap = make(map[string]MixinClient)
var nilIDClientMap = MixinClient{}

func GetMixinClientByID(ctx context.Context, clientID string) MixinClient {
	if cacheIdClientMap[clientID] == nilIDClientMap {
		var keystore mixin.Keystore
		var secret, assetID, host string
		var speakStatus int
		err := session.Database(ctx).QueryRow(ctx, `
SELECT client_id,client_secret,session_id,pin_token,private_key,speak_status,asset_id,host
FROM client WHERE client_id=$1
`, clientID).Scan(&keystore.ClientID, &secret, &keystore.SessionID, &keystore.PinToken, &keystore.PrivateKey, &speakStatus, &assetID, &host)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				session.Logger(ctx).Println(err)
			}
			return MixinClient{}
		}
		client, err := mixin.NewFromKeystore(&keystore)
		if err != nil {
			session.Logger(ctx).Println(err)
			return MixinClient{}
		}
		cacheIdClientMap[clientID] = MixinClient{Client: client, Secret: secret, SpeakStatus: speakStatus, AssetID: assetID, Host: host}
	}
	return cacheIdClientMap[clientID]
}

var cachePinMap = make(map[string]string)

func getMixinPinByID(ctx context.Context, clientID string) (string, error) {
	if cachePinMap[clientID] == "" {
		var pin string
		if err := session.Database(ctx).QueryRow(ctx, `
SELECT pin FROM client WHERE client_id=$1
`, clientID).Scan(&pin); err != nil {
			return "", err
		}
		cachePinMap[clientID] = pin
	}
	return cachePinMap[clientID], nil
}

func GetClientStatusByID(ctx context.Context, u *ClientUser) string {
	return getClientConversationStatus(ctx, u.ClientID)
}

type ClientInfo struct {
	*Client
	PriceUsd      decimal.Decimal `json:"price_usd,omitempty"`
	ChangeUsd     string          `json:"change_usd,omitempty"`
	TotalPeople   decimal.Decimal `json:"total_people"`
	WeekPeople    decimal.Decimal `json:"week_people"`
	Activity      []*Activity     `json:"activity,omitempty"`
	HasReward     bool            `json:"has_reward"`
	NeedPayAmount decimal.Decimal `json:"need_pay_amount,omitempty"`
	LargeAmount   string          `json:"large_amount,omitempty"`
}

const (
	ExinOneClientID = "47cdbc9e-e2b9-4d1f-b13e-42fec1d8853d"
	XinAssetID      = "c94ac88f-4671-3976-b60a-09064f1811e8"
	BtcAssetID      = "c6d0c728-2624-429b-8e0d-d9d19b6592fa"
)

func GetClientInfoByHostOrID(ctx context.Context, host, id string) (*ClientInfo, error) {
	var mixinClient MixinClient
	if id != "" {
		mixinClient = GetMixinClientByID(ctx, id)
	} else {
		mixinClient = GetMixinClientByHost(ctx, host)
	}
	client, err := GetClientByID(ctx, mixinClient.ClientID)
	if err != nil {
		return nil, err
	}
	var c ClientInfo
	if client.Pin != "" {
		c.HasReward = true
	}
	client.SessionID = ""
	client.PinToken = ""
	client.PrivateKey = ""
	client.Pin = ""
	c.Client = &client
	assetID := client.AssetID
	if c.ClientID == ExinOneClientID {
		assetID = XinAssetID
	} else if assetID == "" {
		assetID = BtcAssetID
	}
	asset, err := GetAssetByID(ctx, mixinClient.Client, assetID)
	if err == nil {
		c.PriceUsd = asset.PriceUsd
		c.ChangeUsd = asset.ChangeUsd
		c.Symbol = asset.Symbol
		if client.AssetID != "" {
			c.IconURL = asset.IconUrl
		}
	}
	r, err := GetClientReplay(client.ClientID)
	if err == nil {
		c.Welcome = r.Welcome
	}
	amount, err := GetClientAssetLevel(ctx, client.ClientID)
	if err == nil {
		c.Amount = amount.Fresh.String()
		c.LargeAmount = amount.Large.String()
	}

	c.TotalPeople, c.WeekPeople, err = getClientPeopleCount(ctx, client.ClientID)
	if err != nil {
		return nil, err
	}
	c.Activity, err = GetActivityByClientID(ctx, client.ClientID)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

var cacheAllClient = make([]ClientInfo, 0)

func GetAllClientInfo(ctx context.Context) ([]ClientInfo, error) {
	if len(cacheAllClient) == 0 {
		cis := make([]ClientInfo, 0)
		if err := session.Database(ctx).ConnQuery(ctx, `
SELECT client_id FROM client WHERE client_id=ANY($1)
`, func(rows pgx.Rows) error {
			for rows.Next() {
				var clientID string
				if err := rows.Scan(&clientID); err != nil {
					return err
				}
				if ci, err := GetClientInfoByHostOrID(ctx, "", clientID); err != nil {
					return err
				} else {
					cis = append(cis, *ci)
				}
			}
			return nil
		}, config.Config.ShowClientList); err != nil {
			return nil, err
		}
		cacheAllClient = cis
	}
	return cacheAllClient, nil
}

func getAllClient(ctx context.Context) ([]string, error) {
	cs := make([]string, 0)
	err := session.Database(ctx).ConnQuery(ctx, `
SELECT client_id FROM client
`, func(rows pgx.Rows) error {
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			cs = append(cs, c)
		}
		return nil
	})
	return cs, err
}

var _ctx context.Context

func init() {
	_ctx = session.WithDatabase(context.Background(), durable.NewDatabase(context.Background()))
	_ctx = session.WithRedis(_ctx, durable.NewRedis(context.Background()))
	initAllDDL()
}

func initAllDDL() {
	session.Database(_ctx).Exec(_ctx, `
alter table client add if not exists pay_amount varchar default '0';
alter table client add if not exists pay_status smallint default 0;
alter table client_asset_level add if not exists fresh_amount varchar default '0';
alter table client_asset_level add if not exists large_amount varchar default '0';
alter table client_users add if not exists pay_status smallint default 1;
alter table client_users add if not exists pay_expired_at TIMESTAMP WITH TIME ZONE default '1970-01-01 00:00:00+00';
`)
	session.Database(_ctx).Exec(_ctx, airdrop_DDL)
}

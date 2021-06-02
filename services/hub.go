package services

import (
	"context"
	"fmt"

	"github.com/MixinNetwork/supergroup/durable"
	"github.com/MixinNetwork/supergroup/session"
)

type Hub struct {
	context  context.Context
	services map[string]Service
}

func NewHub(db *durable.Database) *Hub {
	hub := &Hub{services: make(map[string]Service)}
	hub.context = session.WithDatabase(context.Background(), db)
	hub.context = session.WithMixinClient(hub.context, durable.GetMixinClient())
	hub.registerServices()
	return hub
}

func (hub *Hub) StartService(name string) error {
	service := hub.services[name]
	if service == nil {
		return fmt.Errorf("no service found: %s", name)
	}

	return service.Run(hub.context)
}

func (hub *Hub) registerServices() {
	hub.services["scan"] = &ScanService{}
	hub.services["distribute_message"] = &DistributeMessageService{}
	hub.services["blaze"] = &BlazeService{}
	hub.services["test"] = &TestService{}
	hub.services["assets_check"] = &AssetsCheckService{}
	hub.services["add_client"] = &AddClientService{}
	hub.services["swap"] = &SwapService{}
	hub.services["update_lp_check"] = &UpdateLpCheckService{}
}

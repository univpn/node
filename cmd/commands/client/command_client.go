package client

import (
	"fmt"
	log "github.com/cihub/seelog"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/mysterium/node/client_connection"
	"github.com/mysterium/node/cmd"
	"github.com/mysterium/node/communication"
	nats_dialog "github.com/mysterium/node/communication/nats/dialog"
	nats_discovery "github.com/mysterium/node/communication/nats/discovery"
	"github.com/mysterium/node/identity"
	"github.com/mysterium/node/ip"
	"github.com/mysterium/node/openvpn"
	"github.com/mysterium/node/openvpn/middlewares/client/bytescount"
	"github.com/mysterium/node/server"
	"github.com/mysterium/node/tequilapi"
	tequilapi_endpoints "github.com/mysterium/node/tequilapi/endpoints"
	"time"
)

// NewCommand function creates new client command by given options
func NewCommand(options CommandOptions, stop cmd.Stopper) *Command {
	return NewCommandWith(
		options,
		server.NewClient(),
		stop,
	)
}

// NewCommandWith does the same as NewCommand with possibility to override mysterium api client for external communication
func NewCommandWith(
	options CommandOptions,
	mysteriumClient server.Client,
	stop cmd.Stopper,
) *Command {
	nats_discovery.Bootstrap()
	openvpn.Bootstrap()

	keystoreInstance := keystore.NewKeyStore(options.DirectoryKeystore, keystore.StandardScryptN, keystore.StandardScryptP)

	identityManager := identity.NewIdentityManager(keystoreInstance)

	dialogEstablisherFactory := func(myID identity.Identity) communication.DialogEstablisher {
		return nats_dialog.NewDialogEstablisher(myID, identity.NewSigner(keystoreInstance, myID))
	}

	signerFactory := func(id identity.Identity) identity.Signer {
		return identity.NewSigner(keystoreInstance, id)
	}

	statsKeeper := bytescount.NewSessionStatsKeeper(time.Now)

	vpnClientFactory := client_connection.ConfigureVpnClientFactory(mysteriumClient, options.DirectoryRuntime, signerFactory, statsKeeper)

	connectionManager := client_connection.NewManager(mysteriumClient, dialogEstablisherFactory, vpnClientFactory, statsKeeper)

	router := tequilapi.NewAPIRouter()

	httpAPIServer := tequilapi.NewServer(options.TequilapiAddress, options.TequilapiPort, router)

	command := &Command{
		stop:              stop,
		connectionManager: connectionManager,
		httpAPIServer:     httpAPIServer,
	}

	tequilapi_endpoints.AddRoutesForIdentities(router, identityManager, mysteriumClient, signerFactory)
	ipResolver := ip.NewResolver()
	tequilapi_endpoints.AddRoutesForConnection(router, connectionManager, ipResolver, statsKeeper)
	tequilapi_endpoints.AddRoutesForProposals(router, mysteriumClient)
	tequilapi_endpoints.AddRouteForStop(router, command.stopAfterDelay)

	return command
}

//Command represent entrypoint for Mysterium client with top level components
type Command struct {
	connectionManager client_connection.Manager
	httpAPIServer     tequilapi.APIServer
	stop              cmd.Stopper
}

// Run starts Tequilapi service - does not block
func (cmd *Command) Run() error {
	err := cmd.httpAPIServer.StartServing()
	if err != nil {
		return err
	}

	port, err := cmd.httpAPIServer.Port()
	if err != nil {
		return err
	}
	fmt.Printf("Api started on: %d\n", port)

	return nil
}

// Wait blocks until tequilapi service is stopped
func (cmd *Command) Wait() error {
	return cmd.httpAPIServer.Wait()
}

// Kill stops tequilapi service
func (cmd *Command) Kill() error {
	err := cmd.connectionManager.Disconnect()
	if err != nil {
		return err
	}

	cmd.httpAPIServer.Stop()
	fmt.Printf("Api stopped\n")

	return nil
}

func (cmd *Command) stopAfterDelay() {
	log.Info("Client is stopping")
	delay(cmd.stop, 1*time.Second)
}

func delay(function func(), duration time.Duration) {
	go func() {
		time.Sleep(duration)
		function()
	}()
}
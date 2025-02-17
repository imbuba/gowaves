package main

import (
	"context"
	"github.com/alecthomas/kong"
	"github.com/wavesplatform/gowaves/pkg/api"
	"github.com/wavesplatform/gowaves/pkg/miner"
	scheduler2 "github.com/wavesplatform/gowaves/pkg/miner/scheduler"
	"github.com/wavesplatform/gowaves/pkg/miner/utxpool"
	"github.com/wavesplatform/gowaves/pkg/settings"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wavesplatform/gowaves/pkg/libs/bytespool"
	"github.com/wavesplatform/gowaves/pkg/node"
	"github.com/wavesplatform/gowaves/pkg/p2p/peer"
	"github.com/wavesplatform/gowaves/pkg/proto"
	"github.com/wavesplatform/gowaves/pkg/state"
	"go.uber.org/zap"
	"strings"
)

var version = proto.Version{Major: 0, Minor: 15, Patch: 1}

type Cli struct {
	Run struct {
		WavesNetwork string `kong:"wavesnetwork,short='n',help='Waves network.',required"`
		Addresses    string `kong:"address,short='a',help='Addresses connect to.'"`
		DeclAddr     string `kong:"decladdr,short='d',help='Address listen on.'"`
		HttpAddr     string `kong:"httpaddr,short='w',help='Http addr bind on.'"`
		GenesisPath  string `kong:"genesis,short='g',help='Path to genesis json file.'"`
		Seed         string `kong:"seed,help='Seed for miner.'"`
	} `kong:"cmd,help='Run node'"`
}

func init() {
	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	zap.S().Info(os.Args)
	zap.S().Info(os.Environ())
	zap.S().Info(os.LookupEnv("WAVES_OPTS"))

	var cli Cli
	kong.Parse(&cli)

	conf := &settings.NodeSettings{}
	settings.ApplySettings(conf,
		FromArgs(&cli),
		settings.FromJavaEnviron)

	zap.S().Info("conf", conf)

	err := conf.Validate()
	if err != nil {
		zap.S().Error(err)
		cancel()
		return
	}

	custom := &settings.BlockchainSettings{
		Type: 'E',
		FunctionalitySettings: settings.FunctionalitySettings{
			MaxTxTimeBackOffset:    120 * 60000,
			MaxTxTimeForwardOffset: 90 * 60000,

			AddressSchemeCharacter: 'E',

			AverageBlockDelaySeconds: 60,
			MaxBaseTarget:            200,
		},
		GenesisGetter: settings.FromPath(cli.Run.GenesisPath),
	}

	state, err := state.NewState("./", state.DefaultStateParams(), custom)
	if err != nil {
		zap.S().Error(err)
		cancel()
		return
	}

	declAddr := proto.NewTCPAddrFromString(conf.DeclaredAddr)

	mb := 1024 * 1014
	btsPool := bytespool.NewBytesPool(64, mb+(mb/2))

	parent := peer.NewParent()

	peerSpawnerImpl := node.NewPeerSpawner(btsPool, parent, conf.WavesNetwork, declAddr, "gowaves", uint64(rand.Int()), version)

	peerManager := node.NewPeerManager(peerSpawnerImpl, state)

	var keyPairs []proto.KeyPair
	if len(cli.Run.Seed) > 0 {
		keyPairs = append(keyPairs, proto.NewKeyPair([]byte(cli.Run.Seed)))
	}

	scheduler := scheduler2.NewScheduler(state, keyPairs, custom)

	utx := utxpool.New(10000)
	Mainer := miner.New(utx, state, peerManager, scheduler)

	go miner.Run(ctx, Mainer, scheduler)
	go scheduler.Reschedule()

	n := node.NewNode(state, peerManager, declAddr, scheduler, Mainer)

	go node.RunNode(ctx, n, parent)

	if len(conf.Addresses) > 0 {
		adrs := strings.Split(conf.Addresses, ",")
		for _, addr := range adrs {
			peerManager.AddAddress(ctx, addr)
		}
	}

	// TODO hardcore
	app, err := api.NewApp("integration-test-rest-api", n, scheduler)
	if err != nil {
		zap.S().Error(err)
		cancel()
		return
	}

	webApi := api.NewNodeApi(app, state, n)
	go func() {
		zap.S().Info("===== ", conf.HttpAddr)
		err := api.Run(ctx, conf.HttpAddr, webApi)
		if err != nil {
			zap.S().Error("Failed to start API: %v", err)
		}
	}()

	var gracefulStop = make(chan os.Signal)
	signal.Notify(gracefulStop, syscall.SIGTERM)
	signal.Notify(gracefulStop, syscall.SIGINT)

	select {
	case sig := <-gracefulStop:

		n.Close()

		zap.S().Infow("Caught signal, stopping", "signal", sig)
		cancel()

		<-time.After(2 * time.Second)
	}

}

func FromArgs(c *Cli) func(s *settings.NodeSettings) {
	return func(s *settings.NodeSettings) {
		s.DeclaredAddr = c.Run.DeclAddr
		s.HttpAddr = c.Run.HttpAddr
		s.WavesNetwork = c.Run.WavesNetwork
		s.Addresses = c.Run.Addresses
	}
}

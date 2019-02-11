package httpserver

import (
	"context"
	"encoding/json"
	"github.com/gorilla/mux"
	"github.com/wavesplatform/gowaves/pkg/network/retransmit"
	"github.com/wavesplatform/gowaves/pkg/network/retransmit/utils"
	"github.com/wavesplatform/gowaves/pkg/proto"
	"go.uber.org/zap"
	"net/http"
	"sort"
)

type HttpServer struct {
	retransmitter *retransmit.Retransmitter
	srv           http.Server
}

func NewHttpServer(r *retransmit.Retransmitter) *HttpServer {
	return &HttpServer{
		retransmitter: r,
	}
}

type ActiveConnection struct {
	Addr       string        `json:"addr"`
	DeclAddr   string        `json:"decl_addr"`
	Direction  string        `json:"direction"`
	RemoteAddr string        `json:"remote_addr"`
	LocalAddr  string        `json:"local_addr"`
	Version    proto.Version `json:"version"`
}

type ActiveConnections []ActiveConnection

func (a ActiveConnections) Len() int           { return len(a) }
func (a ActiveConnections) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ActiveConnections) Less(i, j int) bool { return a[i].Addr < a[j].Addr }

type FullState struct {
	Active  []ActiveConnection
	Spawned []string
	Known   []string
}

func (a *HttpServer) ActiveConnections(rw http.ResponseWriter, r *http.Request) {
	var out ActiveConnections
	addr2peer := a.retransmitter.ActiveConnections()
	addr2peer.Each(func(id string, p *utils.PeerInfo) {
		out = append(out, ActiveConnection{
			Addr:       id,
			Direction:  p.Peer.Direction().String(),
			DeclAddr:   p.DeclAddr.String(),
			RemoteAddr: p.RemoteAddr,
			LocalAddr:  p.LocalAddr,
			Version:    p.Version,
		})
	})

	sort.Sort(out)

	bts, err := json.Marshal(out)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		rw.Write([]byte(err.Error()))
		return
	}

	rw.WriteHeader(http.StatusOK)
	rw.Write(bts)
}

func (a *HttpServer) KnownPeers(rw http.ResponseWriter, r *http.Request) {
	out := a.retransmitter.KnownPeers().GetAll()
	bts, err := json.Marshal(out)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		rw.Write([]byte(err.Error()))
		return
	}

	rw.WriteHeader(http.StatusOK)
	rw.Write(bts)
}

func (a *HttpServer) Spawned(rw http.ResponseWriter, r *http.Request) {
	out := a.retransmitter.SpawnedPeers().GetAll()
	bts, err := json.Marshal(out)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		rw.Write([]byte(err.Error()))
		return
	}

	rw.WriteHeader(http.StatusOK)
	rw.Write(bts)
}

func (a *HttpServer) counter(rw http.ResponseWriter, r *http.Request) {

	c := a.retransmitter.Counter()
	out := c.Get()
	zap.S().Info(out)
	bts, err := json.Marshal(out)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		rw.Write([]byte(err.Error()))
		return
	}

	rw.WriteHeader(http.StatusOK)
	rw.Write(bts)

}

func (a *HttpServer) ListenAndServe() error {
	router := mux.NewRouter()
	router.HandleFunc("/active", a.ActiveConnections)
	router.HandleFunc("/known", a.KnownPeers)
	router.HandleFunc("/spawned", a.Spawned)
	router.HandleFunc("/counter", a.counter)
	http.Handle("/", router)

	a.srv = http.Server{
		Handler: router,
		Addr:    "0.0.0.0:8000",
	}
	return a.srv.ListenAndServe()
}

func (a *HttpServer) Shutdown(ctx context.Context) error {
	return a.srv.Shutdown(ctx)
}

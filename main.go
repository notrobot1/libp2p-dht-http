package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	gostream "github.com/libp2p/go-libp2p-gostream"
	p2phttp "github.com/libp2p/go-libp2p-http"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	"github.com/multiformats/go-multiaddr"
)

// Connection manager to limit connections
var connMgr, _ = connmgr.NewConnManager(100, 600, connmgr.WithGracePeriod(time.Minute))

// Extra options for libp2p
var Libp2pOptionsExtra = []libp2p.Option{
	libp2p.NATPortMap(),
	libp2p.ConnectionManager(connMgr),
	//libp2p.EnableAutoRelay(),
	libp2p.EnableNATService(),
}

func main() {
	ctx := context.Background()
	privKey, _ := LoadKeyFromFile()
	listen, _ := multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/0")

	// Initialize our host
	h, mydht, _, err := SetupLibp2p(
		ctx,
		privKey,
		nil,
		[]multiaddr.Multiaddr{listen},
		nil,
		Libp2pOptionsExtra...,
	)
	if err != nil {
		fmt.Println(err.Error())
	}
	defer h.Close()

	fmt.Println("My id: ", h.ID().String())
	fmt.Println("My address: ", h.Addrs())

	// Connect to a known host
	bootstrapHost, _ := multiaddr.NewMultiaddr("/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ")
	peerinfo, _ := peer.AddrInfoFromP2pAddr(bootstrapHost)
	err = h.Connect(ctx, *peerinfo)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	time.Sleep(5 * time.Second)

	for _, i := range h.Network().Peers() {
		log.Printf("   Connected peer ID: %s", i)
		peerInfo := h.Peerstore().PeerInfo(i)
		log.Printf("   Peer Addresses: %v", peerInfo.Addrs)
	}

	routingTable := mydht.RoutingTable()
	log.Printf("DHT routing table size: %d", routingTable.Size())

	// Iterating through all entries in the routing table
	for _, peerID := range routingTable.ListPeers() {
		log.Printf("Peer ID: %s", peerID)
	}
	fmt.Println("")

	provideCid := cid.NewCidV1(cid.Raw, []byte("unique_string"))

	// Provide a value in the DHT
	if err := mydht.Provide(ctx, provideCid, true); err != nil {
		log.Fatalf("Failed to provide value: %v", err)
		return
	}

	fmt.Println(mydht.RoutingTable().ListPeers())

	// Start the HTTP server
	listener, _ := gostream.Listen(h, p2phttp.DefaultP2PProtocol)
	defer listener.Close()
	go func() {
		http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("Hi!"))
		})
		server := &http.Server{}
		server.Serve(listener)
	}()

	// Find providers for the given CID
	providers, err := mydht.FindProviders(ctx, provideCid)
	if err != nil {
		log.Fatalf("Failed to find providers: %v", err)
	}

	// Output information about providers
	for _, provider := range providers {
		fmt.Printf("[+] Provider ID: %s\n", provider.ID)
		fmt.Printf("    Provider Addresses: %v\n", provider.Addrs)

		if provider.ID.String() != h.ID().String() {
			sendGet(provider.ID.String(), h)
		}
	}
  for{
    time.Sleep(10)
  }
}

// Configuring libp2p
func SetupLibp2p(ctx context.Context,
	hostKey crypto.PrivKey,
	secret pnet.PSK,
	listenAddrs []multiaddr.Multiaddr,
	ds datastore.Batching,
	opts ...libp2p.Option) (host.Host, *dht.IpfsDHT, peer.ID, error) {
	var ddht *dht.IpfsDHT

	var err error
	var transports = libp2p.DefaultTransports

	if secret != nil {
		transports = libp2p.ChainOptions(
			libp2p.NoTransports,
			libp2p.Transport(tcp.NewTCPTransport),
			libp2p.Transport(websocket.New),
		)
	}

	finalOpts := []libp2p.Option{
		libp2p.Identity(hostKey),
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.PrivateNetwork(secret),
		transports,
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			ddht, err = newDHT2(ctx, h, ds)
			return ddht, err
		}),
	}
	finalOpts = append(finalOpts, opts...)

	h, err := libp2p.New(
		finalOpts...,
	)
	if err != nil {
		return nil, nil, "", err
	}

	pid, _ := peer.IDFromPublicKey(hostKey.GetPublic())
	// Connect to default peers

	return h, ddht, pid, nil
}

// Create a new DHT instance
func newDHT2(ctx context.Context, h host.Host, ds datastore.Batching) (*dht.IpfsDHT, error) {
	var options []dht.Option

	// If no bootstrap peers, this peer acts as a bootstrapping node
	// Other peers can use this peer's IPFS address for peer discovery via DHT
	options = append(options, dht.Mode(dht.ModeAuto))

	kdht, err := dht.New(ctx, h, options...)
	if err != nil {
		return nil, err
	}

	if err = kdht.Bootstrap(ctx); err != nil {
		return nil, err
	}

	return kdht, nil
}

// Load keys from file or generate new ones
func LoadKeyFromFile() (crypto.PrivKey, crypto.PubKey) {
	privKey, err := os.ReadFile("key.priv")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			privKey, pubKey := saveKeyToFile()
			return privKey, pubKey
		} else {
			panic(err)
		}
	}

	pubKey, err := os.ReadFile("key.pub")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			privKey, pubKey := saveKeyToFile()
			return privKey, pubKey
		} else {
			panic(err)
		}
	}
	privKeyByte, err := crypto.UnmarshalPrivateKey(privKey)
	if err != nil {
		panic(err)
	}
	pubKeyByte, err := crypto.UnmarshalPublicKey(pubKey)
	if err != nil {
		panic(err)
	}
	return privKeyByte, pubKeyByte
}

// Save generated keys to files
func saveKeyToFile() (crypto.PrivKey, crypto.PubKey) {
	fmt.Println("[+] Generate keys")
	privKey, pubKey, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
	if err != nil {
		panic(err)
	}
	privKeyByte, _ := crypto.MarshalPrivateKey(privKey)
	pubKeyByte, _ := crypto.MarshalPublicKey(pubKey)
	err = os.WriteFile("key.priv", privKeyByte, 0644)
	if err != nil {
		// Handle error (optional)
	}
	err = os.WriteFile("key.pub", pubKeyByte, 0644)
	if err != nil {
		panic(err)
	}
	return privKey, pubKey
}

// Send a GET request to a peer
func sendGet(id string, h host.Host) {
	tr := &http.Transport{}
	tr.RegisterProtocol("libp2p", p2phttp.NewTransport(h))
	client := &http.Client{Transport: tr}
	res, err := client.Get("libp2p://" + id + "/hello")
	if err != nil {
		fmt.Println(err.Error())
	}
	defer res.Body.Close()
	text, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err.Error())
	}
	fmt.Println(string(text))
}

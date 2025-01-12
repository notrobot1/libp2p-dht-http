package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/routing"

	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
)

// Connection manager to limit connections
var connMgr, _ = connmgr.NewConnManager(1, 2, connmgr.WithGracePeriod(time.Minute))


// Extra options for libp2p
var Libp2pOptionsExtra = []libp2p.Option{
	libp2p.NATPortMap(),
	libp2p.ConnectionManager(connMgr),
	//libp2p.EnableAutoRelay(),
	libp2p.EnableNATService(),
}

func handleConnection(net network.Network, conn network.Conn) {

	// Here you can reject the connection based on the blacklist
	
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
		log.Panic(err.Error())

	}
	defer h.Close()

	fmt.Println("My id: ", h.ID().String())
	fmt.Println("My address: ", h.Addrs())

	h.Network().Notify(&network.NotifyBundle{
		ConnectedF: handleConnection,
	})

	h.SetStreamHandler("/myhttp/1.0.0", func(s network.Stream) {
		peerID := s.Conn().RemotePeer()

		// Here you can check the identifier in the database or cache if necessary.
		// Process requests via stream
		fmt.Println("Received stream data", peerID)
		buf := make([]byte, 1024)
		n, err := s.Read(buf)
		if err != nil {
			log.Println("Error reading from stream:", err)
			return
		}

		// Here you can process HTTP requests from the stream
		log.Printf("Received: %s", string(buf[:n]))

		// Sending the response back through the stream
		_, err = s.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nHello from libp2p HTTP server!"))
		if err != nil {
			log.Println("Error writing to stream:", err)
		}

		// Closing the stream
		s.Close()
	})

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
			sendRequestViaMyProtocol(h, provider.ID, "/myhttp/1.0.0", peerinfo.Addrs)
		}
	}

	var wg sync.WaitGroup
	wg.Add(1) // Add 1 goroutine to wait
	wg.Wait() // Block here until the WaitGroup is done
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
	//var transports = libp2p.NoTransports
	if secret != nil {
		transports = libp2p.ChainOptions(
			libp2p.NoTransports,
			libp2p.Transport(tcp.NewTCPTransport),
			//libp2p.Transport(websocket.New),
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

func sendRequestViaMyProtocol(h host.Host, peerID peer.ID, ProtocolID protocol.ID, peerAddr []multiaddr.Multiaddr) {
	// Connecting to a remote node
	if err := h.Connect(context.Background(), peer.AddrInfo{ID: peerID, Addrs: peerAddr}); err != nil {
		log.Fatal("Connection failed:", err)
	}

	// Sending HTTP request via libp2p
	stream, err := h.NewStream(context.Background(), peerID, ProtocolID)
	if err != nil {
		log.Fatal("Error creating stream:", err)
	}

	// Example of sending HTTP request via libp2p stream
	_, err = stream.Write([]byte("GET / HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"))
	if err != nil {
		log.Fatal("Error writing to stream:", err)
	}

	// close the stream
	stream.Close()
}

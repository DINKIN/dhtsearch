package main

import (
	"fmt"
	"net"
	"strconv"
	"time"
)

var (
	routers = []string{
		"router.bittorrent.com:6881",
		"dht.transmissionbt.com:6881",
		"router.utorrent.com:6881",
	}
)

type DHTNode struct {
	id           string
	debug        bool
	address      string
	port         int
	conn         *net.UDPConn
	tid          uint32
	kTable       kTable
	peerChan     chan<- peer
	packetsIn    chan packet
	packetsOut   chan packet
	workerTokens chan struct{}
}

// Unprocessed packet from socket
type packet struct {
	b     []byte
	raddr net.UDPAddr
}

func newDHTNode(address string, port int, p chan<- peer) (node *DHTNode) {
	node = &DHTNode{
		address:      address,
		port:         port,
		workerTokens: make(chan struct{}, 256),
		peerChan:     p,
	}

	// Get random id for this node
	node.id = genInfoHash()
	node.kTable = newKTable(address, port, node.id)
	return
}

func (d *DHTNode) run(done <-chan struct{}) error {
	listener, err := net.ListenPacket("udp4", d.address+":"+strconv.Itoa(d.port))
	if err != nil {
		fmt.Printf("Failed to listen: %s\n", err)
		return err
	}
	d.conn = listener.(*net.UDPConn)
	d.port = d.conn.LocalAddr().(*net.UDPAddr).Port

	if d.debug {
		fmt.Printf("We are node %x\n", d.id)
		fmt.Printf("Listening at %s:%d\n", d.address, d.port)
	}

	// Packets off the network
	d.packetsIn = make(chan packet)
	d.packetsOut = make(chan packet)

	// Create a slab for allocation
	// Adjust number to suit contention
	byteSlab := newSlab(8192, 10)

	// Start reading packets from conn into channel
	go func() {
		for {
			b := byteSlab.Alloc()
			c, addr, err := d.conn.ReadFromUDP(b)
			if err != nil {
				fmt.Println("UDP read error", err)
				continue
			}
			dhtBytesIn.Add(int64(c))
			dhtPacketsIn.Add(1)
			// Chop
			b = b[0:c]
			d.packetsIn <- packet{b, *addr}
			byteSlab.Free(b)
		}
	}()

	// Start writing packets from channel to conn
	go func() {
		var p packet
		for {
			select {
			case p = <-d.packetsOut:
				d.conn.SetWriteDeadline(time.Now().Add(time.Second * 10))
				b, err := d.conn.WriteToUDP(p.b, &p.raddr)
				if err != nil {
					dhtErrorPackets.Add(1)
					// TODO remove from kTAble
					if d.debug {
						fmt.Printf("Error writing packet %s\n", err)
					}
				}
				dhtBytesOut.Add(int64(b))
				dhtPacketsOut.Add(1)
			}
		}
	}()

	// TODO configurable
	ticker := time.Tick(5 * time.Second)

	// Read and process packets from incoming channel
	var p packet
	go func() {
		defer d.conn.Close()
		for {
			select {
			case <-done:
				fmt.Println("Stopping")
				return
			case p = <-d.packetsIn:
				d.processPacket(p)
			case <-ticker:
				go d.makeNeighbours()
			}
		}
	}()
	return nil
}

func (d *DHTNode) bootstrap() {
	if d.debug {
		fmt.Println("Bootstrapping")
	}
	for _, s := range routers {
		addr, err := net.ResolveUDPAddr("udp4", s)
		if err != nil {
			fmt.Printf("Error parsing bootstrap address: %s\n", err)
			return
		}
		rn := newRemoteNode(*addr, "")
		d.findNode(rn, "")
	}
}

func (d *DHTNode) makeNeighbours() {
	// TODO configurable
	if !d.kTable.isFull() {
		if d.debug {
			fmt.Println("Making neighbours")
		}
		if d.kTable.isEmpty() {
			d.bootstrap()
		} else {
			for _, rn := range d.kTable.getNodes() {
				d.findNode(rn, rn.id)
			}
			d.kTable.refresh()
		}
	}
}

func (d DHTNode) findNode(rn *remoteNode, target string) {
	var id string
	if target == "" {
		id = d.id
	} else {
		id = genNeighbour(d.id, target)
	}
	// fmt.Printf("Sending find_node msg id:%x target:%x addr:%s\n", id, target, rn.address)
	d.sendQuery(rn, "find_node", map[string]interface{}{
		"id":     id,
		"target": genInfoHash(),
	})
}

// ping sends ping query to the chan.
func (d *DHTNode) ping(rn *remoteNode) {
	d.sendQuery(rn, "ping", map[string]interface{}{
		"id": genNeighbour(d.id, rn.id),
	})
}

// Process another node's response to a find_node query.
func (d *DHTNode) processFindNodeResults(rn *remoteNode, nodeList string) {
	nodeLength := 26
	/*
		if d.config.proto == "udp6" {
			nodeList = m.R.Nodes6
			nodeLength = 38
		} else {
			nodeList = m.R.Nodes
		}

		// Not much to do
		if nodeList == "" {
			return
		}
	*/

	if len(nodeList)%nodeLength != 0 {
		fmt.Printf("Node list is wrong length, got %d\n", len(nodeList))
		return
	}

	// We got a byte array in groups of 26 or 38
	var count int = 0
	for i := 0; i < len(nodeList); i += nodeLength {
		id := nodeList[i : i+ihLength]
		addr := compactNodeInfoToString(nodeList[i+ihLength : i+nodeLength])
		//fmt.Printf("Node list entry %s @ %s\n", id, addr)

		//fmt.Printf("find_node response id len:%d address:%s\n", len(id), addr)

		if d.id == id {
			if d.debug {
				fmt.Println("find_nodes ignoring self")
			}
			continue
		}

		address, err := net.ResolveUDPAddr("udp4", addr)
		if err != nil {
			fmt.Printf("Error parsing node from find_find response: %s\n", err)
			continue
		}
		// TODO check IP and ports are valid and not self
		rn := newRemoteNode(*address, id)
		d.kTable.add(rn)
		count = count + 1
		// TODO check size of kTable
	}
}

package main

import (
	"context"
	godsutils "github.com/emirpasic/gods/utils"
	"github.com/go-zookeeper/zk"
	"github.com/google/uuid"
	"github.com/vmihailenco/msgpack/v5"
	"github.com/ztrue/tracerr"
	"log"
	"strings"
	"sync/atomic"
	"time"
	"zk-intro/zk_client"
)

const (
	zkRoot             = "/myService"
	alivePath          = zkRoot + "/n"
	evtPath            = zkRoot + "/e"
	dataForJoinedPath  = evtPath + "/fj-"
	separator          = "/"
	nodeJoinRetryCount = 10
)

type ZkDiscovery struct {
	client         *zk_client.ZkClient
	DiscoEvents    chan DiscoveryEvent
	ErrorEvents    chan error
	evtsProcessed  uint64
	Joined         uint32
	alives         *SyncTreeMap
	localNode      *Node
	leaderNode     atomic.Value
	localNodePath  string
	sessionTimeout time.Duration
	stopChan       chan interface{}
}

type LastProcessedEvent struct {
	nodeOrder uint64
	evtId     uint64
}

func Create(servers []string, sessionTimeout time.Duration) (*ZkDiscovery, error) {
	client, err := zk_client.NewZkClient(servers, sessionTimeout)
	if err != nil {
		return nil, tracerr.Wrap(err)
	}

	if err := client.MkDir(alivePath, true); err != nil {
		return nil, tracerr.Wrap(err)
	}

	if err = client.MkDir(evtPath, true); err != nil {
		return nil, tracerr.Wrap(err)
	}

	disco := &ZkDiscovery{
		client:         client,
		sessionTimeout: sessionTimeout,
		ErrorEvents:    make(chan error),
		alives:         CreateTreeMap(godsutils.UInt64Comparator),
		stopChan:       make(chan interface{}),
		DiscoEvents:    make(chan DiscoveryEvent, 100),
	}

	if err := disco.CreateSessionNode(alivePath); err != nil {
		return nil, tracerr.Wrap(err)
	}

	go disco.loop()

	return disco, nil
}

func (disco *ZkDiscovery) loop() {
	var cancel context.CancelFunc
	var ctx context.Context
LOOP:
	for {
		isLeader, curLeader, changeLeaderChan, err := disco.checkLeader(disco.localNodePath)
		if err != nil {
			disco.ErrorEvents <- tracerr.Wrap(err)
			return
		} else if cancel != nil {
			cancel()
		}
		disco.leaderNode.Store(curLeader)

		ctx, cancel = context.WithCancel(context.Background())

		justJoined := atomic.LoadUint32(&disco.Joined) == 0
		if isLeader {
			go disco.lead(justJoined, ctx)
		} else {
			go disco.follow(justJoined, ctx)
		}
		for {
			select {
			case e := <-changeLeaderChan:
				if e.Err != nil && !isRecoverable(e.Err) {
					disco.ErrorEvents <- tracerr.Wrap(e.Err)
				}
				switch e.Type {
				case zk.EventNodeDeleted:
					continue LOOP
				default:
					var exists bool
					exists, _, changeLeaderChan, err = disco.client.ExistsW(e.Path)
					if err != nil && !isRecoverable(err) {
						disco.ErrorEvents <- tracerr.Wrap(err)
						return
					}
					if !exists {
						continue LOOP
					}
					continue
				}
			case err := <-disco.client.ErrorChan:
				disco.ErrorEvents <- tracerr.Wrap(err)
				return
			case <-disco.stopChan:
				cancel()
				return
			}
		}
	}
}

func (disco *ZkDiscovery) checkLeader(nodePath string) (leader bool, currLeader Node, ev <-chan zk.Event, err error) {
	localNode, err := NewNode(nodePath)
	for {
		var children []string
		children, _, err = disco.client.Children(alivePath)
		if err != nil {
			err = tracerr.Wrap(err)
			return
		}

		leader = true
		currLeader = localNode

		if localNode.NodeOrder == 0 {
			return
		}

		lowestOrder := localNode.NodeOrder
		prevOrder := uint64(0)
		prevOrderPath := ""
		for _, c := range children {
			var node Node
			node, err = NewNode(c)
			if err != nil {
				err = tracerr.Wrap(err)
				return
			}
			if node.NodeOrder < lowestOrder {
				lowestOrder = node.NodeOrder
				currLeader = node
			}
			if node.NodeOrder < localNode.NodeOrder && node.NodeOrder >= prevOrder {
				prevOrder = node.NodeOrder
				prevOrderPath = c
			}
		}

		if localNode.NodeOrder == lowestOrder {
			return
		} else {
			leader = false
		}

		var exists bool
		exists, _, ev, err = disco.client.ExistsW(zk_client.Join(alivePath, prevOrderPath))
		if err != nil {
			err = tracerr.Wrap(err)
			return
		}
		if !exists {
			continue
		}
		return
	}
}

func (disco *ZkDiscovery) lead(newCluster bool, ctx context.Context) {
	events := NewEvents()
	if !newCluster {
		if _, err := disco.loadEvents(events, false); err != nil {
			disco.ErrorEvents <- tracerr.Wrap(err)
			return
		}
	}
	atomic.CompareAndSwapUint32(&disco.Joined, 0, 1)
	procEvtChan := make(chan LastProcessedEvent, 100)

	log.Printf("local nodeOrder %v is leader", disco.localNode)

LOOP:
	for {
		var children []string
		var evt <-chan zk.Event
		var err error

		if children, _, evt, err = disco.client.ChildrenW(alivePath); err != nil {
			disco.ErrorEvents <- tracerr.Wrap(err)
			return
		}

		curr := make(map[uint64]bool, len(children))
		joined := make([]Node, 0, len(children))
		for _, c := range children {
			var node Node
			node, err := NewNode(c)
			if err != nil {
				disco.ErrorEvents <- tracerr.Wrap(err)
				return
			}
			curr[node.NodeOrder] = true

			if _, found := disco.alives.Get(node.NodeOrder); !found {
				events.AddEvent(DiscoveryEvent{Node: node, Type: NodeJoined}, disco.alives)

				if node != *disco.localNode {
					go disco.trackProcessedEvents(c, procEvtChan, ctx)
				}

				joined = append(joined, node)
			}
		}

		for _, v := range disco.alives.Values() {
			node := v.(Node)

			if _, alive := curr[node.NodeOrder]; !alive {
				events.AddEvent(DiscoveryEvent{Node: node, Type: NodeFailed}, disco.alives)
			}
		}

		if err = disco.saveEvents(events); err != nil {
			disco.ErrorEvents <- tracerr.Wrap(err)
			return
		}

		if err = disco.createClusterDataForJoined(joined); err != nil {
			disco.ErrorEvents <- tracerr.Wrap(err)
			return
		}

		disco.processEvents(events, true)

		for {
			select {
			case e := <-evt:
				if e.Err != nil && isRecoverable(e.Err) {
					disco.ErrorEvents <- tracerr.Wrap(e.Err)
					return
				}
				continue LOOP
			case <-ctx.Done():
				close(procEvtChan)
				return
			case procEvt := <-procEvtChan:
				events.OnAckReceived(procEvt.nodeOrder, procEvt.evtId)
				continue
			}
		}
	}
}

func (disco *ZkDiscovery) trackProcessedEvents(nodePath string, procEvtChan chan<- LastProcessedEvent, ctx context.Context) {
	node, err := NewNode(nodePath)
	if err != nil {
		disco.ErrorEvents <- tracerr.Wrap(err)
		return
	}
	for {
		data, _, evt, err := disco.client.GetW(zk_client.Join(alivePath, nodePath))
		if err != nil && !isRecoverable(err) {
			disco.ErrorEvents <- tracerr.Wrap(err)
			return
		} else if err != nil {
			continue
		}

		nodeData := new(NodeData)

		if err := msgpack.Unmarshal(data, &nodeData); err != nil {
			disco.ErrorEvents <- tracerr.Wrap(err)
			return
		}

		procEvtChan <- LastProcessedEvent{node.NodeOrder, nodeData.LastProcessedEvent}

		select {
		case ev := <-evt:
			if ev.Type == zk.EventNodeDeleted {
				return
			}

			continue
		case <-ctx.Done():
			return
		}
	}
}

func (disco *ZkDiscovery) saveEvents(events *Events) error {
	data, err := msgpack.Marshal(events)
	if err != nil {
		return tracerr.Wrap(err)
	}

	if _, err = disco.client.SetOrCreateIfNotExists(evtPath, data); err != nil {
		return tracerr.Wrap(err)
	}
	return nil
}

func (disco *ZkDiscovery) createClusterDataForJoined(joined []Node) error {
	for _, n := range joined {
		data, err := msgpack.Marshal(NewTopology(disco.alives))
		if err != nil {
			return tracerr.Wrap(err)
		}

		_, err = disco.client.SetOrCreateIfNotExists(dataForJoinedPath+n.NodeId.String(), data)
		if err != nil {
			return tracerr.Wrap(err)
		}
	}
	return nil
}

func (disco *ZkDiscovery) follow(justJoined bool, ctx context.Context) {
	events := NewEvents()
	loadClusterData := justJoined

	atomic.CompareAndSwapUint32(&disco.Joined, 0, 1)

	curLeader := disco.leaderNode.Load().(Node)
	log.Printf("local nodeOrder %v is follower, leader is %v\n", disco.localNode, curLeader)

	for {
		if loadClusterData {
			if err := disco.loadClusterData(ctx); err != nil {
				disco.ErrorEvents <- tracerr.Wrap(err)
				return
			}
			loadClusterData = false
		}

		evt, err := disco.loadEvents(events, true)
		if err != nil {
			disco.ErrorEvents <- tracerr.Wrap(err)
			return
		}

		disco.processEvents(events, false)

		select {
		case e := <-evt:
			if e.Err != nil && !isRecoverable(e.Err) {
				disco.ErrorEvents <- tracerr.Wrap(e.Err)
				return
			}
			continue
		case <-ctx.Done():
			return
		}
	}
}

func (disco *ZkDiscovery) loadEvents(events *Events, watch bool) (<-chan zk.Event, error) {
	var data []byte
	var evt <-chan zk.Event
	var err error
	if watch {
		data, _, evt, err = disco.client.GetW(evtPath)
	} else {
		data, _, err = disco.client.Get(evtPath)
	}

	if err != nil {
		return nil, tracerr.Wrap(err)
	}

	if err = msgpack.Unmarshal(data, events); err != nil {
		return nil, tracerr.Wrap(err)
	}

	return evt, err
}

func (disco *ZkDiscovery) processEvents(events *Events, leader bool) {
	iter := events.events.Iterator()
	curLeader := disco.leaderNode.Load().(Node)
	newLeader := false
	for iter.Next() {
		key := iter.Key().(uint64)

		if key <= disco.evtsProcessed {
			continue
		}

		disco.evtsProcessed = key

		val := iter.Value().(DiscoveryEvent)

		switch val.Type {
		case NodeJoined:
			disco.alives.Put(val.Node.NodeOrder, val.Node)
		case NodeFailed:
			disco.alives.Remove(val.Node.NodeOrder)
			if curLeader.NodeId == val.Node.NodeId {
				newLeader = true
				_, min := disco.alives.Min()
				disco.leaderNode.Store(min)
			}

			if leader {
				events.OnNodeLeave(&val.Node)
			}
		}

		if events.topVer <= val.TopVer {
			disco.DiscoEvents <- val
		}

		if !leader && newLeader {
			log.Printf("local nodeOrder %v is follower, leader is %v\n", disco.localNode, curLeader)
		}
	}

	if !leader {
		disco.storeLastProcessedEventId()
	} else {
		events.OnAckReceived(disco.localNode.NodeOrder, disco.evtsProcessed)
	}
}

func (disco *ZkDiscovery) storeLastProcessedEventId() {
	data, err := msgpack.Marshal(&NodeData{LastProcessedEvent: disco.evtsProcessed})
	if err != nil {
		disco.ErrorEvents <- tracerr.Wrap(err)
		return
	}

	path := zk_client.Join(alivePath, disco.localNodePath)
	if _, err = disco.client.Set(path, data, -1); err != nil {
		disco.ErrorEvents <- tracerr.Wrap(err)
	}
	return
}

func (disco *ZkDiscovery) loadClusterData(ctx context.Context) error {
	jdPath := dataForJoinedPath + disco.localNode.NodeId.String()

	defer func() {
		go func() {
			_ = disco.client.Delete(jdPath, -1)
		}()
	}()
	to := time.NewTimer(10 * time.Second)

	for {
		exists, _, evt, err := disco.client.ExistsW(jdPath)
		if err != nil {
			return tracerr.Wrap(err)
		}

		if exists {
			data, _, err := disco.client.Get(jdPath)
			if err != nil {
				return tracerr.Wrap(err)
			}
			var topology Topology
			if err = msgpack.Unmarshal(data, &topology); err != nil {
				return tracerr.Wrap(err)
			}
			for _, v := range topology {
				disco.alives.Put(v.NodeOrder, v)
			}

			return nil
		}

		select {
		case e := <-evt:
			if e.Err != nil && !isRecoverable(e.Err) {
				return tracerr.Wrap(e.Err)
			}
			continue
		case <-to.C:
			return tracerr.New("failed to join within timeout")
		case <-ctx.Done():
			return nil
		}
	}
}

func (disco *ZkDiscovery) CreateSessionNode(basePath string) error {
	var b strings.Builder
	b.WriteString(basePath)
	b.WriteString(separator)
	b.WriteString(uuid.New().String())
	b.WriteRune('_')

	prefix := b.String()

	data, _ := msgpack.Marshal(&NodeData{})

	for i := 0; i < nodeJoinRetryCount; i++ {
		newPath, err := disco.client.Create(prefix, data, zk.FlagEphemeral|zk.FlagSequence, zk.WorldACL(zk.PermAll))
		err = tracerr.Unwrap(err)
		switch err {
		case zk.ErrSessionExpired:
			continue
		case zk.ErrConnectionClosed:
			children, _, err := disco.client.Children(basePath)
			if err != nil {
				return tracerr.Wrap(err)
			}
			for _, p := range children {
				if strings.HasPrefix(p, prefix) {
					locNode, err := NewNode(p)
					if err != nil {
						return tracerr.Wrap(err)
					}

					disco.localNode = &locNode
					disco.localNodePath = p

					return nil
				}
			}
			continue
		case nil:
			parts := strings.Split(newPath, "/")
			locNodePath := parts[len(parts)-1]

			locNode, err := NewNode(locNodePath)
			if err != nil {
				return tracerr.Wrap(err)
			}

			disco.localNode = &locNode
			disco.localNodePath = locNodePath

			return nil
		}
	}
	return tracerr.Errorf("failed to create session nodeOrder")
}

func isRecoverable(err error) bool {
	err = tracerr.Unwrap(err)
	return err == zk.ErrSessionMoved || err == zk.ErrConnectionClosed
}

func (disco *ZkDiscovery) Close() {
	close(disco.stopChan)
	disco.client.Close()
}

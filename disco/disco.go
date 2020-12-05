package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/emirpasic/gods/maps/treemap"
	godsutils "github.com/emirpasic/gods/utils"
	"github.com/go-zookeeper/zk"
	"github.com/google/uuid"
	"github.com/vmihailenco/msgpack/v5"
	"log"
	"strings"
	"sync/atomic"
	"time"
	"zk-intro/zk_client"
)

const (
	zkRoot            = "/myService"
	alivePath         = zkRoot + "/n"
	evtPath           = zkRoot + "/e"
	dataForJoinedPath = evtPath + "/fj-"
	separator         = "/"
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

func Create(servers []string, sessionTimeout time.Duration) (*ZkDiscovery, error) {
	client, err := zk_client.NewZkClient(servers, sessionTimeout)
	if err != nil {
		return nil, err
	}

	if err := client.MkDir(alivePath, true); err != nil {
		return nil, err
	}

	if err = client.MkDir(evtPath, true); err != nil {
		return nil, err
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
		return nil, err
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
			panic(err)
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
					panic(err)
				}
				switch e.Type {
				case zk.EventNodeDeleted:
					continue LOOP
				default:
					var exists bool
					exists, _, changeLeaderChan, err = disco.client.ExistsW(e.Path)
					if err != nil && !isRecoverable(err) {
						panic(err)
					}
					if !exists {
						continue LOOP
					}
					continue
				}
			case err := <-disco.client.ErrorChan:
				disco.ErrorEvents <- err
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
			return
		}

		nodes := treemap.NewWith(godsutils.UInt64Comparator)
		for _, c := range children {
			var node Node
			node, err = NewNode(c)
			if err != nil {
				return
			}
			if node.NodeOrder != localNode.NodeOrder {
				nodes.Put(node.NodeOrder, c)
			}
		}

		if _, v := nodes.Floor(localNode.NodeOrder); v != nil {
			var exists bool
			exists, _, ev, err = disco.client.ExistsW(zk_client.Join(alivePath, v.(string)))
			if err != nil {
				return
			}
			if !exists {
				continue
			}
			leader = false
			if _, v := nodes.Min(); v != nil {
				currLeader, err = NewNode(v.(string))
				if err != nil {
					return
				}
			}
		} else {
			leader = true
			currLeader = localNode
		}
		return
	}
}

func (disco *ZkDiscovery) lead(newCluster bool, ctx context.Context) {
	events := NewEvents()

	if !newCluster {
		if _, err := disco.loadEvents(events, false); err != nil {
			disco.ErrorEvents <- err
			return
		}
	} else {
		disco.alives.Put(disco.localNode.NodeOrder, *disco.localNode)
		events.AddEvent(DiscoveryEvent{Node: *disco.localNode, Type: NodeJoined})
	}
	atomic.CompareAndSwapUint32(&disco.Joined, 0, 1)

	log.Printf("local node %v is leader", disco.localNode)

	for {
		var children []string
		var evt <-chan zk.Event
		var err error

		if children, _, evt, err = disco.client.ChildrenW(alivePath); err != nil {
			disco.ErrorEvents <- err
			return
		}

		curr := make(map[uint64]bool, len(children))
		joined := make([]Node, 0, len(children))
		for _, c := range children {
			var node Node
			node, err := NewNode(c)
			if err != nil {
				disco.ErrorEvents <- err
				return
			}
			curr[node.NodeOrder] = true

			if _, found := disco.alives.Get(node.NodeOrder); !found {
				disco.alives.Put(node.NodeOrder, node)
				events.AddEvent(DiscoveryEvent{Node: node, Type: NodeJoined})
				joined = append(joined, node)
			}
		}

		for _, v := range disco.alives.Values() {
			node := v.(Node)

			if _, alive := curr[node.NodeOrder]; !alive {
				events.AddEvent(DiscoveryEvent{Node: node, Type: NodeFailed})
				disco.alives.Remove(node.NodeOrder)
			}
		}

		if err = disco.saveEvents(events); err != nil {
			disco.ErrorEvents <- err
			return
		}

		if err = disco.createClusterDataForJoined(joined); err != nil {
			disco.ErrorEvents <- err
			return
		}

		disco.processEvents(events, true)

		select {
		case e := <-evt:
			if e.Err != nil && isRecoverable(e.Err) {
				disco.ErrorEvents <- e.Err
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
		return err
	}

	if _, err = disco.client.SetOrCreateIfNotExists(evtPath, data); err != nil {
		return err
	}
	return nil
}

func (disco *ZkDiscovery) createClusterDataForJoined(joined []Node) error {
	for _, n := range joined {
		data, err := msgpack.Marshal(NewTopology(disco.alives))
		if err != nil {
			return err
		}

		_, err = disco.client.SetOrCreateIfNotExists(dataForJoinedPath+n.NodeId.String(), data)
		if err != nil {
			return err
		}
	}
	return nil
}

func (disco *ZkDiscovery) follow(justJoined bool, ctx context.Context) {
	events := NewEvents()
	loadClusterData := justJoined

	atomic.CompareAndSwapUint32(&disco.Joined, 0, 1)

	curLeader := disco.leaderNode.Load().(Node)
	log.Printf("local node %v is follower, leader is %v\n", disco.localNode, curLeader)

	for {
		if loadClusterData {
			if err := disco.loadClusterData(ctx); err != nil {
				disco.ErrorEvents <- err
				return
			}
			loadClusterData = false
		}

		evt, err := disco.loadEvents(events, true)
		if err != nil {
			disco.ErrorEvents <- err
			return
		}

		disco.processEvents(events, false)

		select {
		case e := <-evt:
			if e.Err != nil && !isRecoverable(e.Err) {
				disco.ErrorEvents <- e.Err
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
		return nil, err
	}

	if err = msgpack.Unmarshal(data, events); err != nil {
		return nil, err
	}

	return evt, err
}

func (disco *ZkDiscovery) processEvents(events *Events, leader bool) {
	iter := events.events.Iterator()
	curLeader := disco.leaderNode.Load().(Node)
	newLeader := false
	for iter.Next() {
		key := iter.Key().(uint64)

		if key <= atomic.LoadUint64(&disco.evtsProcessed) {
			continue
		}

		atomic.StoreUint64(&disco.evtsProcessed, key)

		val := iter.Value().(DiscoveryEvent)

		if !leader {
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
			}
		}

		if leader || events.topVer <= val.TopVer {
			disco.DiscoEvents <- val
		}

		if !leader && newLeader {
			log.Printf("local node %v is follower, leader is %v\n", disco.localNode, curLeader)
		}
	}
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
			return err
		}

		if exists {
			data, _, err := disco.client.Get(jdPath)
			if err != nil {
				return err
			}
			var topology Topology
			if err = msgpack.Unmarshal(data, &topology); err != nil {
				return err
			}
			for _, v := range topology {
				disco.alives.Put(v.NodeOrder, v)
			}

			return nil
		}

		select {
		case e := <-evt:
			if e.Err != nil && !isRecoverable(e.Err) {
				panic(e.Err)
			}
			continue
		case <-to.C:
			return errors.New("failed to join within timeout")
		case <-ctx.Done():
			return nil
		}
	}
}

func (disco *ZkDiscovery) CreateSessionNode(basePath string) (err error) {
	var b strings.Builder
	b.WriteString(basePath)
	b.WriteString(separator)
	b.WriteString(uuid.New().String())
	b.WriteRune('_')

	prefix := b.String()

	for i := 0; i < 10; i++ {
		newPath, err := disco.client.Create(prefix, nil, zk.FlagEphemeral|zk.FlagSequence, zk.WorldACL(zk.PermAll))
		switch err {
		case zk.ErrSessionExpired:
			continue
		case zk.ErrConnectionClosed:
			children, _, err := disco.client.Children(basePath)
			if err != nil {
				return err
			}
			for _, p := range children {
				if strings.HasPrefix(p, prefix) {
					locNode, err := NewNode(p)
					if err != nil {
						return err
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
				return err
			}

			disco.localNode = &locNode
			disco.localNodePath = locNodePath

			return nil
		}
	}
	return fmt.Errorf("failed to create session node")
}

func isRecoverable(err error) bool {
	return err == zk.ErrSessionMoved || err == zk.ErrConnectionClosed
}

func (disco *ZkDiscovery) Close() {
	close(disco.stopChan)

	disco.client.Close()
}

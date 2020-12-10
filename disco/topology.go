package main

import (
	"github.com/google/uuid"
	"github.com/vmihailenco/msgpack/v5"
	"github.com/ztrue/tracerr"
	"sort"
	"strconv"
	"strings"
)

type Topology []Node

type Node struct {
	NodeId    uuid.UUID
	NodeOrder uint64
}

type NodeData struct {
	LastProcessedEvent uint64
}

func (n Node) String() string {
	var b strings.Builder
	b.WriteString("Node [id=")
	b.WriteString(n.NodeId.String())
	b.WriteString(", order=")
	b.WriteString(strconv.FormatUint(n.NodeOrder, 10))
	b.WriteString("]")
	return b.String()
}

func NewNode(node string) (Node, error) {
	parts := strings.Split(node, "_")
	if len(parts) == 2 {
		ord, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return Node{}, tracerr.Wrap(err)
		}
		id, err := uuid.Parse(parts[0])
		if err != nil {
			return Node{}, tracerr.Wrap(err)
		}

		return Node{NodeId: id, NodeOrder: ord}, nil
	} else {
		return Node{}, tracerr.Errorf("unexpected input %s", node)
	}
}

func NewTopology(alives *SyncTreeMap) *Topology {
	var top Topology = make([]Node, alives.Size())
	for i, v := range alives.Values() {
		top[i] = v.(Node)
	}
	return &top
}

func (top *Topology) Len() int {
	return len(*top)
}

func (top *Topology) EncodeMsgpack(enc *msgpack.Encoder) (err error) {
	err = enc.EncodeArrayLen(top.Len())
	if err != nil {
		return tracerr.Wrap(err)
	}

	sort.Slice(*top, func(i, j int) bool {
		return (*top)[i].NodeOrder < (*top)[j].NodeOrder
	})

	for _, n := range *top {
		err = enc.EncodeUint64(n.NodeOrder)
		if err != nil {
			return tracerr.Wrap(err)
		}
		err = enc.Encode(n.NodeId)
		if err != nil {
			return tracerr.Wrap(err)
		}
	}
	return
}

func (top *Topology) DecodeMsgpack(dec *msgpack.Decoder) error {
	sz, err := dec.DecodeArrayLen()
	if err != nil {
		return tracerr.Wrap(err)
	}

	*top = make([]Node, sz)

	for i := 0; i < sz; i++ {
		ord, err := dec.DecodeUint64()
		if err != nil {
			return tracerr.Wrap(err)
		}
		var id uuid.UUID
		err = dec.Decode(&id)
		if err != nil {
			return tracerr.Wrap(err)
		}
		(*top)[i] = Node{NodeId: id, NodeOrder: ord}
	}

	return nil
}

func (nd *NodeData) EncodeMsgpack(enc *msgpack.Encoder) error {
	if err := enc.EncodeUint64(nd.LastProcessedEvent); err != nil {
		return tracerr.Wrap(err)
	}
	return nil
}

func (nd *NodeData) DecodeMsgpack(dec *msgpack.Decoder) error {
	lastProcEvt, err := dec.DecodeUint64()
	if err != nil {
		return tracerr.Wrap(err)
	}
	nd.LastProcessedEvent = lastProcEvt
	return nil
}

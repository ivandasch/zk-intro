package main

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/vmihailenco/msgpack/v5"
	"sort"
	"strconv"
	"strings"
)

type Topology []Node

type Node struct {
	NodeId    uuid.UUID
	NodeOrder uint64
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
			return Node{}, err
		}
		id, err := uuid.Parse(parts[0])
		if err != nil {
			return Node{}, err
		}

		return Node{NodeId: id, NodeOrder: ord}, nil
	} else {
		return Node{}, fmt.Errorf("unexpected input %s", node)
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
		return
	}

	sort.Slice(*top, func(i, j int) bool {
		return (*top)[i].NodeOrder < (*top)[j].NodeOrder
	})

	for _, n := range *top {
		err = enc.EncodeUint64(n.NodeOrder)
		if err != nil {
			return
		}
		err = enc.Encode(n.NodeId)
		if err != nil {
			return
		}
	}
	return
}

func (top *Topology) DecodeMsgpack(dec *msgpack.Decoder) error {
	sz, err := dec.DecodeArrayLen()
	if err != nil {
		return err
	}

	*top = make([]Node, sz)

	for i := 0; i < sz; i++ {
		ord, err := dec.DecodeUint64()
		if err != nil {
			return err
		}
		var id uuid.UUID
		err = dec.Decode(&id)
		if err != nil {
			return err
		}
		(*top)[i] = Node{NodeId: id, NodeOrder: ord}
	}

	return nil
}

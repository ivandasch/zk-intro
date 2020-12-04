package main

import (
	"github.com/emirpasic/gods/maps/treemap"
	godsutils "github.com/emirpasic/gods/utils"
	"github.com/vmihailenco/msgpack/v5"
	"strconv"
	"strings"
)

const (
	NodeJoined = iota
	NodeFailed
)

type EventType int8

type DiscoveryEvent struct {
	Node   Node
	Type   EventType
	TopVer uint64
	acks   map[uint64]bool
}

type Events struct {
	topVer uint64
	evtsId uint64
	events *treemap.Map
}

func (e DiscoveryEvent) String() string {
	var b strings.Builder
	b.WriteString("Event [id=")
	b.WriteString(e.Node.NodeId.String())
	b.WriteString(", node=")
	b.WriteString(strconv.FormatUint(e.Node.NodeOrder, 10))
	b.WriteString(", type=")
	switch e.Type {
	case NodeJoined:
		b.WriteString("NODE_JOINED")
	case NodeFailed:
		b.WriteString("NODE_FAILED")
	}
	b.WriteString(", topVer=")
	b.WriteString(strconv.FormatUint(e.TopVer, 10))
	b.WriteRune(']')
	return b.String()
}

func NewEvents() *Events {
	ev := &Events{}
	ev.events = treemap.NewWith(godsutils.UInt64Comparator)
	return ev
}

func (e *Events) AddEvent(evt DiscoveryEvent) {
	e.evtsId++
	e.topVer++
	evt.acks = make(map[uint64]bool)
	evt.TopVer = e.topVer
	e.events.Put(e.evtsId, evt)
}

func (e *Events) EncodeMsgpack(enc *msgpack.Encoder) (err error) {
	err = enc.EncodeUint64(e.topVer)
	if err != nil {
		return
	}
	err = enc.EncodeUint64(e.evtsId)
	if err != nil {
		return
	}

	err = enc.EncodeMapLen(e.events.Size())
	if err != nil {
		return
	}
	if !e.events.Empty() {
		iter := e.events.Iterator()
		for iter.Next() {
			err = enc.Encode(iter.Key())
			if err != nil {
				return
			}
			err = enc.Encode(iter.Value())
			if err != nil {
				return
			}
		}
	}
	return
}

func (e *Events) DecodeMsgpack(dec *msgpack.Decoder) (err error) {
	e.topVer, err = dec.DecodeUint64()
	if err != nil {
		return
	}
	e.evtsId, err = dec.DecodeUint64()
	if err != nil {
		return
	}

	e.events = treemap.NewWith(godsutils.UInt64Comparator)

	evtsSz, err := dec.DecodeMapLen()
	if err != nil {
		return
	}

	for i := 0; i < evtsSz; i++ {
		var key uint64
		key, err = dec.DecodeUint64()
		if err != nil {
			return
		}
		var val DiscoveryEvent
		err = dec.Decode(&val)
		e.events.Put(key, val)
	}
	return
}

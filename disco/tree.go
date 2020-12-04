package main

import (
	"github.com/emirpasic/gods/maps/treemap"
	"github.com/emirpasic/gods/utils"
	"sync"
)

type SyncTreeMap struct {
	tree  *treemap.Map
	wlock sync.RWMutex
	rlock sync.Locker
}

func CreateTreeMap(comparator utils.Comparator) *SyncTreeMap {
	m := &SyncTreeMap{tree: treemap.NewWith(comparator)}
	m.rlock = m.wlock.RLocker()
	return m
}

func (m *SyncTreeMap) Put(key interface{}, value interface{}) {
	m.wlock.Lock()
	m.tree.Put(key, value)
	m.wlock.Unlock()
}

func (m *SyncTreeMap) Get(key interface{}) (value interface{}, found bool) {
	m.rlock.Lock()
	value, found = m.tree.Get(key)
	m.rlock.Unlock()
	return
}

func (m *SyncTreeMap) Remove(key interface{}) {
	m.wlock.Lock()
	m.tree.Remove(key)
	m.wlock.Unlock()
}

func (m *SyncTreeMap) Empty() (out bool) {
	m.rlock.Lock()
	out = m.tree.Empty()
	m.rlock.Unlock()
	return
}

func (m *SyncTreeMap) Size() (size int) {
	m.rlock.Lock()
	size = m.tree.Size()
	m.rlock.Unlock()
	return
}

func (m *SyncTreeMap) Keys() (keys []interface{}) {
	m.rlock.Lock()
	keys = m.tree.Keys()
	m.rlock.Unlock()
	return
}

func (m *SyncTreeMap) Values() (out []interface{}) {
	m.rlock.Lock()
	out = m.tree.Values()
	m.rlock.Unlock()
	return
}

func (m *SyncTreeMap) Clear() {
	m.wlock.Lock()
	m.tree.Clear()
	m.wlock.Unlock()
}

func (m *SyncTreeMap) Min() (key interface{}, value interface{}) {
	m.rlock.Lock()
	key, value = m.tree.Min()
	m.rlock.Unlock()
	return
}

func (m *SyncTreeMap) Max() (key interface{}, value interface{}) {
	m.rlock.Lock()
	key, value = m.tree.Max()
	m.rlock.Unlock()
	return
}

func (m *SyncTreeMap) Floor(key interface{}) (foundKey interface{}, foundValue interface{}) {
	m.rlock.Lock()
	foundKey, foundValue = m.tree.Floor(key)
	m.rlock.Unlock()
	return
}

func (m *SyncTreeMap) Ceiling(key interface{}) (foundKey interface{}, foundValue interface{}) {
	m.rlock.Lock()
	foundKey, foundValue = m.tree.Ceiling(key)
	m.rlock.Unlock()
	return
}

func (m *SyncTreeMap) String() (out string) {
	m.rlock.Lock()
	out = m.tree.String()
	m.rlock.Unlock()
	return
}

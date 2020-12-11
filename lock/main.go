package main

import (
	"fmt"
	zk "github.com/go-zookeeper/zk"
	"math/rand"
	"os"
	"strconv"
	"time"
	"zk-intro/zk_client"
)

const (
	appDir = "/simple"
)

func Watcher(cli *zk_client.ZkClient, path string) {
	val := 0
	cnt := 0
	for {
		data, _, evt, err := cli.GetW(path)
		if err == zk.ErrNoNode {
			continue
		} else if err != nil {
			panic(err)
		}
		newVal, _ := strconv.Atoi(string(data))
		if newVal <= val {
			fmt.Printf("violation oldVal=%d newVal=%d, oldVal >= newVal\n", val, newVal)
		}
		val = newVal
		cnt++
		if cnt%100 == 0 {
			fmt.Printf("number of iterations %d\n", cnt)
		}
		select {
		case e := <-evt:
			if e.Type != zk.EventNodeDataChanged {
				fmt.Printf("Unexpected type of event %s\n", e.Type)
			}
			continue
		}
	}
}

// Safe lock wrapper.
func DoInLock(lock *Lock, closure func() error) error {
	err := lock.Lock()
	if err != nil {
		return err
	}
	defer func() {
		_ = lock.Unlock()
	}()
	return closure()
}

func LockWriter(cli *zk_client.ZkClient, path string) {
	lock := NewLock(cli, zk_client.Join(appDir, "lock"), zk.WorldACL(zk.PermAll))

	rand.Seed(time.Now().UTC().UnixNano())

	for {
		err := DoInLock(lock, func() error {
			data, _, err := cli.Get(path)
			if err != nil && err != zk.ErrNoNode {
				panic(err)
			}
			if err == zk.ErrNoNode {
				_, err = cli.Create(path, []byte(strconv.Itoa(100)), 0, zk.WorldACL(zk.PermAll))
			} else {
				val, _ := strconv.Atoi(string(data))

				_, err = cli.Set(path, []byte(strconv.Itoa(val+1)), -1)
			}
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			panic(err)
		}
		time.Sleep(time.Duration(rand.Int63n(10) * int64(time.Millisecond)))
		continue
	}
}

func Writer(cli *zk_client.ZkClient, path string) {
	rand.Seed(time.Now().UTC().UnixNano())

	for {
		data, _, err := cli.Get(path)
		if err != nil && err != zk.ErrNoNode {
			panic(err)
		}
		if err == zk.ErrNoNode {
			_, err = cli.Create(path, []byte(strconv.Itoa(100)), 0, zk.WorldACL(zk.PermAll))
		} else {
			val, _ := strconv.Atoi(string(data))

			_, err = cli.Set(path, []byte(strconv.Itoa(val+1)), -1)
		}
		if err != nil {
			panic(err)
		}
		time.Sleep(time.Duration(rand.Int63n(10) * int64(time.Millisecond)))
		continue
	}
}

func main() {
	sessionTimeout := 2 * time.Second
	cli, err := zk_client.NewZkClient([]string{"127.0.0.1:2181", "127.0.0.1:2182", "127.0.0.1:2183"}, sessionTimeout)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = cli.Delete(appDir, -1)
		cli.Close()
	}()
	if err := cli.MkDir(appDir, false); err != nil {
		panic(err)
	}
	path := zk_client.Join(appDir, "counter")
	args := os.Args
	if len(args) == 1 || args[1] == "watcher" {
		Watcher(cli, path)
	} else if args[1] == "writer" {
		rand.Seed(time.Now().UTC().UnixNano())
		lock := len(args) > 2 && args[2] == "lock"
		if lock {
			LockWriter(cli, path)
		} else {
			Writer(cli, path)
		}
	}
}

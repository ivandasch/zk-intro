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

func DoInLock(lock *Lock, closure func() error, fake bool) error {
	if !fake {
		err := lock.Lock()
		if err != nil {
			return err
		}
		defer lock.Unlock()
		return closure()
	}
	return closure()
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
			if cnt%1000 == 0 {
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
	} else if args[1] == "writer" {
		rand.Seed(time.Now().UTC().UnixNano())
		fakeLock := len(args) > 2 && args[2] == "fake"
		lock := NewLock(cli, zk_client.Join(appDir, "lock"), zk.WorldACL(zk.PermAll))
		for {
			err := DoInLock(lock, func() error {
				data, _, err := cli.Get(path)
				if err != nil && err != zk.ErrNoNode {
					return err
				}
				if err == zk.ErrNoNode {
					_, err = cli.Create(path, []byte(strconv.Itoa(100)), 0, zk.WorldACL(zk.PermAll))
				} else {
					val, _ := strconv.Atoi(string(data))

					_, err = cli.Set(path, []byte(strconv.Itoa(val+1)), -1)
				}
				return err
			}, fakeLock)
			if err != nil {
				panic(err)
			}
			time.Sleep(time.Duration(rand.Int63n(10) * int64(time.Millisecond)))
			continue
		}
	}
}

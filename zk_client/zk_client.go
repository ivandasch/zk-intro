package zk_client

import (
	"fmt"
	"github.com/go-zookeeper/zk"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	numRetries = 10
	separator  = "/"
)

type ZkClient struct {
	conn           *zk.Conn
	ErrorChan      chan error
	shouldQuitOnce sync.Once
	closeChan      chan interface{}
	sessionTimeout time.Duration
	retryTimeout   time.Duration
}

func Join(paths ...string) string {
	if paths != nil {
		for i, p := range paths {
			paths[i] = strings.TrimRight(p, "/")
		}
		return strings.Join(paths, separator)
	}
	panic("expected not empty paths")
}

func NewZkClient(servers []string, sessionTimeout time.Duration) (client *ZkClient, err error) {
	closeChan := make(chan interface{})
	errorChan := make(chan error)
	c, evt, err := zk.Connect(servers, sessionTimeout)

	client = &ZkClient{
		conn:           c,
		ErrorChan:      errorChan,
		closeChan:      closeChan,
		sessionTimeout: sessionTimeout,
		retryTimeout:   sessionTimeout / numRetries,
	}
	if err != nil {
		return
	}

	client.connect(sessionTimeout, evt)

	if c.State() != zk.StateHasSession {
		err = fmt.Errorf("failed to connect within timeout %s", sessionTimeout.String())
		return
	}

	go client.loop(sessionTimeout, evt)

	return
}

func (client *ZkClient) connect(timeout time.Duration, evt <-chan zk.Event) {
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if client.conn.SessionID() != 0 {
			return
		}

		to := time.NewTimer(timeout)
		defer to.Stop()
		for {
			select {
			case e := <-evt:
				if (e.Type == zk.EventSession && e.State == zk.StateHasSession) || client.conn.SessionID() != 0 {
					return
				}
			case <-to.C:
				return
			}
		}
	}()
	wg.Wait()
}

func (client *ZkClient) loop(timeout time.Duration, evt <-chan zk.Event) {
	session := client.SessionID()
	sessionRestoring := false
	var onSessionRestoreTimeout <-chan time.Time = nil
MAIN:
	for {
		if sessionRestoring && onSessionRestoreTimeout == nil {
			onSessionRestoreTimeout = time.After(timeout)
		}

		select {
		case e := <-evt:
			if e.Type == zk.EventSession {
				if !sessionRestoring && client.State() != zk.StateHasSession {
					sessionRestoring = true
				} else if sessionRestoring && client.State() == zk.StateHasSession {
					if client.SessionID() == session {
						sessionRestoring = false
						onSessionRestoreTimeout = nil

						log.Printf("restored session %d successfully", session)
					}
				}
			}
			continue MAIN
		case <-onSessionRestoreTimeout:
			client.ErrorChan <- fmt.Errorf("failed to restore session whithin timeot %s", timeout.String())
		case <-client.closeChan:
			return
		}
	}
}

func (client *ZkClient) Close() {
	client.shouldQuitOnce.Do(func() {
		close(client.closeChan)
		client.conn.Close()
	})
}

func (client *ZkClient) SessionID() int64 {
	return client.conn.SessionID()
}

func (client *ZkClient) State() zk.State {
	return client.conn.State()
}

func (client *ZkClient) SetOrCreateIfNotExists(path string, data []byte) (out string, err error) {
	if ok, _, err := client.Exists(path); err == nil {
		if !ok {
			if _, err = client.Create(path, data, 0, zk.WorldACL(zk.PermAll)); err == nil {
				out = path
			}
		} else {
			if _, err = client.Set(path, data, -1); err == nil {
				out = path
			}
		}
	}
	return
}

func (client *ZkClient) MkDir(path string, parents bool) (err error) {
	exists := false
	if exists, _, err = client.Exists(path); err != nil || exists {
		return
	}

	if parents {
		buf := ""
		for _, p := range strings.Split(path, separator) {
			if len(p) == 0 {
				continue
			}

			buf = Join(buf, p)
			if _, err = client.SetOrCreateIfNotExists(buf, nil); err != nil {
				return
			}
		}
	} else {
		_, err = client.SetOrCreateIfNotExists(path, nil)
	}
	return
}

func (client *ZkClient) Exists(path string) (ok bool, stat *zk.Stat, err error) {
	client.retry(func() error {
		ok, stat, err = client.conn.Exists(path)
		return err
	})
	return
}

func (client *ZkClient) ExistsW(path string) (ok bool, stat *zk.Stat, evt <-chan zk.Event, err error) {
	client.retry(func() error {
		ok, stat, evt, err = client.conn.ExistsW(path)
		return err
	})
	return
}

func (client *ZkClient) Children(path string) (children []string, stat *zk.Stat, err error) {
	client.retry(func() error {
		children, stat, err = client.conn.Children(path)
		return err
	})
	return
}

func (client *ZkClient) ChildrenW(path string) (children []string, stat *zk.Stat, evt <-chan zk.Event, err error) {
	client.retry(func() error {
		children, stat, evt, err = client.conn.ChildrenW(path)
		return err
	})
	return
}

func (client *ZkClient) Get(path string) (data []byte, stat *zk.Stat, err error) {
	client.retry(func() error {
		data, stat, err = client.conn.Get(path)
		return err
	})
	return
}

func (client *ZkClient) GetW(path string) (data []byte, stat *zk.Stat, evt <-chan zk.Event, err error) {
	client.retry(func() error {
		data, stat, evt, err = client.conn.GetW(path)
		return err
	})
	return
}

func (client *ZkClient) Create(path string, data []byte, flags int32, acl []zk.ACL) (res string, err error) {
	client.retry(func() error {
		res, err = client.conn.Create(path, data, flags, acl)
		return err
	})
	return
}

func (client *ZkClient) Set(path string, data []byte, version int32) (stat *zk.Stat, err error) {
	client.retry(func() error {
		stat, err = client.conn.Set(path, data, version)
		return err
	})
	return
}

func (client *ZkClient) Delete(path string, version int32) (err error) {
	client.retry(func() error {
		err = client.conn.Delete(path, version)
		return err
	})
	return
}

func (client *ZkClient) retry(closure func() error) {
	ticker := time.NewTicker(client.retryTimeout)
	defer ticker.Stop()
	cnt := 0
LOOP:
	for cnt < numRetries {
		err := closure()

		if err == zk.ErrSessionMoved || err == zk.ErrConnectionClosed {
			select {
			case <-ticker.C:
				cnt++
				continue LOOP
			}
		} else {
			return
		}
	}
}

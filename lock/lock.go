package main

import (
	"fmt"
	"github.com/go-zookeeper/zk"
	"github.com/google/uuid"
	"strconv"
	"strings"
	"zk-intro/zk_client"
)

type Lock struct {
	cli   *zk_client.ZkClient
	path  string
	lock  string
	order int32
	acl   []zk.ACL
}

func NewLock(cli *zk_client.ZkClient, path string, acl []zk.ACL) *Lock {
	return &Lock{
		cli:  cli,
		path: path,
		acl:  acl,
	}
}

func (l *Lock) Lock() error {
	if l.lock != "" {
		return fmt.Errorf("trying to acquire lock twice")
	}

	path := ""
	var err error
	for {
		path, err = l.CreateLockNode()
		if err == zk.ErrNoNode {
			if err := l.cli.MkDir(l.path, true); err != nil {
				return err
			}
			continue
		} else if err == nil {
			break
		} else {
			return err
		}
	}

	order, err := parseSeq(path)
	if err != nil {
		return err
	}

	for {
		children, _, err := l.cli.Children(l.path)
		if err != nil {
			return err
		}

		lowestOrder := order
		prevOrder := int32(-1)
		prevOrderPath := ""
		for _, p := range children {
			s, err := parseSeq(p)
			if err != nil {
				return err
			}
			if s < lowestOrder {
				lowestOrder = s
			}
			if s < order && s > prevOrder {
				prevOrder = s
				prevOrderPath = p
			}
		}

		if order == lowestOrder {
			break
		}

		_, _, ch, err := l.cli.GetW(l.path + "/" + prevOrderPath)
		if err != nil && err != zk.ErrNoNode {
			return err
		} else if err != nil && err == zk.ErrNoNode {
			continue
		}

		ev := <-ch
		if ev.Err != nil {
			return ev.Err
		}
	}

	l.order = order
	l.lock = path
	return nil
}

func (l *Lock) CreateLockNode() (string, error) {
	prefix := fmt.Sprintf("%s/%s-lock-", l.path, uuid.New().String())

	for {
		newPath, err := l.cli.Create(prefix, nil, zk.FlagEphemeral|zk.FlagSequence, l.acl)
		switch err {
		case zk.ErrSessionExpired:
			continue
		case zk.ErrConnectionClosed:
			children, _, err := l.cli.Children(l.path)
			if err != nil {
				return "", err
			}
			for _, p := range children {
				if strings.HasPrefix(p, prefix) {
					return zk_client.Join(l.path, p), nil
				}
			}
			continue
		case nil:
			return newPath, nil
		default:
			return "", err
		}
	}
}

func (l *Lock) Unlock() error {
	if l.lock == "" {
		return fmt.Errorf("not locked")
	}
	if err := l.cli.Delete(l.lock, -1); err != nil {
		return err
	}
	l.lock = ""
	l.order = 0
	return nil
}

func parseSeq(path string) (int32, error) {
	parts := strings.Split(path, "-")
	res, err := strconv.ParseInt(parts[len(parts)-1], 10, 32)
	return int32(res), err
}

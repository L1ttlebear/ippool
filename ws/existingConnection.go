package ws

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	connectedClients = make(map[string]*SafeConn)
	ConnectedUsers   = []*websocket.Conn{}
	mu               = sync.RWMutex{}
)

func GetConnectedClients() map[string]*SafeConn {
	mu.RLock()
	defer mu.RUnlock()
	clientsCopy := make(map[string]*SafeConn)
	for k, v := range connectedClients {
		clientsCopy[k] = v
	}
	return clientsCopy
}

func SetConnectedClients(uuid string, conn *SafeConn) {
	mu.Lock()
	defer mu.Unlock()
	connectedClients[uuid] = conn
}

func DeleteClientConditionally(uuid string, connToRemove *SafeConn) {
	mu.Lock()
	defer mu.Unlock()
	if currentConn, exists := connectedClients[uuid]; exists && currentConn == connToRemove {
		delete(connectedClients, uuid)
	}
}

func DeleteConnectedClients(uuid string) {
	mu.Lock()
	defer mu.Unlock()
	delete(connectedClients, uuid)
}

var presenceOnly = make(map[string]struct {
	id     int64
	expire time.Time
})

func KeepAlivePresence(uuid string, connectionID int64, ttl time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	presenceOnly[uuid] = struct {
		id     int64
		expire time.Time
	}{id: connectionID, expire: time.Now().Add(ttl)}
}

var defaultPresenceTTL = 20 * time.Second

func SetPresence(uuid string, connectionID int64, present bool) {
	mu.Lock()
	defer mu.Unlock()
	if present {
		presenceOnly[uuid] = struct {
			id     int64
			expire time.Time
		}{id: connectionID, expire: time.Now().Add(defaultPresenceTTL)}
		return
	}
	if cur, ok := presenceOnly[uuid]; ok && cur.id == connectionID {
		delete(presenceOnly, uuid)
	}
}

func GetAllOnlineUUIDs() []string {
	mu.RLock()
	defer mu.RUnlock()
	set := make(map[string]struct{})
	for k := range connectedClients {
		set[k] = struct{}{}
	}
	now := time.Now()
	for k, v := range presenceOnly {
		if v.expire.After(now) {
			set[k] = struct{}{}
		}
	}
	res := make([]string, 0, len(set))
	for k := range set {
		res = append(res, k)
	}
	return res
}

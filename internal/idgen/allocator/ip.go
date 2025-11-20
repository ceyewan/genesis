package allocator

import (
	"context"
	"fmt"
	"net"
)

// IPAllocator 基于 IP 地址后 8 位的分配器
type IPAllocator struct{}

// NewIP 创建 IP 分配器
func NewIP() *IPAllocator {
	return &IPAllocator{}
}

// Allocate 获取本机 IP 地址的后 8 位作为 WorkerID
func (a *IPAllocator) Allocate(ctx context.Context) (int64, error) {
	// 获取本机非 loopback 的 IPv4 地址
	ip, err := getLocalIP()
	if err != nil {
		return 0, fmt.Errorf("get local ip failed: %w", err)
	}
	// 取 IP 的最后一段
	workerID := int64(ip[3])
	if workerID < 0 || workerID > 255 {
		return 0, fmt.Errorf("invalid worker id from ip: %d", workerID)
	}
	return workerID, nil
}

// Start 无需保活
func (a *IPAllocator) Start(ctx context.Context, workerID int64) (<-chan error, error) {
	ch := make(chan error)
	return ch, nil
}

// getLocalIP 获取本机第一个非 loopback 的 IPv4 地址
func getLocalIP() (net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip := ipnet.IP.To4(); ip != nil {
				return ip, nil
			}
		}
	}
	return nil, fmt.Errorf("no valid ipv4 address found")
}

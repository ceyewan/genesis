package testkit

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ceyewan/genesis/connector"
)

// NewRedisTLSContainerConfig 启动仅允许 TLS 与命名 ACL 用户的 Redis。
// 返回客户端配置和独立测试 CA；证书、文件和容器由测试生命周期管理。
func NewRedisTLSContainerConfig(t *testing.T) (*connector.RedisConfig, *tls.Config) {
	t.Helper()
	RequireDocker(t)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Genesis test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	ca, err = x509.ParseCertificate(caDER)
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	certificate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "redis.test"}, DNSNames: []string{"redis.test"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, ca, &key.PublicKey, caKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	files := map[string][]byte{
		"ca.crt":     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		"server.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		"server.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		"redis.conf": []byte("port 0\ntls-port 6379\ntls-cert-file /tmp/server.crt\ntls-key-file /tmp/server.key\ntls-ca-cert-file /tmp/ca.crt\ntls-auth-clients no\nuser default off\nuser genesis on >test-only-password ~* +@all\n"),
	}
	dir := t.TempDir()
	var copies []testcontainers.ContainerFile
	for name, data := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, data, 0o600))
		copies = append(copies, testcontainers.ContainerFile{HostFilePath: path, ContainerFilePath: "/tmp/" + name, FileMode: 0o644})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{Image: "redis:7.2-alpine", ExposedPorts: []string{"6379/tcp"}, Cmd: []string{"redis-server", "/tmp/redis.conf"}, Files: copies, WaitingFor: wait.ForListeningPort("6379/tcp").WithStartupTimeout(30 * time.Second)}, Started: true})
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		require.NoError(t, container.Terminate(cleanup))
	})
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "6379/tcp")
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	return &connector.RedisConfig{Addr: net.JoinHostPort(host, port.Port()), Username: "genesis", Password: "test-only-password", DialTimeout: time.Second}, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "redis.test", RootCAs: roots}
}

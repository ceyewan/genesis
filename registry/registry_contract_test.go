package registry

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/resolver"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
)

func TestConfigValidationIsClassifiable(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
	}{
		{name: "negative default ttl", cfg: &Config{DefaultTTL: -time.Second}},
		{name: "subsecond default ttl", cfg: &Config{DefaultTTL: time.Millisecond}},
		{name: "negative retry interval", cfg: &Config{RetryInterval: -time.Second}},
		{name: "negative lease failure buffer", cfg: &Config{LeaseFailureBuffer: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.validate(); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("validate() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestConfigHasMapstructureTags(t *testing.T) {
	tags := map[string]string{
		"Namespace":          "namespace",
		"DefaultTTL":         "default_ttl",
		"RetryInterval":      "retry_interval",
		"LeaseFailureBuffer": "lease_failure_buffer",
	}
	typeOfConfig := reflect.TypeFor[Config]()
	for fieldName, want := range tags {
		field, ok := typeOfConfig.FieldByName(fieldName)
		if !ok {
			t.Fatalf("Config.%s not found", fieldName)
		}
		if got := field.Tag.Get("mapstructure"); got != want {
			t.Fatalf("Config.%s mapstructure tag = %q, want %q", fieldName, got, want)
		}
	}
}

func TestNewClassifiesMissingAndUnconnectedConnector(t *testing.T) {
	_, err := New(nil, nil)
	if !errors.Is(err, connector.ErrClientNil) {
		t.Fatalf("New(nil) error = %v, want connector.ErrClientNil", err)
	}

	conn, err := connector.NewEtcd(&connector.EtcdConfig{
		Name:      "unconnected-registry-test",
		Endpoints: []string{"127.0.0.1:2379"},
	})
	require.NoError(t, err)
	_, err = New(conn, &Config{DefaultTTL: -time.Second})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(invalid config) error = %v, want ErrInvalidConfig", err)
	}

	_, err = New(conn, nil)
	if !errors.Is(err, connector.ErrClientNil) {
		t.Fatalf("New(unconnected) error = %v, want connector.ErrClientNil", err)
	}
}

func TestServiceInstanceRejectsEtcdKeyDelimiters(t *testing.T) {
	tests := []struct {
		name    string
		service *ServiceInstance
	}{
		{
			name: "slash in ID",
			service: &ServiceInstance{
				ID: "instance/other", Name: "orders", Endpoints: []string{"127.0.0.1:9000"},
			},
		},
		{
			name: "slash in name",
			service: &ServiceInstance{
				ID: "instance", Name: "orders/admin", Endpoints: []string{"127.0.0.1:9000"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateServiceInstance(tt.service); !errors.Is(err, ErrInvalidServiceInstance) {
				t.Fatalf("validateServiceInstance() error = %v, want ErrInvalidServiceInstance", err)
			}
		})
	}
}

func TestResolverAddressesAreStableAndSorted(t *testing.T) {
	cc := &testResolverClientConn{}
	r := &etcdResolver{
		registry:    &etcdRegistry{logger: clog.Discard()},
		serviceName: "sorted-resolver-test",
		cc:          cc,
		localCache: map[resolverCacheKey]resolver.Address{
			{instanceID: "instance-c", addr: "127.0.0.1:9003"}: {Addr: "127.0.0.1:9003", ServerName: "sorted-resolver-test"},
			{instanceID: "instance-a", addr: "127.0.0.1:9001"}: {Addr: "127.0.0.1:9001", ServerName: "sorted-resolver-test"},
			{instanceID: "instance-b", addr: "127.0.0.1:9002"}: {Addr: "127.0.0.1:9002", ServerName: "sorted-resolver-test"},
		},
	}

	want := []resolver.Address{
		{Addr: "127.0.0.1:9001", ServerName: "sorted-resolver-test"},
		{Addr: "127.0.0.1:9002", ServerName: "sorted-resolver-test"},
		{Addr: "127.0.0.1:9003", ServerName: "sorted-resolver-test"},
	}
	for range 10 {
		r.pushStateLocked()
		require.Equal(t, want, cc.lastState.Addresses)
	}
}

func TestDeregisterRevokeFailureRetainsLeaseForRetry(t *testing.T) {
	reg := setupRegistry(t, "/test/deregister-retry").(*etcdRegistry)
	service := &ServiceInstance{
		ID:        "deregister-retry-instance",
		Name:      "deregister-retry-service",
		Endpoints: []string{"grpc://127.0.0.1:9000"},
	}
	require.NoError(t, reg.Register(context.Background(), service, 10*time.Second))

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err := reg.Deregister(canceledCtx, service.ID)
	require.Error(t, err)

	reg.mu.Lock()
	_, retained := reg.keepAlives[service.ID]
	reg.mu.Unlock()
	require.True(t, retained, "failed revoke must remain visible for retry and shutdown")

	require.NoError(t, reg.Deregister(context.Background(), service.ID))
	reg.mu.Lock()
	_, retained = reg.keepAlives[service.ID]
	reg.mu.Unlock()
	require.False(t, retained)
}

func TestRegisterDoesNotHoldMutexAcrossNetworkAndShutdownRejectsCommit(t *testing.T) {
	reg := setupRegistry(t, "/test/register-shutdown-commit").(*etcdRegistry)
	reachedCommit := make(chan struct{})
	releaseCommit := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCommit) }) }
	defer release()
	reg.beforeRegisterCommit = func() {
		close(reachedCommit)
		<-releaseCommit
	}

	service := &ServiceInstance{
		ID:        "register-shutdown-instance",
		Name:      "register-shutdown-service",
		Endpoints: []string{"grpc://127.0.0.1:9000"},
	}
	registerDone := make(chan error, 1)
	go func() {
		registerDone <- reg.Register(context.Background(), service, 10*time.Second)
	}()

	select {
	case <-reachedCommit:
	case <-time.After(5 * time.Second):
		t.Fatal("Register did not reach its commit boundary")
	}

	lockAvailable := make(chan int, 1)
	go func() {
		reg.mu.Lock()
		pendingRegistrations := len(reg.registering)
		reg.mu.Unlock()
		lockAvailable <- pendingRegistrations
	}()
	select {
	case pendingRegistrations := <-lockAvailable:
		require.Equal(t, 1, pendingRegistrations)
	case <-time.After(time.Second):
		t.Fatal("Register held registry mutex across its network phase")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := reg.Shutdown(shutdownCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() error = %v, want caller deadline", err)
	}

	release()
	select {
	case err := <-registerDone:
		if !errors.Is(err, ErrRegistryClosed) {
			t.Fatalf("Register() error = %v, want ErrRegistryClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Register did not finish after releasing commit boundary")
	}
	require.NoError(t, reg.Shutdown(context.Background()))

	resp, err := reg.client.Get(context.Background(), reg.buildKey(service.Name, service.ID))
	require.NoError(t, err)
	require.Empty(t, resp.Kvs, "rejected registration left an Etcd key behind")
}

package registry

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"

	"google.golang.org/grpc"
)

// EndpointPicker 选择实例的一个 endpoint
// 用于多 endpoint 场景下决定具体连哪个地址。
type EndpointPicker interface {
	Pick(instance *ServiceInstance) (string, error)
}

// RoundRobinPicker 对单个实例的 endpoints 做轮询选择
// 只在同一个实例的 endpoints 内轮询，不跨实例。
type RoundRobinPicker struct {
	mu   sync.Mutex
	next map[string]uint64
}

// NewRoundRobinPicker 创建默认轮询选择器
func NewRoundRobinPicker() *RoundRobinPicker {
	return &RoundRobinPicker{
		next: make(map[string]uint64),
	}
}

// Pick 选择一个 endpoint
func (p *RoundRobinPicker) Pick(instance *ServiceInstance) (string, error) {
	if instance == nil || len(instance.Endpoints) == 0 {
		return "", xerrors.New("no endpoints available")
	}

	p.mu.Lock()
	idx := p.next[instance.ID] % uint64(len(instance.Endpoints))
	p.next[instance.ID]++
	p.mu.Unlock()

	return instance.Endpoints[idx], nil
}

// StreamFactory 创建具体的 ClientStream
// 由应用层提供：可在此构造具体 client 并创建双向流。
type StreamFactory func(ctx context.Context, conn *grpc.ClientConn, instance *ServiceInstance) (grpc.ClientStream, error)

// StreamHandler 处理流生命周期事件
// OnAdd 会在首次创建与重建时被调用，便于业务打日志/指标。
type StreamHandler interface {
	OnAdd(instance *ServiceInstance, stream grpc.ClientStream)
	OnRemove(instance *ServiceInstance)
	OnError(instance *ServiceInstance, err error)
}

// StreamManagerConfig StreamManager 配置
type StreamManagerConfig struct {
	// ServiceName 要管理的服务名
	ServiceName    string
	// DialOptions 透传给 grpc.NewClient 的选项（TLS/拦截器等）
	DialOptions    []grpc.DialOption
	// EndpointPicker 选择实例的 endpoint（默认轮询）
	EndpointPicker EndpointPicker
	// Factory 创建流的工厂方法（必填）
	Factory        StreamFactory
	// Handler 可选的生命周期回调
	Handler        StreamHandler
}

// StreamOption StreamManager 初始化选项
type StreamOption func(*streamOptions)

type streamOptions struct {
	logger clog.Logger
}

// WithStreamLogger 注入日志记录器
func WithStreamLogger(l clog.Logger) StreamOption {
	return func(o *streamOptions) {
		if l != nil {
			o.logger = l.WithNamespace("registry")
		}
	}
}

func defaultStreamOptions() *streamOptions {
	logger, _ := clog.New(&clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	})
	return &streamOptions{logger: logger}
}

// StreamManager 管理每个实例一条流并自动重建
// 关键行为：
// - 启动时全量 GetService 建立流；
// - Watch 监听上下线，自动增删流；
// - 流断开后自动重建（不内置退避策略）。
type StreamManager struct {
	reg    Registry
	cfg    StreamManagerConfig
	logger clog.Logger

	picker  EndpointPicker
	handler StreamHandler

	mu      sync.RWMutex
	streams map[string]*managedStream

	ctx     context.Context
	cancel  context.CancelFunc
	started uint32
	wg      sync.WaitGroup
}

type managedStream struct {
	instance *ServiceInstance
	endpoint string
	conn     *grpc.ClientConn
	stream   grpc.ClientStream
	cancel   context.CancelFunc
	closed   uint32
}

// NewStreamManager 创建 StreamManager
func NewStreamManager(reg Registry, cfg StreamManagerConfig, opts ...StreamOption) (*StreamManager, error) {
	if reg == nil {
		return nil, xerrors.New("registry is required")
	}
	if cfg.ServiceName == "" {
		return nil, xerrors.New("service name is required")
	}
	if cfg.Factory == nil {
		return nil, xerrors.New("stream factory is required")
	}

	opt := defaultStreamOptions()
	for _, o := range opts {
		o(opt)
	}

	picker := cfg.EndpointPicker
	if picker == nil {
		picker = NewRoundRobinPicker()
	}

	m := &StreamManager{
		reg:     reg,
		cfg:     cfg,
		logger:  opt.logger,
		picker:  picker,
		handler: cfg.Handler,
		streams: make(map[string]*managedStream),
	}

	return m, nil
}

// Start 启动 StreamManager
// 调用后会建立初始流并持续 watch 服务变化。
func (m *StreamManager) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapUint32(&m.started, 0, 1) {
		return xerrors.New("stream manager already started")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	m.ctx, m.cancel = context.WithCancel(ctx)

	instances, err := m.reg.GetService(m.ctx, m.cfg.ServiceName)
	if err != nil {
		m.cancel()
		atomic.StoreUint32(&m.started, 0)
		return xerrors.Wrap(err, "get service failed")
	}

	for _, instance := range instances {
		m.upsert(instance)
	}

	eventCh, err := m.reg.Watch(m.ctx, m.cfg.ServiceName)
	if err != nil {
		m.cancel()
		atomic.StoreUint32(&m.started, 0)
		return xerrors.Wrap(err, "watch service failed")
	}

	m.wg.Add(1)
	go m.run(eventCh)

	return nil
}

// Stop 停止 StreamManager
// 会关闭所有流与连接，确保资源释放。
func (m *StreamManager) Stop(ctx context.Context) error {
	if !atomic.CompareAndSwapUint32(&m.started, 1, 0) {
		return nil
	}

	if m.cancel != nil {
		m.cancel()
	}

	m.mu.Lock()
	for _, ms := range m.streams {
		m.closeManagedStream(ms)
	}
	m.streams = make(map[string]*managedStream)
	m.mu.Unlock()

	m.wg.Wait()
	return nil
}

// Streams 返回当前流快照（instanceID -> stream）
// 只用于读取当前状态，不要修改其中的流对象。
func (m *StreamManager) Streams() map[string]grpc.ClientStream {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]grpc.ClientStream, len(m.streams))
	for id, ms := range m.streams {
		out[id] = ms.stream
	}
	return out
}

// run 处理 Watch 事件
func (m *StreamManager) run(eventCh <-chan ServiceEvent) {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			switch event.Type {
			case EventTypePut:
				m.upsert(event.Service)
			case EventTypeDelete:
				m.remove(event.Service.ID)
			}
		}
	}
}

// upsert 处理 PUT 事件，确保实例有流
func (m *StreamManager) upsert(instance *ServiceInstance) {
	if instance == nil || instance.ID == "" {
		return
	}

	m.mu.RLock()
	existing := m.streams[instance.ID]
	m.mu.RUnlock()

	if existing != nil {
		m.mu.Lock()
		existing.instance = cloneInstance(instance)
		m.mu.Unlock()
		return
	}

	managed, err := m.createManagedStream(m.ctx, instance, nil)
	if err != nil {
		m.handleError(instance, err)
		return
	}

	m.mu.Lock()
	if current := m.streams[instance.ID]; current != nil {
		m.mu.Unlock()
		m.closeManagedStream(managed)
		return
	}
	m.streams[instance.ID] = managed
	m.mu.Unlock()

	m.handleAdd(instance, managed.stream)
}

// remove 处理 DELETE 事件，关闭并移除实例流
func (m *StreamManager) remove(instanceID string) {
	if instanceID == "" {
		return
	}

	m.mu.Lock()
	ms := m.streams[instanceID]
	if ms != nil {
		delete(m.streams, instanceID)
	}
	m.mu.Unlock()

	if ms == nil {
		return
	}

	m.closeManagedStream(ms)
	m.handleRemove(ms.instance)
}

// rebuild 在流断开时尝试重建
// 先复用已有连接，失败后新建连接。
func (m *StreamManager) rebuild(instanceID string) {
	m.mu.RLock()
	current := m.streams[instanceID]
	m.mu.RUnlock()

	if current == nil {
		return
	}

	managed, err := m.createManagedStream(m.ctx, current.instance, current.conn)
	if err != nil {
		m.logger.Warn("rebuild stream with existing connection failed",
			clog.String("service_name", m.cfg.ServiceName),
			clog.String("service_id", instanceID),
			clog.Error(err))
		managed, err = m.createManagedStream(m.ctx, current.instance, nil)
	}
	if err != nil {
		m.handleError(current.instance, err)
		return
	}

	m.mu.Lock()
	if m.streams[instanceID] != current {
		m.mu.Unlock()
		m.closeManagedStream(managed)
		return
	}
	m.streams[instanceID] = managed
	m.mu.Unlock()

	m.handleAdd(managed.instance, managed.stream)
}

// createManagedStream 为实例创建流
// conn 为 nil 时会创建新连接，否则复用已有连接。
func (m *StreamManager) createManagedStream(parent context.Context, instance *ServiceInstance, conn *grpc.ClientConn) (*managedStream, error) {
	endpoint, err := m.picker.Pick(instance)
	if err != nil {
		return nil, err
	}
	endpoint = normalizeEndpoint(endpoint)
	if endpoint == "" {
		return nil, xerrors.New("invalid endpoint")
	}

	createdConn := false
	if conn == nil {
		dialOpts := m.cfg.DialOptions
		if len(dialOpts) == 0 {
			return nil, xerrors.New("DialOptions required, e.g., grpc.WithTransportCredentials()")
		}

		conn, err = grpc.NewClient(endpoint, dialOpts...)
		if err != nil {
			return nil, err
		}
		createdConn = true
	}

	streamCtx, cancel := context.WithCancel(parent)
	stream, err := m.cfg.Factory(streamCtx, conn, instance)
	if err != nil {
		cancel()
		if createdConn && conn != nil {
			_ = conn.Close()
		}
		return nil, err
	}

	ms := &managedStream{
		instance: cloneInstance(instance),
		endpoint: endpoint,
		conn:     conn,
		stream:   stream,
		cancel:   cancel,
	}

	m.wg.Add(1)
	go m.monitorStream(instance.ID, ms)

	return ms, nil
}

// monitorStream 监听 stream.Context() 结束并触发重建
func (m *StreamManager) monitorStream(instanceID string, ms *managedStream) {
	defer m.wg.Done()

	<-ms.stream.Context().Done()

	if atomic.LoadUint32(&ms.closed) == 1 {
		return
	}

	m.mu.RLock()
	current := m.streams[instanceID]
	m.mu.RUnlock()
	if current != ms {
		return
	}

	m.logger.Warn("stream closed, rebuilding",
		clog.String("service_name", m.cfg.ServiceName),
		clog.String("service_id", instanceID))

	m.rebuild(instanceID)
}

// closeManagedStream 主动关闭流和连接
func (m *StreamManager) closeManagedStream(ms *managedStream) {
	if ms == nil {
		return
	}

	atomic.StoreUint32(&ms.closed, 1)
	if ms.cancel != nil {
		ms.cancel()
	}
	if ms.stream != nil {
		_ = ms.stream.CloseSend()
	}
	if ms.conn != nil {
		_ = ms.conn.Close()
	}
}

// handleAdd 触发生命周期回调
func (m *StreamManager) handleAdd(instance *ServiceInstance, stream grpc.ClientStream) {
	if m.handler != nil {
		m.handler.OnAdd(instance, stream)
	}
}

// handleRemove 触发生命周期回调
func (m *StreamManager) handleRemove(instance *ServiceInstance) {
	if m.handler != nil {
		m.handler.OnRemove(instance)
	}
}

// handleError 记录错误并触发生命周期回调
func (m *StreamManager) handleError(instance *ServiceInstance, err error) {
	if err == nil {
		return
	}
	m.logger.Error("stream manager error",
		clog.String("service_name", m.cfg.ServiceName),
		clog.String("service_id", instance.ID),
		clog.Error(err))
	if m.handler != nil {
		m.handler.OnError(instance, err)
	}
}

// normalizeEndpoint 移除协议前缀，得到 host:port
func normalizeEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	endpoint = strings.TrimPrefix(endpoint, "grpc://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	return endpoint
}

// cloneInstance 拷贝实例数据，避免外部修改影响内部状态
func cloneInstance(instance *ServiceInstance) *ServiceInstance {
	if instance == nil {
		return nil
	}

	clone := *instance
	if instance.Metadata != nil {
		clone.Metadata = make(map[string]string, len(instance.Metadata))
		for k, v := range instance.Metadata {
			clone.Metadata[k] = v
		}
	}
	if instance.Endpoints != nil {
		clone.Endpoints = append([]string(nil), instance.Endpoints...)
	}
	return &clone
}

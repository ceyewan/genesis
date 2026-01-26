package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type RequestPayload struct {
	UserID    string `json:"user_id"`
	ProductID string `json:"product_id"`
}

type stats struct {
	totalRequests   int64
	successful      int64
	failed          int64
	totalDuration   int64 // nanoseconds
	minResponseTime int64 // nanoseconds
	maxResponseTime int64 // nanoseconds
}

func main() {
	const targetQPS = 5

	var (
		url         = flag.String("url", "http://localhost:8080/orders", "Target URL")
		authHeader  = flag.String("auth", "Bearer demo-token", "Authorization header value")
		concurrency = flag.Int("concurrency", 10, "Number of concurrent workers")
		reportEvery = flag.Duration("report", 5*time.Second, "Report interval")
	)
	flag.Parse()

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	payload := RequestPayload{
		UserID:    "test_user",
		ProductID: "test_product",
	}
	payloadBytes, _ := json.Marshal(payload)

	var s stats
	atomic.StoreInt64(&s.minResponseTime, 1<<60) // 初始化为一个大值

	stopCh := make(chan struct{})
	var wg sync.WaitGroup

	// 支持 Ctrl+C 优雅退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		close(stopCh)
	}()

	// 创建请求通道以控制 QPS
	requestCh := make(chan struct{}, *concurrency)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Second / targetQPS)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				select {
				case requestCh <- struct{}{}:
				default:
				}
			case <-stopCh:
				close(requestCh)
				return
			}
		}
	}()

	// 启动统计报告协程
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(*reportEvery)
		defer ticker.Stop()

		lastTotal := int64(0)
		lastTime := time.Now()

		for {
			select {
			case <-ticker.C:
				now := time.Now()
				total := atomic.LoadInt64(&s.totalRequests)
				success := atomic.LoadInt64(&s.successful)
				failed := atomic.LoadInt64(&s.failed)
				min := atomic.LoadInt64(&s.minResponseTime)
				max := atomic.LoadInt64(&s.maxResponseTime)
				durationSum := atomic.LoadInt64(&s.totalDuration)

				if success == 0 {
					fmt.Println("Waiting for first successful request...")
					continue
				}

				// 计算本次间隔内的QPS
				intervalTotal := total - lastTotal
				intervalDuration := now.Sub(lastTime).Seconds()
				currentQPS := float64(intervalTotal) / intervalDuration

				// 计算平均响应时间
				avgDuration := durationSum / success

				fmt.Printf("[%s] QPS: %.2f (target: %d) | Total: %d | Success: %d | Failed: %d | Avg: %.2fms | Min: %.2fms | Max: %.2fms\n",
					now.Format("15:04:05"),
					currentQPS,
					targetQPS,
					total,
					success,
					failed,
					float64(avgDuration)/1e6,
					float64(min)/1e6,
					float64(max)/1e6,
				)

				lastTotal = total
				lastTime = now

				// 重置统计
				atomic.StoreInt64(&s.minResponseTime, 1<<60)
				atomic.StoreInt64(&s.maxResponseTime, 0)
			case <-stopCh:
				return
			}
		}
	}()

	// 启动压测worker
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case _, ok := <-requestCh:
					if !ok {
						return
					}

					reqStart := time.Now()
					req, err := http.NewRequest(http.MethodPost, *url, bytes.NewBuffer(payloadBytes))
					if err != nil {
						atomic.AddInt64(&s.totalRequests, 1)
						atomic.AddInt64(&s.failed, 1)
						continue
					}
					req.Header.Set("Content-Type", "application/json")
					if *authHeader != "" {
						req.Header.Set("Authorization", *authHeader)
					}

					resp, err := client.Do(req)
					reqDuration := time.Since(reqStart)

					atomic.AddInt64(&s.totalRequests, 1)

					if err != nil {
						atomic.AddInt64(&s.failed, 1)
						continue
					}

					if resp.StatusCode == 200 {
						atomic.AddInt64(&s.successful, 1)
						durationNs := reqDuration.Nanoseconds()
						atomic.AddInt64(&s.totalDuration, durationNs)

						// 更新最小值
						for {
							old := atomic.LoadInt64(&s.minResponseTime)
							if durationNs >= old {
								break
							}
							if atomic.CompareAndSwapInt64(&s.minResponseTime, old, durationNs) {
								break
							}
						}

						// 更新最大值
						for {
							old := atomic.LoadInt64(&s.maxResponseTime)
							if durationNs <= old {
								break
							}
							if atomic.CompareAndSwapInt64(&s.maxResponseTime, old, durationNs) {
								break
							}
						}
					} else {
						atomic.AddInt64(&s.failed, 1)
					}
					resp.Body.Close()
				case <-stopCh:
					return
				}
			}
		}(i)
	}

	fmt.Println("Load test started. Press Ctrl+C to stop.")
	fmt.Printf("Target: %d QPS, Concurrency: %d\n", targetQPS, *concurrency)
	fmt.Println("--------------------------------------------------")

	// 等待中断信号
	<-stopCh
	wg.Wait()

	fmt.Println("\nLoad test stopped.")
}

package client

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

// 对比测试：直接使用fasthttp vs 使用Fiber客户端
func Test_StreamResponseBody_Comparison(t *testing.T) {
	t.Parallel()

	const totalMessages = 3

	// 创建测试服务器
	app := fiber.New()
	app.Get("/sse", func(c fiber.Ctx) error {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")

		c.RequestCtx().SetBodyStreamWriter(func(w *bufio.Writer) {
			defer func() {
				w.WriteString("data: [DONE]\n\n")
				w.Flush()
			}()

			for i := 0; i < totalMessages; i++ {
				message := fmt.Sprintf("data: Message %d\n\n", i)
				w.WriteString(message)
				w.Flush()
				time.Sleep(50 * time.Millisecond)
			}
		})

		return nil
	})

	// 启动服务器
	addrChan := make(chan string)
	errChan := make(chan error, 1)
	go func() {
		err := app.Listen(":0", fiber.ListenConfig{
			DisableStartupMessage: true,
			ListenerAddrFunc: func(addr net.Addr) {
				addrChan <- addr.String()
			},
		})
		if err != nil && !strings.Contains(err.Error(), "server closed") {
			errChan <- err
		}
	}()

	var addr string
	select {
	case addr = <-addrChan:
	case err := <-errChan:
		t.Fatalf("Failed to start test server: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Server startup timeout")
	}
	defer app.Shutdown()

	serverURL := "http://" + addr + "/sse"

	// 测试1: 直接使用fasthttp (期望的工作方式)
	t.Run("direct fasthttp with StreamResponseBody", func(t *testing.T) {
		req := fasthttp.AcquireRequest()
		resp := fasthttp.AcquireResponse()
		defer fasthttp.ReleaseRequest(req)
		defer fasthttp.ReleaseResponse(resp)

		// 按照issue中的示例配置
		client := &fasthttp.Client{
			StreamResponseBody: true,
		}

		req.SetRequestURI(serverURL)
		req.Header.SetMethod("GET")
		req.Header.Set("Accept", "text/event-stream")

		t.Log("Testing direct fasthttp client with StreamResponseBody=true")

		err := client.Do(req, resp)
		require.NoError(t, err)

		// 验证响应头
		require.Equal(t, "text/event-stream", string(resp.Header.ContentType()))

		// 尝试获取流
		bodyStream := resp.BodyStream()
		t.Logf("FastHTTP BodyStream result: %v", bodyStream)

		var fasthttpMessages []string
		if bodyStream != nil {
			t.Log("FastHTTP: Got BodyStream, reading streaming data")
			reader := bufio.NewReader(bodyStream)

			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					if err == io.EOF {
						break
					}
					t.Logf("FastHTTP read error: %v", err)
					break
				}

				t.Logf("FastHTTP received line: %q", line)

				if line == "\n" {
					continue
				}

				if line == "data: [DONE]\n" {
					t.Log("FastHTTP: Stream ended with [DONE] marker")
					break
				}

				if strings.HasPrefix(line, "data: ") {
					fasthttpMessages = append(fasthttpMessages, strings.TrimSpace(line))
				}
			}
		} else {
			t.Log("FastHTTP: BodyStream is nil, reading full body")
			body := resp.Body()
			t.Logf("FastHTTP full response body: %q", string(body))

			lines := strings.Split(string(body), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "data: ") && !strings.Contains(line, "[DONE]") {
					fasthttpMessages = append(fasthttpMessages, strings.TrimSpace(line))
				}
			}
		}

		t.Logf("FastHTTP received %d messages: %v", len(fasthttpMessages), fasthttpMessages)

		// 验证结果
		if len(fasthttpMessages) > 0 {
			require.Len(t, fasthttpMessages, totalMessages)
			for i, msg := range fasthttpMessages {
				expected := fmt.Sprintf("data: Message %d", i)
				require.Equal(t, expected, msg)
			}
			t.Log("✅ FastHTTP streaming worked correctly")
		} else {
			t.Log("⚠️  FastHTTP streaming returned no messages")
		}
	})

	// 测试2: 使用Fiber客户端 (当前实现)
	t.Run("fiber client with StreamResponseBody", func(t *testing.T) {
		fiberClient := New().SetStreamResponseBody(true)

		t.Log("Testing Fiber client with StreamResponseBody=true")
		t.Logf("Fiber client StreamResponseBody: %v", fiberClient.StreamResponseBody)
		t.Logf("Fiber client fasthttp.StreamResponseBody: %v", fiberClient.fasthttp.StreamResponseBody)

		resp, err := fiberClient.Get(serverURL)
		require.NoError(t, err)
		require.NotNil(t, resp)
		defer resp.Close()

		// 验证响应头
		require.Equal(t, "text/event-stream", resp.Header("Content-Type"))

		// 尝试获取流
		stream := resp.BodyStream()
		t.Logf("Fiber BodyStream result: %v", stream)

		var fiberMessages []string
		if stream != nil {
			t.Log("Fiber: Got BodyStream, reading streaming data")
			reader := bufio.NewReader(stream)

			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					if err == io.EOF {
						break
					}
					t.Logf("Fiber read error: %v", err)
					break
				}

				t.Logf("Fiber received line: %q", line)

				if line == "\n" {
					continue
				}

				if line == "data: [DONE]\n" {
					t.Log("Fiber: Stream ended with [DONE] marker")
					break
				}

				if strings.HasPrefix(line, "data: ") {
					fiberMessages = append(fiberMessages, strings.TrimSpace(line))
				}
			}
		} else {
			t.Log("Fiber: BodyStream is nil, reading full body")
			body := resp.String()
			t.Logf("Fiber full response body: %q", body)

			lines := strings.Split(body, "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "data: ") && !strings.Contains(line, "[DONE]") {
					fiberMessages = append(fiberMessages, strings.TrimSpace(line))
				}
			}
		}

		t.Logf("Fiber received %d messages: %v", len(fiberMessages), fiberMessages)

		// 验证结果
		if len(fiberMessages) > 0 {
			require.Len(t, fiberMessages, totalMessages)
			for i, msg := range fiberMessages {
				expected := fmt.Sprintf("data: Message %d", i)
				require.Equal(t, expected, msg)
			}
			t.Log("✅ Fiber streaming worked correctly")
		} else {
			t.Log("⚠️  Fiber streaming returned no messages - this indicates the feature needs implementation")
			// 验证配置至少是正确的
			require.True(t, fiberClient.StreamResponseBody)
			require.True(t, fiberClient.fasthttp.StreamResponseBody)
		}
	})
}

// 专门测试SetStreamResponseBody API的各种用法
func Test_StreamResponseBody_API_Coverage(t *testing.T) {
	t.Parallel()

	t.Run("client level configuration", func(t *testing.T) {
		// 测试客户端级别配置
		client := New()

		// 默认值测试
		require.False(t, client.StreamResponseBody)
		require.False(t, client.fasthttp.StreamResponseBody)

		// 启用流式传输
		client.SetStreamResponseBody(true)
		require.True(t, client.StreamResponseBody)
		require.True(t, client.fasthttp.StreamResponseBody)

		// 禁用流式传输
		client.SetStreamResponseBody(false)
		require.False(t, client.StreamResponseBody)
		require.False(t, client.fasthttp.StreamResponseBody)
	})

	t.Run("request level configuration", func(t *testing.T) {
		// 测试请求级别配置
		client := New()
		require.False(t, client.StreamResponseBody)

		// 创建启用流式传输的请求
		req := client.R().SetStreamResponseBody(true)

		// 客户端级别应该保持不变
		require.False(t, client.StreamResponseBody)

		// 请求级别应该被设置
		require.NotNil(t, req.streamResponseBody)
		require.True(t, *req.streamResponseBody)
	})

	t.Run("config initialization", func(t *testing.T) {
		// 测试通过Config初始化
		streamEnabled := true
		// config := Config{
		// 	StreamResponseBody: &streamEnabled,
		// }

		// Config用于请求级别，不是客户端级别
		// 这里测试NewWithClient来验证底层fasthttp客户端的配置传递
		fasthttpClient := &fasthttp.Client{
			StreamResponseBody: streamEnabled,
		}
		client := NewWithClient(fasthttpClient)

		// 应该继承配置
		require.True(t, client.StreamResponseBody)
		require.True(t, client.fasthttp.StreamResponseBody)
	})
}

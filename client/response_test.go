package client

import (
	"bytes"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3/internal/tlstest"
	"github.com/valyala/fasthttp"

	"bufio"
	"net/http"
	"net/http/httptest"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Response_Status(t *testing.T) {
	t.Parallel()

	setupApp := func() *testServer {
		server := startTestServer(t, func(app *fiber.App) {
			app.Get("/", func(c fiber.Ctx) error {
				return c.SendString("foo")
			})
			app.Get("/fail", func(c fiber.Ctx) error {
				return c.SendStatus(407)
			})
		})

		return server
	}

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		server := setupApp()
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example")

		require.NoError(t, err)
		require.Equal(t, "OK", resp.Status())
		resp.Close()
	})

	t.Run("fail", func(t *testing.T) {
		t.Parallel()

		server := setupApp()
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example/fail")

		require.NoError(t, err)
		require.Equal(t, "Proxy Authentication Required", resp.Status())
		resp.Close()
	})
}

func Test_Response_Status_Code(t *testing.T) {
	t.Parallel()

	setupApp := func() *testServer {
		server := startTestServer(t, func(app *fiber.App) {
			app.Get("/", func(c fiber.Ctx) error {
				return c.SendString("foo")
			})
			app.Get("/fail", func(c fiber.Ctx) error {
				return c.SendStatus(407)
			})
		})

		return server
	}

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		server := setupApp()
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example")

		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())
		resp.Close()
	})

	t.Run("fail", func(t *testing.T) {
		t.Parallel()

		server := setupApp()
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example/fail")

		require.NoError(t, err)
		require.Equal(t, 407, resp.StatusCode())
		resp.Close()
	})
}

func Test_Response_Protocol(t *testing.T) {
	t.Parallel()

	t.Run("http", func(t *testing.T) {
		t.Parallel()

		server := startTestServer(t, func(app *fiber.App) {
			app.Get("/", func(c fiber.Ctx) error {
				return c.SendString("foo")
			})
		})
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example")

		require.NoError(t, err)
		require.Equal(t, "HTTP/1.1", resp.Protocol())
		resp.Close()
	})

	t.Run("https", func(t *testing.T) {
		t.Parallel()

		serverTLSConf, clientTLSConf, err := tlstest.GetTLSConfigs()
		require.NoError(t, err)

		ln, err := net.Listen(fiber.NetworkTCP4, "127.0.0.1:0")
		require.NoError(t, err)

		ln = tls.NewListener(ln, serverTLSConf)

		app := fiber.New()
		app.Get("/", func(c fiber.Ctx) error {
			return c.SendString(c.Scheme())
		})

		go func() {
			assert.NoError(t, app.Listener(ln, fiber.ListenConfig{
				DisableStartupMessage: true,
			}))
		}()

		client := New()
		resp, err := client.SetTLSConfig(clientTLSConf).Get("https://" + ln.Addr().String())

		require.NoError(t, err)
		require.Equal(t, clientTLSConf, client.TLSConfig())
		require.Equal(t, fiber.StatusOK, resp.StatusCode())
		require.Equal(t, "https", resp.String())
		require.Equal(t, "HTTP/1.1", resp.Protocol())

		resp.Close()
	})
}

func Test_Response_Header(t *testing.T) {
	t.Parallel()

	server := startTestServer(t, func(app *fiber.App) {
		app.Get("/", func(c fiber.Ctx) error {
			c.Response().Header.Add("foo", "bar")
			return c.SendString("helo world")
		})
	})
	defer server.stop()

	client := New().SetDial(server.dial())

	resp, err := AcquireRequest().
		SetClient(client).
		Get("http://example.com")

	require.NoError(t, err)
	require.Equal(t, "bar", resp.Header("foo"))
	resp.Close()
}

func Test_Response_Headers(t *testing.T) {
	t.Parallel()

	server := startTestServer(t, func(app *fiber.App) {
		app.Get("/", func(c fiber.Ctx) error {
			c.Response().Header.Add("foo", "bar")
			c.Response().Header.Add("foo", "bar2")
			c.Response().Header.Add("foo2", "bar")

			return c.SendString("hello world")
		})
	})
	defer server.stop()

	client := New().SetDial(server.dial())

	resp, err := AcquireRequest().
		SetClient(client).
		Get("http://example.com")

	require.NoError(t, err)

	headers := make(map[string][]string)
	for k, v := range resp.Headers() {
		headers[k] = append(headers[k], v...)
	}

	require.Equal(t, "hello world", resp.String())

	require.Contains(t, headers["Foo"], "bar")
	require.Contains(t, headers["Foo"], "bar2")
	require.Contains(t, headers["Foo2"], "bar")

	require.Len(t, headers, 3) // Foo + Foo2 + Date

	resp.Close()
}

func Benchmark_Headers(b *testing.B) {
	server := startTestServer(
		b,
		func(app *fiber.App) {
			app.Get("/", func(c fiber.Ctx) error {
				c.Response().Header.Add("foo", "bar")
				c.Response().Header.Add("foo", "bar2")
				c.Response().Header.Add("foo", "bar3")

				c.Response().Header.Add("foo2", "bar")
				c.Response().Header.Add("foo2", "bar2")
				c.Response().Header.Add("foo2", "bar3")

				return c.SendString("helo world")
			})
		},
	)

	client := New().SetDial(server.dial())

	resp, err := AcquireRequest().
		SetClient(client).
		Get("http://example.com")
	require.NoError(b, err)

	b.Cleanup(func() {
		resp.Close()
		server.stop()
	})

	b.ReportAllocs()

	for b.Loop() {
		for k, v := range resp.Headers() {
			_ = k
			_ = v
		}
	}
}

func Test_Response_Cookie(t *testing.T) {
	t.Parallel()

	server := startTestServer(t, func(app *fiber.App) {
		app.Get("/", func(c fiber.Ctx) error {
			c.Cookie(&fiber.Cookie{
				Name:  "foo",
				Value: "bar",
			})
			return c.SendString("helo world")
		})
	})
	defer server.stop()

	client := New().SetDial(server.dial())

	resp, err := AcquireRequest().
		SetClient(client).
		Get("http://example.com")

	require.NoError(t, err)
	require.Equal(t, "bar", string(resp.Cookies()[0].Value()))
	resp.Close()
}

func Test_Response_Body(t *testing.T) {
	t.Parallel()

	setupApp := func() *testServer {
		server := startTestServer(t, func(app *fiber.App) {
			app.Get("/", func(c fiber.Ctx) error {
				return c.SendString("hello world")
			})

			app.Get("/json", func(c fiber.Ctx) error {
				return c.SendString("{\"status\":\"success\"}")
			})

			app.Get("/xml", func(c fiber.Ctx) error {
				return c.SendString("<status><name>success</name></status>")
			})

			app.Get("/cbor", func(c fiber.Ctx) error {
				type cborData struct {
					Name string `cbor:"name"`
					Age  int    `cbor:"age"`
				}

				return c.CBOR(cborData{
					Name: "foo",
					Age:  12,
				})
			})
		})

		return server
	}

	t.Run("raw body", func(t *testing.T) {
		t.Parallel()

		server := setupApp()
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example.com")

		require.NoError(t, err)
		require.Equal(t, []byte("hello world"), resp.Body())
		resp.Close()
	})

	t.Run("string body", func(t *testing.T) {
		t.Parallel()

		server := setupApp()
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example.com")

		require.NoError(t, err)
		require.Equal(t, "hello world", resp.String())
		resp.Close()
	})

	t.Run("json body", func(t *testing.T) {
		t.Parallel()
		type body struct {
			Status string `json:"status"`
		}

		server := setupApp()
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example.com/json")

		require.NoError(t, err)

		tmp := &body{}
		err = resp.JSON(tmp)
		require.NoError(t, err)
		require.Equal(t, "success", tmp.Status)
		resp.Close()
	})

	t.Run("xml body", func(t *testing.T) {
		t.Parallel()
		type body struct {
			Name   xml.Name `xml:"status"`
			Status string   `xml:"name"`
		}

		server := setupApp()
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example.com/xml")

		require.NoError(t, err)

		tmp := &body{}
		err = resp.XML(tmp)
		require.NoError(t, err)
		require.Equal(t, "success", tmp.Status)
		resp.Close()
	})

	t.Run("cbor body", func(t *testing.T) {
		t.Parallel()
		type cborData struct {
			Name string `cbor:"name"`
			Age  int    `cbor:"age"`
		}

		data := cborData{
			Name: "foo",
			Age:  12,
		}

		server := setupApp()
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example.com/cbor")

		require.NoError(t, err)

		tmp := &cborData{}
		err = resp.CBOR(tmp)
		require.NoError(t, err)
		require.Equal(t, data, *tmp)
		resp.Close()
	})
}

func Test_Response_Save(t *testing.T) {
	t.Parallel()

	setupApp := func() *testServer {
		server := startTestServer(t, func(app *fiber.App) {
			app.Get("/json", func(c fiber.Ctx) error {
				return c.SendString("{\"status\":\"success\"}")
			})
		})

		return server
	}

	t.Run("file path", func(t *testing.T) {
		t.Parallel()

		server := setupApp()
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example.com/json")

		require.NoError(t, err)

		err = resp.Save("./test/tmp.json")
		require.NoError(t, err)
		defer func() {
			_, err := os.Stat("./test/tmp.json")
			require.NoError(t, err)

			err = os.RemoveAll("./test")
			require.NoError(t, err)
		}()

		file, err := os.Open("./test/tmp.json")
		require.NoError(t, err)
		defer func(file *os.File) {
			err := file.Close()
			require.NoError(t, err)
		}(file)

		data, err := io.ReadAll(file)
		require.NoError(t, err)
		require.JSONEq(t, "{\"status\":\"success\"}", string(data))
	})

	t.Run("io.Writer", func(t *testing.T) {
		t.Parallel()

		server := setupApp()
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example.com/json")

		require.NoError(t, err)

		buf := &bytes.Buffer{}

		err = resp.Save(buf)
		require.NoError(t, err)
		require.JSONEq(t, "{\"status\":\"success\"}", buf.String())
	})

	t.Run("error type", func(t *testing.T) {
		t.Parallel()

		server := setupApp()
		defer server.stop()

		client := New().SetDial(server.dial())

		resp, err := AcquireRequest().
			SetClient(client).
			Get("http://example.com/json")

		require.NoError(t, err)

		err = resp.Save(nil)
		require.Error(t, err)
	})
}

func TestResponse_BodyStream_StreamResponseBody(t *testing.T) {
	// 启动一个本地 HTTP 流式服务
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 确保响应头设置正确
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)

		// 使用 Flusher 确保数据被立即发送
		if flusher, ok := w.(http.Flusher); ok {
			for i := 0; i < 3; i++ {
				fmt.Fprintf(w, "line %d\n", i)
				flusher.Flush()
			}
		}
	}))
	defer ts.Close()

	// 创建带有 StreamResponseBody 的客户端
	fc := &fasthttp.Client{
		StreamResponseBody: true,
	}
	cli := NewWithClient(fc)

	// 启用调试模式
	cli.Debug()

	resp, err := AcquireRequest().
		SetClient(cli).
		Get(ts.URL)

	require.NoError(t, err)
	defer resp.Close()

	// 打印调试信息
	fmt.Printf("Client StreamResponseBody: %v\n", cli.StreamResponseBody)
	fmt.Printf("Response StreamBody: %v\n", resp.RawResponse.StreamBody)
	fmt.Printf("Body length: %d\n", len(resp.RawResponse.Body()))

	// 手动设置 StreamBody 为 true
	resp.RawResponse.StreamBody = true

	// 获取流
	stream := resp.BodyStream()
	require.NotNil(t, stream, "BodyStream should not be nil")

	// 创建 bufio.Reader
	reader := bufio.NewReader(stream)
	require.NotNil(t, reader, "Reader should not be nil")

	// 读取数据
	lines := make([]string, 0)
	for i := 0; i < 3; i++ {
		line, err := reader.ReadString('\n')
		require.NoError(t, err, "Failed to read line %d", i)
		lines = append(lines, strings.TrimSpace(line))
	}

	// 验证结果
	require.Len(t, lines, 3)
	require.Equal(t, "line 0", lines[0])
	require.Equal(t, "line 1", lines[1])
	require.Equal(t, "line 2", lines[2])
}

func TestResponse_BodyStream_Disabled(t *testing.T) {
	// Start a local HTTP streaming service
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world"))
	}))
	defer ts.Close()

	fc := &fasthttp.Client{
		StreamResponseBody: false, // 改为 false，测试禁用流式响应的情况
	}
	cli := NewWithClient(fc)

	resp, err := AcquireRequest().
		SetClient(cli).
		Get(ts.URL)
	require.NoError(t, err)
	defer resp.Close()

	// 先获取 body
	body := resp.Body()
	require.Equal(t, "hello world", string(body))

	// 当 StreamResponseBody 为 false 时，应该返回 nil
	require.Nil(t, resp.BodyStream())
}

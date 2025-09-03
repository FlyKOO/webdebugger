package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var (
	defaultPort = getEnv("PORT", "8080")
	addr        = flag.String("addr", ":"+defaultPort, "http service address")
)

// WebSocket 升级器（开发环境允许任意 Origin；生产请按需校验）
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// 注意：生产环境请替换为严格的 Origin 校验
	CheckOrigin: func(r *http.Request) bool { return true },
	// 可选：从客户端提供的协议中选择（如果需要）
	// Subprotocols: []string{"json", "binary"},
}

func main() {
	flag.Parse()

	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/", homeHandler)

	log.Printf("WebSocket echo server listening on %s  (ws://127.0.0.1%s/ws)\n", *addr, *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	// —— 连接参数打印（在升级前就能拿到完整的 HTTP 请求）——
	now := time.Now().Format(time.RFC3339)
	remoteIP, remotePort := splitHostPort(r.RemoteAddr)
	forwardedFor := r.Header.Get("X-Forwarded-For")
	realIP := r.Header.Get("X-Real-IP")
	origin := r.Header.Get("Origin")
	ua := r.Header.Get("User-Agent")
	subprotoRequested := r.Header.Values("Sec-WebSocket-Protocol")

	// Query 参数
	query := r.URL.Query()

	// 全量 Header（便于排查）
	headerJSON, _ := json.MarshalIndent(r.Header, "", "  ")

	// Cookies
	cookies := make(map[string]string)
	for _, c := range r.Cookies() {
		cookies[c.Name] = c.Value
	}
	cookieJSON, _ := json.MarshalIndent(cookies, "", "  ")

	log.Printf(
		"\n==== New WebSocket connection @ %s ====\n"+
			"RequestURI: %s\nPath: %s\nRawQuery: %s\n"+
			"RemoteAddr: %s (ip=%s, port=%s)\nX-Forwarded-For: %s\nX-Real-IP: %s\n"+
			"Origin: %s\nUser-Agent: %s\n"+
			"Subprotocols (requested): %v\n"+
			"Query: %v\nHeaders:\n%s\nCookies:\n%s\n",
		now, r.RequestURI, r.URL.Path, r.URL.RawQuery,
		r.RemoteAddr, remoteIP, remotePort, forwardedFor, realIP,
		origin, ua, subprotoRequested, query, string(headerJSON), string(cookieJSON),
	)

	// —— 升级为 WebSocket —— //
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// 记录最终选中的子协议
	if sp := conn.Subprotocol(); sp != "" {
		log.Printf("Subprotocol (selected): %s", sp)
	}

	// 心跳与超时（可根据需要调整）
	conn.SetReadLimit(1 << 20) // 1MB
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		// 收到 pong 后延长读超时
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// 定时发送 ping 保活
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	// 写协程（只负责发送 ping）
	writeErrCh := make(chan error, 1)
	go func() {
		for range pingTicker.C {
			if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second)); err != nil {
				writeErrCh <- fmt.Errorf("write ping error: %w", err)
				return
			}
		}
	}()

	// 读循环 + 回显
	for {
		select {
		case err := <-writeErrCh:
			log.Printf("writer goroutine exit: %v", err)
			return
		default:
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				// 常见：客户端正常关闭会出现 CloseError
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Printf("client closed: %v", err)
				} else {
					log.Printf("read error: %v", err)
				}
				return
			}

			// 打印消息（文本过长时可截断）
			preview := string(msg)
			if len(preview) > 512 {
				preview = preview[:512] + "...(truncated)"
			}
			log.Printf("recv message: type=%s len=%d preview=%q",
				messageTypeName(mt), len(msg), preview)

			// 原样回显
			if err := conn.WriteMessage(mt, msg); err != nil {
				log.Printf("write error: %v", err)
				return
			}
		}
	}
}

// 提供一个简单的测试页面： http://localhost:8080/
func homeHandler(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if strings.HasPrefix(host, "[::]") || strings.HasPrefix(host, "0.0.0.0") {
		host = "127.0.0.1" + strings.TrimPrefix(host, "0.0.0.0")
	}
	html := `<!doctype html>
<html>
<head><meta charset="utf-8"><title>WS Echo Test</title></head>
<body>
<h3>WebSocket Echo Test</h3>
<p>连接示例：<code>ws://` + host + `/ws?uid=123&token=abc</code></p>
<input id="qs" style="width: 420px" value="uid=123&token=abc">
<button onclick="connect()">Connect</button>
<div id="status"></div>
<hr/>
<input id="msg" style="width: 420px" value="hello">
<button onclick="sendMsg()">Send</button>
<pre id="log"></pre>
<script>
let ws;
function connect() {
  const qs = document.getElementById('qs').value;
  const url = 'ws://' + location.host + '/ws' + (qs ? '?' + qs : '');
  ws = new WebSocket(url, ['json']); // 测试子协议
  ws.onopen = () => log('open');
  ws.onmessage = ev => log('recv: ' + ev.data);
  ws.onclose = ev => log('close: code=' + ev.code + ' reason=' + ev.reason);
  ws.onerror = ev => log('error');
}
function sendMsg() {
  if (!ws || ws.readyState !== 1) return log('not open');
  const v = document.getElementById('msg').value;
  ws.send(v);
}
function log(s){ document.getElementById('log').textContent += s + '\n'; }
</script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func messageTypeName(mt int) string {
	switch mt {
	case websocket.TextMessage:
		return "Text"
	case websocket.BinaryMessage:
		return "Binary"
	case websocket.CloseMessage:
		return "Close"
	case websocket.PingMessage:
		return "Ping"
	case websocket.PongMessage:
		return "Pong"
	default:
		return fmt.Sprintf("Unknown(%d)", mt)
	}
}

func splitHostPort(remote string) (ip, port string) {
	host, p, err := net.SplitHostPort(remote)
	if err != nil {
		return remote, ""
	}
	return host, p
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

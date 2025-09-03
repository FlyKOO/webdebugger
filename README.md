# WebDebugger

WebDebugger 是一个使用 Go 编写的 HTTP 与 WebSocket 调试服务器。
服务器会捕获所有连接并打印请求的详细信息，方便网络客户端开发与调试。

## 功能
- **HTTP 捕获**：记录请求方法、路径、头部与请求体。
- **WebSocket 捕获与回显**：输出连接握手信息及每条消息，并将消息原样回显。

## 使用方法

1. 安装 Go 环境 (1.20 及以上)。
2. 克隆仓库并编译：

```bash
go build
```

3. 启动服务器：

```bash
./webdebugger
```

服务器默认监听 `:8080`。
- 发送 HTTP 请求至 `http://localhost:8080/` 可查看控制台输出。
- 通过 `ws://localhost:8080/ws` 建立 WebSocket 连接可测试消息回显。

## 许可证

本项目使用 [MIT](LICENSE) 许可证。

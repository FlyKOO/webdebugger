package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"strings"
	"time"
)

func main() {
	url := flag.String("url", "", "请求的完整 URL（必填）")
	lan := flag.String("lan", "", "请求头        （必填）")
	timeout := flag.Duration("timeout", 15*time.Second, "请求超时")
	maxBody := flag.Int64("max-body", 2<<20, "打印 body 的最大字节数（默认 2MB）")
	flag.Parse()

	if strings.TrimSpace(*url) == "" {
		fmt.Fprintln(os.Stderr, "用法：go run main.go -url https://example.com/path?foo=bar")
		os.Exit(2)
	}

	// 记录时间
	start := time.Now()

	// 准备上下文和 httptrace（可观测 DNS、连接等，可选）
	var (
		dnsStart, connStart time.Time
	)
	trace := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone: func(info httptrace.DNSDoneInfo) {
			if !dnsStart.IsZero() {
				fmt.Printf("DNS 解析耗时：%v\n", time.Since(dnsStart))
			}
		},
		ConnectStart: func(network, addr string) { connStart = time.Now() },
		ConnectDone: func(network, addr string, err error) {
			if !connStart.IsZero() {
				fmt.Printf("TCP 连接耗时：%v（目标：%s）\n", time.Since(connStart), addr)
			}
		},
	}
	ctx, cancel := context.WithTimeout(httptrace.WithClientTrace(context.Background(), trace), *timeout)
	defer cancel()

	// 构建请求
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "构建请求失败：%v\n", err)
		os.Exit(1)
	}
	// 自定义头：Lang: zh_CN pl th en ...
	req.Header.Set("Lang", *lan)
	// 一些通用头
	req.Header.Set("User-Agent", "Go-HTTP-Client/1.1 (+lang=zh_CN)")
	req.Header.Set("Accept", "*/*")

	// 自定义 Client：跟随最多 10 次重定向，并打印重定向信息
	client := &http.Client{
		Timeout: *timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 {
				fmt.Printf("重定向 #%d → %s\n", len(via), req.URL.String())
			}
			if len(via) >= 10 {
				return fmt.Errorf("重定向过多")
			}
			return nil
		},
	}

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "请求失败：%v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// 打印基础信息
	fmt.Println("====== 请求信息 ======")
	fmt.Printf("方法：%s\n", req.Method)
	fmt.Printf("初始 URL：%s\n", *url)
	fmt.Printf("已发送请求头：\n")
	for k, v := range req.Header {
		fmt.Printf("  %s: %s\n", k, strings.Join(v, ", "))
	}

	fmt.Println("\n====== 响应信息 ======")
	fmt.Printf("最终 URL：%s\n", resp.Request.URL.String())
	fmt.Printf("协议版本：%s\n", resp.Proto)
	fmt.Printf("状态码：%d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
	if resp.ContentLength >= 0 {
		fmt.Printf("Content-Length：%d\n", resp.ContentLength)
	} else {
		fmt.Printf("Content-Length：未知（分块传输或未提供）\n")
	}

	fmt.Printf("\n响应头：\n")
	for k, v := range resp.Header {
		fmt.Printf("  %s: %s\n", k, strings.Join(v, ", "))
	}

	// 读取并打印 body（限制最大字节数，避免刷屏）
	fmt.Println("\n====== 响应 Body（可能已截断） ======")
	limited := io.LimitReader(resp.Body, *maxBody)
	n, err := io.Copy(os.Stdout, limited)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n读取 body 出错：%v\n", err)
	}
	if n == *maxBody {
		fmt.Fprintf(os.Stderr, "\n\n[提示] 输出已达到 max-body=%d 字节的上限，可能已截断。\n", *maxBody)
	}

	fmt.Printf("\n====== 用时 ======\n总耗时：%v\n", time.Since(start))
}

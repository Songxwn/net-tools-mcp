// net-tools-mcp 是一个通过 stdio 提供网络查询工具的 MCP 服务。
// 标准输出只承载 MCP 协议，日志写到标准错误。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	listenFlag := flag.String("listen", "", "HTTP 监听地址，例如 :8080。设置后提供 Streamable HTTP，不再占用 stdio")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "net-tools-mcp",
		Version: version,
	}, nil)
	registerTools(server)

	addr := os.Getenv("MCP_LISTEN")
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "listen" {
			addr = *listenFlag
		}
	})
	if addr == "" {
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := serveHTTP(server, addr); err != nil {
		log.Fatal(err)
	}
}

func serveHTTP(server *mcp.Server, addr string) error {
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		SessionTimeout: 30 * time.Minute,
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/mcp", mcpHandler)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("MCP HTTP 监听 %s ，路径 /mcp", addr)
	err := httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func registerTools(server *mcp.Server) {
	hints := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(true)}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ping",
		Title:       "Ping 解析 IP",
		Description: "调用系统 ping，从应答中解析主机名对应的 IP，并返回是否可达与往返时延。",
		Annotations: hints,
	}, pingTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ip_me",
		Title:       "公网 IP 归属",
		Description: "用与 curl https://ip.me 相同的方式获取本机出口公网 IP，再读取 ip.me 上的国家、城市、运营商和 ASN 归属。传入 ip 时改为查询该地址的归属。",
		Annotations: hints,
	}, ipMeTool)
}

func boolPtr(v bool) *bool { return &v }

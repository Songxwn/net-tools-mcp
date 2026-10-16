// net-tools-mcp 是一个通过 stdio 提供网络查询工具的 MCP 服务。
// 标准输出只承载 MCP 协议，日志写到标准错误。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
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
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
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

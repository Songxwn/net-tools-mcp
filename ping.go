package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	maxPingCount   = 4
	maxPingTimeout = 5 * time.Second
)

type pingInput struct {
	Host       string `json:"host" jsonschema:"要解析的主机名或 IP 地址"`
	Count      int    `json:"count,omitempty" jsonschema:"探测包数量，1 到 4，默认 1"`
	TimeoutSec int    `json:"timeout_sec,omitempty" jsonschema:"单包等待秒数，1 到 5，默认 2"`
}

type pingOutput struct {
	Host      string    `json:"host" jsonschema:"查询的主机名或地址"`
	IP        string    `json:"ip" jsonschema:"ping 解析到的 IP"`
	Reachable bool      `json:"reachable" jsonschema:"是否收到至少一次应答"`
	Sent      int       `json:"sent" jsonschema:"发送的探测包数量"`
	Received  int       `json:"received" jsonschema:"收到的应答数量"`
	RTTMS     []float64 `json:"rtt_ms,omitempty" jsonschema:"各次应答的往返时延，单位毫秒"`
}

func pingTool(ctx context.Context, _ *mcp.CallToolRequest, in pingInput) (*mcp.CallToolResult, pingOutput, error) {
	out, err := runPing(ctx, in.Host, in.Count, time.Duration(in.TimeoutSec)*time.Second)
	if err != nil {
		return nil, pingOutput{}, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: formatPing(out)}},
	}, out, nil
}

func runPing(ctx context.Context, host string, count int, timeout time.Duration) (pingOutput, error) {
	host = strings.TrimSpace(host)
	if err := validateHost(host); err != nil {
		return pingOutput{}, err
	}
	if count <= 0 {
		count = 1
	}
	if count > maxPingCount {
		return pingOutput{}, fmt.Errorf("count 最大为 %d", maxPingCount)
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if timeout > maxPingTimeout {
		return pingOutput{}, fmt.Errorf("timeout_sec 最大为 %d", int(maxPingTimeout.Seconds()))
	}

	name, args := pingCommand(runtime.GOOS, host, count, timeout)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(count)*timeout+2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	text := decodeCommandOutput(buf.Bytes())
	parsed := parsePingOutput(host, text)
	if parsed.Sent == 0 {
		parsed.Sent = count
	}
	if parsed.IP == "" {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return pingOutput{}, fmt.Errorf("ping 超时，未解析到 IP：%s", truncate(text, 400))
		}
		if err != nil {
			return pingOutput{}, fmt.Errorf("ping 失败：%s", truncate(text, 400))
		}
		return pingOutput{}, fmt.Errorf("未能从 ping 输出解析 IP：%s", truncate(text, 400))
	}
	return parsed, nil
}

func validateHost(host string) error {
	if host == "" {
		return errors.New("host 不能为空")
	}
	if len(host) > 253 {
		return errors.New("host 过长")
	}
	if strings.HasPrefix(host, "-") {
		return errors.New("host 不能以 - 开头")
	}
	for _, r := range host {
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) {
			return errors.New("host 包含非法字符")
		}
	}
	return nil
}

func pingCommand(goos, host string, count int, timeout time.Duration) (string, []string) {
	n := strconv.Itoa(count)
	if goos == "windows" {
		args := []string{"-n", n, "-w", strconv.Itoa(int(timeout.Milliseconds()))}
		if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
			args = append(args, "-6")
		}
		return "ping", append(args, host)
	}
	wait := strconv.Itoa(int(timeout.Seconds()))
	if goos == "darwin" {
		wait = strconv.Itoa(int(timeout.Milliseconds()))
	}
	if wait == "0" {
		wait = "1"
	}
	return "ping", []string{"-c", n, "-W", wait, host}
}

var (
	bracketIP = regexp.MustCompile(`\[([0-9A-Fa-f:.]+)\]`)
	parenIP   = regexp.MustCompile(`\(([0-9A-Fa-f:.]+)\)`)
	rttRe     = regexp.MustCompile(`(?i)(?:time|时间)\s*[=<]\s*([0-9]+(?:\.[0-9]+)?)\s*ms`)
	sentRe    = regexp.MustCompile(`(?:Sent|已发送)\s*=\s*(\d+)`)
	recvRe    = regexp.MustCompile(`(?:Received|已接收)\s*=\s*(\d+)`)
	linuxStat = regexp.MustCompile(`(\d+)\s+packets transmitted,\s*(\d+)\s+received`)
)

func parsePingOutput(host, text string) pingOutput {
	out := pingOutput{Host: host}
	out.IP = firstIP(bracketIP, text)
	if out.IP == "" {
		out.IP = firstIP(parenIP, text)
	}
	if out.IP == "" {
		if ip := net.ParseIP(host); ip != nil {
			out.IP = ip.String()
		}
	}
	for _, m := range rttRe.FindAllStringSubmatch(text, -1) {
		v, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			out.RTTMS = append(out.RTTMS, v)
		}
	}
	if m := linuxStat.FindStringSubmatch(text); m != nil {
		out.Sent, _ = strconv.Atoi(m[1])
		out.Received, _ = strconv.Atoi(m[2])
	} else {
		if m := sentRe.FindStringSubmatch(text); m != nil {
			out.Sent, _ = strconv.Atoi(m[1])
		}
		if m := recvRe.FindStringSubmatch(text); m != nil {
			out.Received, _ = strconv.Atoi(m[1])
		}
	}
	out.Reachable = out.Received > 0 || len(out.RTTMS) > 0
	if out.Received == 0 && len(out.RTTMS) > 0 {
		out.Received = len(out.RTTMS)
	}
	return out
}

func firstIP(re *regexp.Regexp, text string) string {
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		if ip := net.ParseIP(m[1]); ip != nil {
			return ip.String()
		}
	}
	return ""
}

func decodeCommandOutput(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	if runtime.GOOS == "windows" {
		decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(b)
		if err == nil && utf8.Valid(decoded) {
			return string(decoded)
		}
	}
	return string(b)
}

func formatPing(out pingOutput) string {
	state := "不可达"
	if out.Reachable {
		state = "可达"
	}
	msg := fmt.Sprintf("主机 %s 解析为 %s，%s（发送 %d，接收 %d）", out.Host, out.IP, state, out.Sent, out.Received)
	if len(out.RTTMS) > 0 {
		parts := make([]string, len(out.RTTMS))
		for i, v := range out.RTTMS {
			parts[i] = strconv.FormatFloat(v, 'f', -1, 64) + "ms"
		}
		msg += "，时延 " + strings.Join(parts, ", ")
	}
	return msg
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "无输出"
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.ValidString(s[:n]) {
		n--
	}
	return s[:n] + "…"
}

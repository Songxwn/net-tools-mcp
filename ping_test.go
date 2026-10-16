package main

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestParsePingOutputWindowsEnglish(t *testing.T) {
	text := `
Pinging example.com [93.184.216.34] with 32 bytes of data:
Reply from 93.184.216.34: bytes=32 time=11ms TTL=56

Ping statistics for 93.184.216.34:
    Packets: Sent = 1, Received = 1, Lost = 0 (0% loss),
Approximate round trip times in milli-seconds:
    Minimum = 11ms, Maximum = 11ms, Average = 11ms
`
	got := parsePingOutput("example.com", text)
	if got.IP != "93.184.216.34" || !got.Reachable || got.Sent != 1 || got.Received != 1 {
		t.Fatalf("%+v", got)
	}
	if len(got.RTTMS) == 0 || got.RTTMS[0] != 11 {
		t.Fatalf("rtt: %+v", got.RTTMS)
	}
}

func TestParsePingOutputWindowsChinese(t *testing.T) {
	text := `
正在 Ping example.com [93.184.216.34] 具有 32 字节的数据:
来自 93.184.216.34 的回复: 字节=32 时间=12ms TTL=56

93.184.216.34 的 Ping 统计信息:
    数据包: 已发送 = 1，已接收 = 1，丢失 = 0 (0% 丢失)，
`
	got := parsePingOutput("example.com", text)
	if got.IP != "93.184.216.34" || !got.Reachable || got.Received != 1 || got.RTTMS[0] != 12 {
		t.Fatalf("%+v", got)
	}
}

func TestParsePingOutputLinux(t *testing.T) {
	text := `
PING example.com (93.184.216.34) 56(84) bytes of data.
64 bytes from lga34s.example.com (93.184.216.34): icmp_seq=1 ttl=56 time=11.2 ms

--- example.com ping statistics ---
1 packets transmitted, 1 received, 0% packet loss, time 0ms
rtt min/avg/max/mdev = 11.234/11.234/11.234/0.000 ms
`
	got := parsePingOutput("example.com", text)
	if got.IP != "93.184.216.34" || !got.Reachable || got.Sent != 1 || got.Received != 1 {
		t.Fatalf("%+v", got)
	}
	if got.RTTMS[0] != 11.2 {
		t.Fatalf("rtt: %+v", got.RTTMS)
	}
}

func TestParsePingOutputUnreachableStillResolves(t *testing.T) {
	text := "Pinging missing.example [203.0.113.5] with 32 bytes of data:\nRequest timed out.\n"
	got := parsePingOutput("missing.example", text)
	if got.IP != "203.0.113.5" || got.Reachable {
		t.Fatalf("%+v", got)
	}
}

func TestPingCommand(t *testing.T) {
	name, args := pingCommand("windows", "example.com", 1, 2*time.Second)
	if name != "ping" || strings.Join(args, " ") != "-n 1 -w 2000 example.com" {
		t.Fatalf("%s %v", name, args)
	}
	_, v6 := pingCommand("windows", "2001:db8::1", 1, time.Second)
	if !strings.Contains(strings.Join(v6, " "), "-6") {
		t.Fatalf("%v", v6)
	}
	_, linux := pingCommand("linux", "example.com", 2, 3*time.Second)
	if strings.Join(linux, " ") != "-c 2 -W 3 example.com" {
		t.Fatalf("%v", linux)
	}
	_, mac := pingCommand("darwin", "example.com", 1, time.Second)
	if strings.Join(mac, " ") != "-c 1 -W 1000 example.com" {
		t.Fatalf("%v", mac)
	}
}

func TestValidateHost(t *testing.T) {
	if err := validateHost("example.com"); err != nil {
		t.Fatal(err)
	}
	if err := validateHost("-n"); err == nil {
		t.Fatal("flag-like host accepted")
	}
	if err := validateHost("bad host"); err == nil {
		t.Fatal("space accepted")
	}
}

func TestDecodeGBKPing(t *testing.T) {
	utf8Text := "正在 Ping example.com [1.1.1.1] 具有 32 字节的数据:"
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(utf8Text))
	if err != nil {
		t.Fatal(err)
	}
	if decodeCommandOutput(gbk) == utf8Text && strings.Contains(decodeCommandOutput(gbk), "1.1.1.1") {
		return
	}
	got := parsePingOutput("example.com", decodeCommandOutput(gbk))
	if got.IP != "1.1.1.1" {
		t.Fatalf("decoded %q -> %+v", decodeCommandOutput(gbk), got)
	}
}

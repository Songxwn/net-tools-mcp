package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const ipMeHost = "ip.me"

type ipMeInput struct {
	IP string `json:"ip,omitempty" jsonschema:"可选。留空则查询本机出口公网 IP 及其归属；填写则只查询该 IP 在 ip.me 上的归属"`
}

type ipMeOutput struct {
	IP           string `json:"ip" jsonschema:"公网 IP"`
	City         string `json:"city,omitempty" jsonschema:"城市"`
	Country      string `json:"country,omitempty" jsonschema:"国家或地区"`
	CountryCode  string `json:"country_code,omitempty" jsonschema:"国家或地区代码"`
	Latitude     string `json:"latitude,omitempty" jsonschema:"纬度"`
	Longitude    string `json:"longitude,omitempty" jsonschema:"经度"`
	PostalCode   string `json:"postal_code,omitempty" jsonschema:"邮编"`
	Organization string `json:"organization,omitempty" jsonschema:"组织或地址归属"`
	ASN          string `json:"asn,omitempty" jsonschema:"自治系统号"`
	ISP          string `json:"isp,omitempty" jsonschema:"运营商或 ISP"`
	Source       string `json:"source" jsonschema:"数据来源"`
}

func ipMeTool(ctx context.Context, _ *mcp.CallToolRequest, in ipMeInput) (*mcp.CallToolResult, ipMeOutput, error) {
	out, err := lookupIPMe(ctx, strings.TrimSpace(in.IP))
	if err != nil {
		return nil, ipMeOutput{}, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: formatIPMe(out)}},
	}, out, nil
}

func lookupIPMe(ctx context.Context, query string) (ipMeOutput, error) {
	client := &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: allowIPMeRedirect,
	}
	if query != "" {
		ip := net.ParseIP(query)
		if ip == nil {
			return ipMeOutput{}, fmt.Errorf("ip 不是合法地址：%s", query)
		}
		page := (&url.URL{Scheme: "https", Host: ipMeHost, Path: "/ip/" + ip.String()}).String()
		body, err := getIPMe(ctx, client, page, browserUA, "text/html")
		if err != nil {
			return ipMeOutput{}, err
		}
		out := parseIPMeHTML(body)
		if out.IP == "" {
			out.IP = ip.String()
		}
		if out.Organization == "" && out.Country == "" && out.ISP == "" {
			return ipMeOutput{}, fmt.Errorf("ip.me 未返回 %s 的归属信息", ip.String())
		}
		out.Source = page
		return out, nil
	}

	plain, err := getIPMe(ctx, client, "https://ip.me", curlUA, "*/*")
	if err != nil {
		return ipMeOutput{}, err
	}
	ip, err := parsePlainIP(plain)
	if err != nil {
		return ipMeOutput{}, err
	}
	page := "https://ip.me/"
	html, err := getIPMe(ctx, client, page, browserUA, "text/html")
	out := ipMeOutput{IP: ip, Source: "https://ip.me"}
	if err != nil {
		return out, nil
	}
	attr := parseIPMeHTML(html)
	attr.IP = ip
	attr.Source = out.Source
	if attr.Country == "" && attr.Organization == "" && attr.ISP == "" {
		return out, nil
	}
	return attr, nil
}

const (
	curlUA    = "curl/8.7.1"
	browserUA = "Mozilla/5.0"
)

func allowIPMeRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return fmt.Errorf("ip.me 重定向次数过多")
	}
	host := req.URL.Hostname()
	if req.URL.Scheme != "https" || (host != ipMeHost && host != "www.ip.me") {
		return fmt.Errorf("拒绝跳转到 %s", req.URL.String())
	}
	return nil
}

func getIPMe(ctx context.Context, client *http.Client, rawURL, userAgent, accept string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 %s 失败：%w", rawURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("读取 %s 失败：%w", rawURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("请求 %s 返回 HTTP %d", rawURL, resp.StatusCode)
	}
	return string(body), nil
}

func parsePlainIP(body string) (string, error) {
	line := strings.TrimSpace(body)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	ip := net.ParseIP(line)
	if ip == nil {
		return "", fmt.Errorf("ip.me 未返回公网 IP：%s", truncate(line, 120))
	}
	return ip.String(), nil
}

var (
	ipMeFieldRe = regexp.MustCompile(`(?s)<th>\s*([^<]+?)\s*</th>\s*<td[^>]*>\s*(?:<code>)?\s*([^<]*?)\s*(?:</code>)?\s*</td>`)
	ipMeAddrRe  = regexp.MustCompile(`(?s)<p class="ip-address">\s*([^<]+?)\s*</p>`)
)

func parseIPMeHTML(body string) ipMeOutput {
	var out ipMeOutput
	if m := ipMeAddrRe.FindStringSubmatch(body); m != nil {
		if ip := net.ParseIP(strings.TrimSpace(m[1])); ip != nil {
			out.IP = ip.String()
		}
	}
	for _, m := range ipMeFieldRe.FindAllStringSubmatch(body, -1) {
		key := strings.TrimSuffix(strings.TrimSpace(m[1]), ":")
		val := cleanIPMeValue(m[2])
		if val == "" {
			continue
		}
		switch key {
		case "City":
			out.City = val
		case "Country":
			out.Country = val
		case "Country Code":
			out.CountryCode = val
		case "Latitude":
			out.Latitude = val
		case "Longitude":
			out.Longitude = val
		case "Postal Code":
			out.PostalCode = val
		case "Organization":
			out.Organization = val
		case "ASN":
			out.ASN = val
		case "ISP Name":
			out.ISP = val
		}
	}
	return out
}

func cleanIPMeValue(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "none") || strings.EqualFold(s, "null") {
		return ""
	}
	return s
}

func formatIPMe(out ipMeOutput) string {
	parts := []string{"公网 IP: " + out.IP}
	if out.Country != "" || out.City != "" {
		place := strings.TrimSpace(out.Country + " " + out.City)
		parts = append(parts, "归属: "+place)
	}
	if out.Organization != "" {
		parts = append(parts, "组织: "+out.Organization)
	}
	if out.ISP != "" && out.ISP != out.Organization {
		parts = append(parts, "ISP: "+out.ISP)
	}
	if out.ASN != "" {
		parts = append(parts, "ASN: "+out.ASN)
	}
	return strings.Join(parts, "，")
}

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
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
	IP string `json:"ip,omitempty" jsonschema:"可选。留空则查询本机出口公网 IP 及其归属；填写则查询该 IP 的归属"`
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
	if query != "" {
		ip := net.ParseIP(query)
		if ip == nil {
			return ipMeOutput{}, fmt.Errorf("ip 不是合法地址：%s", query)
		}
		query = ip.String()
	}
	client := &http.Client{
		Timeout:       4 * time.Second,
		CheckRedirect: allowProviderRedirect,
	}
	return lookupSources(ctx, client, query, defaultGeoSources, 10*time.Second)
}

type geoResult struct {
	out ipMeOutput
	err error
}

func lookupSources(ctx context.Context, client *http.Client, query string, sources []geoSource, budget time.Duration) (ipMeOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	ready := make([]chan geoResult, len(sources))
	for i, src := range sources {
		ready[i] = make(chan geoResult, 1)
		go func(src geoSource, ch chan geoResult) {
			out, err := src.lookup(ctx, client, query)
			ch <- geoResult{out: out, err: err}
		}(src, ready[i])
	}

	got := make([]geoResult, len(sources))
	seen := make([]bool, len(sources))
	var partial ipMeOutput
	var errs []string
	next := 0
	for next < len(sources) {
		if !seen[next] {
			select {
			case got[next] = <-ready[next]:
				seen[next] = true
			case <-ctx.Done():
				collectReady(ready, got, seen)
			}
		}
		if !seen[next] {
			next++
			continue
		}
		out, errText, ok := acceptGeo(sources[next].name, got[next], query)
		if errText != "" {
			errs = append(errs, errText)
		}
		if !ok {
			next++
			continue
		}
		if out.hasAttribution() {
			log.Printf("归属查询使用 %s，IP %s", sources[next].name, out.IP)
			return out, nil
		}
		if partial.IP == "" {
			partial = out
		}
		next++
	}
	if partial.IP != "" {
		if ctx.Err() != nil {
			log.Printf("归属查询超时，返回已获得的 IP %s", partial.IP)
		}
		return partial, nil
	}
	if ctx.Err() != nil {
		errs = append(errs, "总超时")
	}
	if len(errs) == 0 {
		return ipMeOutput{}, errors.New("没有可用的归属查询站点")
	}
	if ctx.Err() != nil {
		return ipMeOutput{}, fmt.Errorf("归属查询超时：%s", strings.Join(errs, "；"))
	}
	return ipMeOutput{}, fmt.Errorf("归属查询失败：%s", strings.Join(errs, "；"))
}

func collectReady(ready []chan geoResult, got []geoResult, seen []bool) {
	for i := range ready {
		if seen[i] {
			continue
		}
		select {
		case got[i] = <-ready[i]:
			seen[i] = true
		default:
		}
	}
}

func acceptGeo(name string, got geoResult, query string) (ipMeOutput, string, bool) {
	if got.err != nil {
		log.Printf("归属查询 %s 失败：%s", name, got.err.Error())
		return ipMeOutput{}, name + ": " + got.err.Error(), false
	}
	out := got.out
	if query != "" && out.IP == "" {
		out.IP = query
	}
	if net.ParseIP(out.IP) == nil || (query != "" && out.IP != query) {
		return ipMeOutput{}, name + ": 未返回所查询的 IP", false
	}
	if !out.hasAttribution() {
		return out, name + ": 无归属信息", true
	}
	return out, "", true
}

func (o ipMeOutput) hasAttribution() bool {
	return o.Country != "" || o.City != "" || o.Organization != "" || o.ISP != "" || o.ASN != ""
}

func lookupIPMeSite(ctx context.Context, client *http.Client, query string) (ipMeOutput, error) {
	if query != "" {
		page := (&url.URL{Scheme: "https", Host: ipMeHost, Path: "/ip/" + query}).String()
		body, err := getText(ctx, client, page, browserUA, "text/html")
		if err != nil {
			return ipMeOutput{}, err
		}
		out := parseIPMeHTML(body)
		if out.IP == "" {
			out.IP = query
		}
		out.Source = page
		if !out.hasAttribution() {
			return out, fmt.Errorf("ip.me 未返回归属")
		}
		return out, nil
	}

	plain, err := getText(ctx, client, "https://ip.me", curlUA, "*/*")
	if err != nil {
		return ipMeOutput{}, err
	}
	ip, err := parsePlainIP(plain)
	if err != nil {
		return ipMeOutput{}, err
	}
	out := ipMeOutput{IP: ip, Source: "https://ip.me"}
	html, err := getText(ctx, client, "https://ip.me/", browserUA, "text/html")
	if err != nil {
		return out, nil
	}
	attr := parseIPMeHTML(html)
	attr.IP = ip
	attr.Source = out.Source
	if attr.hasAttribution() {
		return attr, nil
	}
	return out, nil
}

const (
	curlUA    = "curl/8.7.1"
	browserUA = "Mozilla/5.0"
)

func allowProviderRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 || len(via) >= 3 {
		return fmt.Errorf("重定向过多")
	}
	from := via[0].URL.Hostname()
	to := req.URL.Hostname()
	if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
		return fmt.Errorf("拒绝协议 %s", req.URL.Scheme)
	}
	if to == from || to == "www."+from || from == "www."+to {
		return nil
	}
	return fmt.Errorf("拒绝跳转到 %s", req.URL.Host)
}

func getText(ctx context.Context, client *http.Client, rawURL, userAgent, accept string) (string, error) {
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
		return "", fmt.Errorf("未返回公网 IP：%s", truncate(line, 120))
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
	placeName := out.Country
	if placeName == "" {
		placeName = out.CountryCode
	}
	if placeName != "" || out.City != "" {
		place := strings.TrimSpace(placeName + " " + out.City)
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
	if out.Source != "" {
		parts = append(parts, "来源: "+out.Source)
	}
	if !out.hasAttribution() {
		parts = append(parts, "未获取到归属")
	}
	return strings.Join(parts, "，")
}

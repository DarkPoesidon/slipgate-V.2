package handlers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/anonvector/slipgate/internal/actions"
	"github.com/anonvector/slipgate/internal/cloudflare"
	"github.com/anonvector/slipgate/internal/config"
	"github.com/anonvector/slipgate/internal/prompt"
	"golang.org/x/term"
)

type dnsRecordPlan struct {
	Type    string
	Name    string
	Content string
	Proxied bool
}

type dnsPlanResult struct {
	Created []dnsRecordPlan
	Updated []dnsRecordPlan
	Kept    []dnsRecordPlan
	Blocked []string
}

func offerCloudflareDNS(ctx *actions.Context, tunnels []config.TunnelConfig) {
	out := ctx.Output
	planned := buildCloudflareDNSPlan(tunnels)
	if len(planned) == 0 {
		return
	}

	out.Print("")
	out.Print("  Cloudflare DNS setup")
	out.Print("  This optional step creates the DNS records these tunnels need.")
	out.Print("  DNS tunnel records are delegated to this server:")
	out.Print("    A   ns.example.com -> this server IP")
	out.Print("    NS  t.example.com  -> ns.example.com")
	out.Print("  HTTPS proxy records point directly to this server:")
	out.Print("    A   example.com    -> this server IP")
	out.Print("  Records are created as DNS-only because tunnel traffic must reach")
	out.Print("  this server directly. Required token permissions: Zone:Read, DNS:Edit.")
	out.Print("")

	if !shouldPromptCloudflare(ctx) {
		out.Print("  Cloudflare automation skipped in non-interactive mode.")
		return
	}

	ok, err := prompt.Confirm("Automatically configure Cloudflare DNS records?")
	if err != nil || !ok {
		return
	}

	token := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
	if token == "" {
		token, err = prompt.String("Cloudflare API token", "")
		if err != nil || token == "" {
			out.Warning("Cloudflare DNS setup skipped: API token is required")
			return
		}
	}

	defaultZone := inferZone(planned)
	zoneName, err := prompt.String("Cloudflare zone/root domain", defaultZone)
	if err != nil || zoneName == "" {
		out.Warning("Cloudflare DNS setup skipped: zone is required")
		return
	}

	defaultIP := detectPublicIPv4()
	ip, err := prompt.String("Server public IPv4", defaultIP)
	if err != nil || net.ParseIP(ip) == nil || strings.Contains(ip, ":") {
		out.Warning("Cloudflare DNS setup skipped: valid public IPv4 is required")
		return
	}

	planned = fillRecordIP(planned, ip)
	out.Print("")
	out.Print("  Planned Cloudflare DNS changes:")
	for _, rec := range planned {
		out.Print(fmt.Sprintf("    %-4s %-28s -> %s", rec.Type, rec.Name, rec.Content))
	}
	out.Print("")

	apply, err := prompt.Confirm("Apply these Cloudflare DNS changes?")
	if err != nil || !apply {
		return
	}

	result, err := applyCloudflareDNS(context.Background(), token, zoneName, planned)
	if err != nil {
		out.Warning("Cloudflare DNS setup failed: " + err.Error())
		return
	}

	printCloudflareResult(out, result)
}

func shouldPromptCloudflare(ctx *actions.Context) bool {
	if strings.EqualFold(ctx.GetArg("cloudflare-dns"), "yes") || strings.EqualFold(ctx.GetArg("cloudflare-dns"), "true") {
		return true
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func buildCloudflareDNSPlan(tunnels []config.TunnelConfig) []dnsRecordPlan {
	seen := make(map[string]bool)
	var records []dnsRecordPlan

	add := func(rec dnsRecordPlan) {
		rec.Name = normalizeDNSName(rec.Name)
		rec.Content = normalizeDNSName(rec.Content)
		key := rec.Type + ":" + rec.Name + ":" + rec.Content
		if rec.Name == "" || rec.Content == "" || seen[key] {
			return
		}
		seen[key] = true
		records = append(records, rec)
	}

	for _, t := range tunnels {
		if t.Domain == "" || t.IsDirectTransport() {
			continue
		}
		if t.Transport == config.TransportNaive {
			add(dnsRecordPlan{Type: "A", Name: t.Domain, Content: "<server-ip>", Proxied: false})
			continue
		}

		parent := baseDomain(t.Domain)
		nsName := "ns." + parent
		add(dnsRecordPlan{Type: "A", Name: nsName, Content: "<server-ip>", Proxied: false})
		add(dnsRecordPlan{Type: "NS", Name: t.Domain, Content: nsName, Proxied: false})
	}

	return records
}

func fillRecordIP(records []dnsRecordPlan, ip string) []dnsRecordPlan {
	out := make([]dnsRecordPlan, len(records))
	for i, rec := range records {
		if rec.Content == "<server-ip>" {
			rec.Content = ip
		}
		out[i] = rec
	}
	return out
}

func inferZone(records []dnsRecordPlan) string {
	for _, rec := range records {
		if rec.Name != "" {
			return baseDomain(rec.Name)
		}
	}
	return ""
}

func applyCloudflareDNS(ctx context.Context, token, zoneName string, desired []dnsRecordPlan) (dnsPlanResult, error) {
	client := cloudflare.New(token)
	zoneID, err := client.ZoneID(ctx, zoneName)
	if err != nil {
		return dnsPlanResult{}, err
	}

	var result dnsPlanResult
	for _, want := range desired {
		existing, err := client.Records(ctx, zoneID, want.Name)
		if err != nil {
			return result, err
		}

		action, record, reason := decideCloudflareAction(want, existing)
		switch action {
		case "keep":
			result.Kept = append(result.Kept, want)
		case "block":
			result.Blocked = append(result.Blocked, reason)
		case "update":
			record.Content = want.Content
			record.TTL = 1
			record.Proxied = nil
			if want.Type == "A" || want.Type == "AAAA" || want.Type == "CNAME" {
				record.Proxied = boolPtr(want.Proxied)
			}
			if err := client.UpdateRecord(ctx, zoneID, record); err != nil {
				return result, err
			}
			result.Updated = append(result.Updated, want)
		case "create":
			rec := cloudflare.Record{
				Type:    want.Type,
				Name:    want.Name,
				Content: want.Content,
				TTL:     1,
			}
			if want.Type == "A" || want.Type == "AAAA" || want.Type == "CNAME" {
				rec.Proxied = boolPtr(want.Proxied)
			}
			if err := client.CreateRecord(ctx, zoneID, rec); err != nil {
				return result, err
			}
			result.Created = append(result.Created, want)
		}
	}
	return result, nil
}

func decideCloudflareAction(want dnsRecordPlan, existing []cloudflare.Record) (string, cloudflare.Record, string) {
	var sameType *cloudflare.Record
	for i := range existing {
		rec := existing[i]
		if strings.EqualFold(rec.Type, want.Type) {
			sameType = &rec
			if normalizeDNSName(rec.Content) == normalizeDNSName(want.Content) {
				if want.Type != "A" && want.Type != "AAAA" && want.Type != "CNAME" {
					return "keep", rec, ""
				}
				if rec.Proxied == nil || *rec.Proxied == want.Proxied {
					return "keep", rec, ""
				}
			}
			continue
		}
		if want.Type == "NS" || strings.EqualFold(rec.Type, "NS") {
			return "block", rec, fmt.Sprintf("%s already has %s record; NS records cannot share a name with other record types", want.Name, rec.Type)
		}
		if (want.Type == "A" || want.Type == "AAAA") && strings.EqualFold(rec.Type, "CNAME") {
			return "block", rec, fmt.Sprintf("%s already has CNAME record; A/AAAA cannot share a name with CNAME", want.Name)
		}
		if want.Type == "CNAME" && (strings.EqualFold(rec.Type, "A") || strings.EqualFold(rec.Type, "AAAA")) {
			return "block", rec, fmt.Sprintf("%s already has A/AAAA record; CNAME cannot share a name with A/AAAA", want.Name)
		}
	}

	if sameType != nil {
		return "update", *sameType, ""
	}
	return "create", cloudflare.Record{}, ""
}

func printCloudflareResult(out actions.OutputWriter, result dnsPlanResult) {
	out.Print("")
	out.Print("  Cloudflare DNS result:")
	for _, rec := range result.Created {
		out.Success(fmt.Sprintf("Created %s %s -> %s", rec.Type, rec.Name, rec.Content))
	}
	for _, rec := range result.Updated {
		out.Success(fmt.Sprintf("Updated %s %s -> %s", rec.Type, rec.Name, rec.Content))
	}
	for _, rec := range result.Kept {
		out.Info(fmt.Sprintf("Already correct: %s %s -> %s", rec.Type, rec.Name, rec.Content))
	}
	for _, msg := range result.Blocked {
		out.Warning("Skipped conflicting record: " + msg)
	}
}

func detectPublicIPv4() string {
	client := http.Client{Timeout: 5 * time.Second}
	for _, endpoint := range []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
	} {
		resp, err := client.Get(endpoint)
		if err != nil {
			continue
		}
		data := make([]byte, 64)
		n, _ := resp.Body.Read(data)
		_ = resp.Body.Close()
		ip := strings.TrimSpace(string(data[:n]))
		parsed := net.ParseIP(ip)
		if parsed != nil && parsed.To4() != nil {
			return ip
		}
	}
	return ""
}

func normalizeDNSName(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

func boolPtr(v bool) *bool {
	return &v
}

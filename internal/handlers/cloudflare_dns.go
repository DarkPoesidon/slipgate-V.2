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
	Missing []dnsRecordPlan
}

func offerCloudflareDNS(ctx *actions.Context, tunnels []config.TunnelConfig) error {
	out := ctx.Output
	planned := buildCloudflareDNSPlan(tunnels)
	if len(planned) == 0 {
		return nil
	}

	out.Print("")
	out.Print("  Cloudflare DNS setup")
	out.Print("  This step can create the Cloudflare DNS records automatically.")
	out.Print("")
	out.Print("  Before you continue, make sure:")
	out.Print("    1) The root domain is added to Cloudflare and is Active.")
	out.Print("    2) Your registrar nameservers point to the Cloudflare nameservers.")
	out.Print("    3) Your API token has Zone:Read and DNS:Edit/DNS Write for this zone.")
	out.Print("    4) Tunnel records must be DNS only (gray cloud), not proxied.")
	out.Print("")
	out.Print("  SlipGate will create/update these record types as needed:")
	out.Print("    A   ns.<root-domain>      -> this server IPv4")
	out.Print("    NS  <tunnel-subdomain>    -> ns.<root-domain>")
	out.Print("    A   <naive/root-domain>   -> this server IPv4")
	out.Print("")
	out.Print("  Cloudflare note: NS records cannot share the same name with A,")
	out.Print("  CNAME, TXT, or other records. If a conflict exists, SlipGate will")
	out.Print("  skip that record and show exactly what must be removed or changed.")
	out.Print("")

	mode := cloudflareMode(ctx)
	if mode == "skip" {
		out.Print("  Cloudflare automation skipped in non-interactive mode.")
		return nil
	}

	if mode != "force" {
		ok, err := prompt.ConfirmYes("Automatically configure Cloudflare DNS records now?")
		if err != nil || !ok {
			out.Print("  Cloudflare automation skipped. Use the DNS records in the summary below.")
			return nil
		}
	}

	var err error
	token := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
	if token == "" {
		token, err = prompt.String("Cloudflare API token", "")
		if err != nil || token == "" {
			out.Warning("Cloudflare DNS setup skipped: API token is required")
			return strictCloudflareErr(mode, "Cloudflare API token is required", nil)
		}
	}

	defaultZone := inferZone(planned)
	zoneName := strings.TrimSpace(ctx.GetArg("cloudflare-zone"))
	if zoneName == "" {
		zoneName, err = prompt.String("Cloudflare zone/root domain", defaultZone)
	}
	if err != nil || strings.TrimSpace(zoneName) == "" {
		out.Warning("Cloudflare DNS setup skipped: zone is required")
		return strictCloudflareErr(mode, "Cloudflare zone is required", nil)
	}

	defaultIP := detectPublicIPv4()
	ip := strings.TrimSpace(ctx.GetArg("cloudflare-ip"))
	if ip == "" {
		ip, err = prompt.String("Server public IPv4", defaultIP)
	}
	if err != nil || net.ParseIP(ip) == nil || strings.Contains(ip, ":") {
		out.Warning("Cloudflare DNS setup skipped: valid public IPv4 is required")
		return strictCloudflareErr(mode, "valid public IPv4 is required for Cloudflare DNS", nil)
	}

	planned = fillRecordIP(planned, ip)
	out.Print("")
	out.Print("  Planned Cloudflare DNS changes:")
	for _, rec := range planned {
		out.Print(fmt.Sprintf("    %-4s %-28s -> %s", rec.Type, rec.Name, rec.Content))
	}
	out.Print("")
	out.Print("  These records will be set to DNS only where Cloudflare supports proxying.")
	out.Print("  Existing matching records are kept. Existing same-type records with")
	out.Print("  different content are updated. Unsafe conflicts are skipped.")
	out.Print("")

	if !isYes(ctx.GetArg("cloudflare-apply")) {
		apply, err := prompt.ConfirmYes("Apply these Cloudflare DNS changes?")
		if err != nil || !apply {
			out.Print("  Cloudflare automation skipped before applying changes.")
			return strictCloudflareErr(mode, "Cloudflare DNS changes were not applied", nil)
		}
	}

	result, err := applyCloudflareDNS(context.Background(), token, zoneName, planned)
	if err != nil {
		out.Warning("Cloudflare DNS setup failed: " + err.Error())
		return strictCloudflareErr(mode, "Cloudflare DNS setup failed", err)
	}

	printCloudflareResult(out, result)
	if mode == "force" && (len(result.Blocked) > 0 || len(result.Missing) > 0) {
		return strictCloudflareErr(mode, "Cloudflare DNS setup did not complete; resolve the listed DNS conflicts/errors and rerun install", nil)
	}
	return nil
}

func strictCloudflareErr(mode, msg string, err error) error {
	if mode != "force" {
		return nil
	}
	return actions.NewError(actions.SystemInstall, msg, err)
}

func shouldPromptCloudflare(ctx *actions.Context) bool {
	return cloudflareMode(ctx) != "skip"
}

func cloudflareMode(ctx *actions.Context) string {
	value := strings.ToLower(strings.TrimSpace(ctx.GetArg("cloudflare-dns")))
	switch value {
	case "yes", "true", "1", "on":
		return "force"
	case "no", "false", "0", "off":
		return "skip"
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return "prompt"
	}
	return "skip"
}

func isYes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1", "on", "y":
		return true
	default:
		return false
	}
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

	missing, err := verifyCloudflareDNS(ctx, client, zoneID, desired)
	if err != nil {
		return result, err
	}
	result.Missing = missing
	return result, nil
}

func verifyCloudflareDNS(ctx context.Context, client *cloudflare.Client, zoneID string, desired []dnsRecordPlan) ([]dnsRecordPlan, error) {
	var missing []dnsRecordPlan
	for _, want := range desired {
		existing, err := client.Records(ctx, zoneID, want.Name)
		if err != nil {
			return missing, err
		}
		found := false
		for _, rec := range existing {
			if strings.EqualFold(rec.Type, want.Type) && normalizeDNSName(rec.Content) == normalizeDNSName(want.Content) {
				if want.Type == "A" || want.Type == "AAAA" || want.Type == "CNAME" {
					if rec.Proxied != nil && *rec.Proxied != want.Proxied {
						continue
					}
				}
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, want)
		}
	}
	return missing, nil
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
	for _, rec := range result.Missing {
		out.Warning(fmt.Sprintf("Missing after apply: %s %s -> %s", rec.Type, rec.Name, rec.Content))
	}
	if len(result.Created) == 0 && len(result.Updated) == 0 && len(result.Kept) == 0 && len(result.Blocked) == 0 && len(result.Missing) == 0 {
		out.Warning("No Cloudflare DNS records were changed.")
		return
	}
	if len(result.Blocked) == 0 && len(result.Missing) == 0 {
		out.Success("Cloudflare DNS records are configured. No manual DNS setup is needed for the records above.")
	} else {
		out.Warning("Some Cloudflare DNS records still need manual cleanup or token permission fixes.")
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

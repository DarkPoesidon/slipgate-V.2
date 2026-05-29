package handlers

import (
	"fmt"
	"strings"

	"github.com/anonvector/slipgate/internal/actions"
	"github.com/anonvector/slipgate/internal/config"
	"github.com/anonvector/slipgate/internal/service"
)

func handleRouterStatus(ctx *actions.Context) error {
	cfg := ctx.Config.(*config.Config)
	out := ctx.Output

	out.Print(fmt.Sprintf("  Mode:    %s", cfg.Route.Mode))
	out.Print(fmt.Sprintf("  Active:  %s", cfg.Route.Active))
	out.Print(fmt.Sprintf("  Default: %s", cfg.Route.Default))

	// DNS router service status
	routerStatus, err := service.Status("slipgate-dnsrouter")
	if err != nil {
		routerStatus = "not installed"
	}
	out.Print(fmt.Sprintf("  Router:  %s", routerStatus))

	out.Print("")
	out.Print(fmt.Sprintf("  %-15s %-12s %-8s %-25s %s", "TAG", "TRANSPORT", "BACKEND", "DOMAIN", "STATUS"))
	out.Print("  " + "─────────────────────────────────────────────────────────────────────────")

	for i := range cfg.Tunnels {
		t := &cfg.Tunnels[i]
		svcName := service.TunnelServiceName(t.Tag)
		status, _ := service.Status(svcName)
		if status == "" {
			status = "unknown"
		}
		marker := " "
		if t.Tag == cfg.Route.Active {
			marker = "*"
		}
		out.Print(fmt.Sprintf(" %s%-15s %-12s %-8s %-25s %s",
			marker, t.Tag, t.Transport, t.Backend, t.Domain, status))
		// DNSTT tunnels also serve noizdns clients
		if t.Transport == config.TransportDNSTT {
			noizTag := strings.ReplaceAll(t.Tag, "dnstt", "noizdns")
			out.Print(fmt.Sprintf("  %-15s %-12s %-8s %-25s %s",
				noizTag, "noizdns", t.Backend, t.Domain, status))
		}
	}

	if len(cfg.Tunnels) == 0 {
		out.Info("No tunnels configured")
	}

	return nil
}

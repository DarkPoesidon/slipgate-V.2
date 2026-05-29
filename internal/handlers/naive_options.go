package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/anonvector/slipgate/internal/config"
	"github.com/anonvector/slipgate/internal/prompt"
)

func applyNaiveArgs(n *config.NaiveConfig, args map[string]string) error {
	if n == nil {
		return nil
	}
	if v := strings.TrimSpace(args["naive-port"]); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port <= 0 || port > 65535 {
			return fmt.Errorf("invalid NaiveProxy port: %s", v)
		}
		n.Port = port
	}
	if v := strings.TrimSpace(args["naive-user"]); v != "" {
		n.User = v
	}
	if v := args["naive-password"]; v != "" {
		if err := config.ValidatePassword(v); err != nil {
			return fmt.Errorf("invalid NaiveProxy password: %w", err)
		}
		n.Password = v
	}
	if v := strings.TrimSpace(args["naive-log-level"]); v != "" {
		n.LogLevel = strings.ToUpper(v)
	}
	if v := strings.TrimSpace(args["naive-hide-ip"]); v != "" {
		enabled, err := parseBoolText(v)
		if err != nil {
			return fmt.Errorf("invalid naive-hide-ip: %w", err)
		}
		n.DisableHideIP = !enabled
	}
	if v := strings.TrimSpace(args["naive-hide-via"]); v != "" {
		enabled, err := parseBoolText(v)
		if err != nil {
			return fmt.Errorf("invalid naive-hide-via: %w", err)
		}
		n.DisableHideVia = !enabled
	}
	if v := strings.TrimSpace(args["naive-probe-resistance"]); v != "" {
		enabled, err := parseBoolText(v)
		if err != nil {
			return fmt.Errorf("invalid naive-probe-resistance: %w", err)
		}
		n.DisableProbeResistance = !enabled
	}
	return nil
}

func promptNaiveAdvanced(n *config.NaiveConfig) error {
	if n == nil {
		return nil
	}
	advanced, err := prompt.Confirm("Customize advanced NaiveProxy options?")
	if err != nil || !advanced {
		return err
	}

	user, err := prompt.String("Naive auth username", n.User)
	if err != nil {
		return err
	}
	n.User = strings.TrimSpace(user)

	pass, err := prompt.String("Naive auth password", n.Password)
	if err != nil {
		return err
	}
	if pass != "" {
		if err := config.ValidatePassword(pass); err != nil {
			return err
		}
	}
	n.Password = pass

	logLevel, err := prompt.String("Caddy log level (DEBUG, INFO, WARN, ERROR)", n.ResolvedLogLevel())
	if err != nil {
		return err
	}
	n.LogLevel = strings.ToUpper(strings.TrimSpace(logLevel))

	hideIP, err := prompt.String("Enable hide_ip", boolText(n.HideIPEnabled()))
	if err != nil {
		return err
	}
	if hideIP != "" {
		enabled, err := parseBoolText(hideIP)
		if err != nil {
			return err
		}
		n.DisableHideIP = !enabled
	}

	hideVia, err := prompt.String("Enable hide_via", boolText(n.HideViaEnabled()))
	if err != nil {
		return err
	}
	if hideVia != "" {
		enabled, err := parseBoolText(hideVia)
		if err != nil {
			return err
		}
		n.DisableHideVia = !enabled
	}

	probe, err := prompt.String("Enable probe_resistance", boolText(n.ProbeResistanceEnabled()))
	if err != nil {
		return err
	}
	if probe != "" {
		enabled, err := parseBoolText(probe)
		if err != nil {
			return err
		}
		n.DisableProbeResistance = !enabled
	}
	return nil
}

func hasNaiveAdvancedArgs(args map[string]string) bool {
	for _, key := range []string{
		"naive-user",
		"naive-password",
		"naive-log-level",
		"naive-hide-ip",
		"naive-hide-via",
		"naive-probe-resistance",
	} {
		if strings.TrimSpace(args[key]) != "" {
			return true
		}
	}
	return false
}

func parseBoolText(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on", "enable", "enabled":
		return true, nil
	case "0", "false", "no", "n", "off", "disable", "disabled":
		return false, nil
	default:
		return false, fmt.Errorf("expected yes/no")
	}
}

func boolText(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

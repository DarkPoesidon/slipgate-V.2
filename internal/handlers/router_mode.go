package handlers

import (
	"fmt"

	"github.com/anonvector/slipgate/internal/actions"
	"github.com/anonvector/slipgate/internal/config"
	"github.com/anonvector/slipgate/internal/prompt"
	"github.com/anonvector/slipgate/internal/router"
)

func handleRouterMode(ctx *actions.Context) error {
	cfg := ctx.Config.(*config.Config)
	mode := ctx.GetArg("mode")

	if mode == "" {
		var err error
		mode, err = prompt.Select("Router mode", actions.RouterModeOptions)
		if err != nil {
			return err
		}
	}

	if mode != "single" && mode != "multi" {
		return actions.NewError(actions.RouterMode, fmt.Sprintf("invalid mode: %s", mode), nil)
	}

	if mode == cfg.Route.Mode {
		ctx.Output.Info(fmt.Sprintf("Already in %s mode", mode))
		return nil
	}

	if err := router.SwitchMode(cfg, mode); err != nil {
		return actions.NewError(actions.RouterMode, "failed to switch mode", err)
	}

	cfg.Route.Mode = mode
	if err := cfg.Save(); err != nil {
		return actions.NewError(actions.RouterMode, "failed to save config", err)
	}

	ctx.Output.Success(fmt.Sprintf("Switched to %s mode", mode))
	return nil
}

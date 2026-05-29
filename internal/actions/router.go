package actions

func init() {
	Register(&Action{
		ID:       RouterStatus,
		Name:     "Router Status",
		Category: "router",
	})

	Register(&Action{
		ID:       RouterMode,
		Name:     "Set Router Mode",
		Category: "router",
		Inputs: []InputField{
			{Key: "mode", Label: "Mode", Required: true, Options: RouterModeOptions},
		},
	})

	Register(&Action{
		ID:       RouterSwitch,
		Name:     "Switch Active Tunnel",
		Category: "router",
		Inputs: []InputField{
			{Key: "tag", Label: "Tunnel tag", Required: true},
		},
	})
}

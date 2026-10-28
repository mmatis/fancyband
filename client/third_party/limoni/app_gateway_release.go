//go:build !limoni_debug

package limoni

func openGateway(cfg appConfig) (gateway, error) {
	if cfg.automationPath != "" {
		return nil, ErrAutomationNotCompiled
	}
	return nil, nil
}

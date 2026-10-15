package core

// NavigationDisplayDefaults is used only to freeze old effective device settings
// during migration. Shortcut pairings do not consult projection inputs.
func (e *Engine) NavigationDisplayDefaults() map[string]*uint32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := map[string]*uint32{}
	for _, route := range e.config.Routes {
		if route.NFCDMetricsLeftPadding != nil {
			result[route.NodeID] = route.NFCDMetricsLeftPadding
		}
	}
	return result
}

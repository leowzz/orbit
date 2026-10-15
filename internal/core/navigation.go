package core

// NavigationSources follows the same configured computers as each workbench.
func (e *Engine) NavigationSources() map[string][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := map[string][]string{}
	for _, route := range e.config.Routes {
		if route.Profile != appProfile && route.Profile != "overview-web" && route.Profile != "overview-android" {
			continue
		}
		for _, input := range route.Inputs {
			result[route.NodeID] = append(result[route.NodeID], input.AgentID)
		}
	}
	return result
}

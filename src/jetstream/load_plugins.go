package main

import (
	"github.com/cloudfoundry-incubator/stratos/src/jetstream/repository/interfaces"
	log "github.com/sirupsen/logrus"

	"github.com/cloudfoundry-incubator/stratos/src/jetstream/plugins/yamlgenerated"

	_ "github.com/cloudfoundry-incubator/stratos/src/jetstream/plugins/autoscaler"
	_ "github.com/cloudfoundry-incubator/stratos/src/jetstream/plugins/cfapppush"
	_ "github.com/cloudfoundry-incubator/stratos/src/jetstream/plugins/cfappssh"
	_ "github.com/cloudfoundry-incubator/stratos/src/jetstream/plugins/cloudfoundry"
	_ "github.com/cloudfoundry-incubator/stratos/src/jetstream/plugins/kubernetes"
	_ "github.com/cloudfoundry-incubator/stratos/src/jetstream/plugins/monocular"
	_ "github.com/cloudfoundry-incubator/stratos/src/jetstream/plugins/userinvite"
)

func (pp *portalProxy) loadPlugins() {

	pp.Plugins = make(map[string]interfaces.StratosPlugin)
	log.Info("Initialising plugins")

	yamlgenerated.MakePluginsFromConfig()

	for name := range interfaces.PluginInits {
		addPlugin(pp, name)
	}
	addPlugin(pp, "autoscaler")
	addPlugin(pp, "cloudfoundry")
	addPlugin(pp, "autoscaler")
	addPlugin(pp, "cfapppush")
	addPlugin(pp, "cfappssh")
	addPlugin(pp, "kubernetes")
	addPlugin(pp, "userinvite")
	addPlugin(pp, "monocular")

}

func addPlugin(pp *portalProxy, name string) bool {
	// Has the plugin already been inited?
	if _, ok := pp.Plugins[name]; ok {
		return true
	}

	// Register this one if not already registered
	reg, ok := interfaces.PluginInits[name]
	if !ok {
		// Could not find plugin
		log.Errorf("Could not find plugin: %s", name)
		return false
	}

	// Add all of the plugins for the dependencies
	for _, depend := range reg.Dependencies {
		if !addPlugin(pp, depend) {
			log.Errorf("Unmet dependency - skipping plugin %s", name)
			return false
		}
	}

	plugin, err := reg.Init(pp)
	pp.Plugins[name] = plugin
	if err != nil {
		log.Fatalf("Error loading plugin: %s (%s)", name, err)
	}
	log.Infof("Loaded plugin: %s", name)
	return true
}

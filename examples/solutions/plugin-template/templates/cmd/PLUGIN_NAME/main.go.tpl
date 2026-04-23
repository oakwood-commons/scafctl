// Package main is the entry point for the <% .name %> plugin.
package main

import (
	"<% .module %>/internal/<% .pkg_name %>"

	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
)

func main() {
<%- if eq .plugin_type "auth-handler" %>
	sdkplugin.ServeAuthHandler(&<% .pkg_name %>.Plugin{})
<%- else %>
	sdkplugin.Serve(&<% .pkg_name %>.Plugin{})
<%- end %>
}

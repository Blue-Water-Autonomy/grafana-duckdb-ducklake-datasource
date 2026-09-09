package main

import (
	"os"

	"github.com/Blue-Water-Autonomy/grafana-duckdb-ducklake-datasource/pkg/plugin"
	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

func main() {
	if err := datasource.Manage("local-duckdb-datasource", plugin.NewDatasource, datasource.ManageOpts{}); err != nil {
		log.DefaultLogger.Error("Plugin stopped", "error", err)
		os.Exit(1)
	}
}

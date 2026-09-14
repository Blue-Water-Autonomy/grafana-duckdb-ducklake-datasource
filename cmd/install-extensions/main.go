// Install the extensions with exactly the same embedded DuckDB version as the plugin.
package main

import (
	"database/sql"
	"log"
	"net/url"
	"os"

	_ "github.com/duckdb/duckdb-go/v2"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: install-extensions DIRECTORY")
	}
	db, err := sql.Open("duckdb", "?extension_directory="+url.QueryEscape(os.Args[1]))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"ducklake", "postgres"} {
		if _, err := db.Exec("INSTALL " + name); err != nil {
			log.Fatal(err)
		}
	}
}

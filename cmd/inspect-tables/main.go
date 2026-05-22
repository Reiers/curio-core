package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

func main() {
	ctx := context.Background()
	db, err := harmonysqlite.New(ctx, harmonysqlite.Config{Path: ":memory:"})
	if err != nil { fmt.Println("err:", err); return }
	defer db.Close()
	rows, _ := db.Query(ctx, `SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	defer rows.Close()
	var tables []string
	for rows.Next() { var n string; rows.Scan(&n); tables = append(tables, n) }
	sort.Strings(tables)
	fmt.Printf("Total tables: %d\n", len(tables))
	pdp := 0
	for _, t := range tables {
		if len(t) >= 4 && t[:4] == "pdp_" { pdp++ }
	}
	fmt.Printf("PDP tables: %d\n", pdp)
	for _, t := range tables { fmt.Println("  -", t) }
}

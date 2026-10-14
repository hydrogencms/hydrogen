package main

import (
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

func sqliteModule() kernel.Module   { return sqlite.Module() }
func postgresModule() kernel.Module { return postgres.Module() }

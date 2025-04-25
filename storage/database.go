package storage

import (
	. "clack/common"
	"context"
	_ "embed"
	"encoding/base64"
	"path/filepath"
	"strings"
	"sync"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

//go:embed sql/database_schema.sql
var schema string

var dbFile = filepath.Join(DataFolder, "database.db")
var dbPool *sqlitemigration.Pool
var dbWait sync.WaitGroup
var dbLog = NewLogger("DATABASE")

func StartDatabase(ctx context.Context) *sync.WaitGroup {
	dbWait.Add(1)
	dbLog.Println("Starting")

	schema := sqlitemigration.Schema{
		Migrations: strings.Split(schema, "\n\n"),
	}

	dbPool = sqlitemigration.NewPool(dbFile, schema, sqlitemigration.Options{
		Flags: sqlite.OpenReadWrite | sqlite.OpenCreate,

		PrepareConn: func(conn *sqlite.Conn) error {
			err := conn.CreateFunction("webp_base64", &sqlite.FunctionImpl{
				NArgs:         1,
				Deterministic: true,
				Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
					str := "data:image/webp;base64, " + base64.StdEncoding.EncodeToString(args[0].Blob())
					return sqlite.TextValue(str), nil
				},
			})
			if err != nil {
				dbLog.Panicln(err)
			}

			return sqlitex.ExecuteTransient(conn, "PRAGMA foreign_keys = ON;", nil)
		},
		OnError: func(err error) {
			dbLog.Panicln(err)
		},
	})

	conn, err := dbPool.Get(context.TODO())
	if err != nil {
		panic(err.Error())
	}
	dbPool.Put(conn)

	go func() {
		<-ctx.Done()
		dbPool.Close()
		dbLog.Println("Finished")
		dbWait.Done()
	}()

	return &dbWait
}

func OpenDatabase(ctx context.Context) (*sqlite.Conn, error) {
	return dbPool.Get(ctx)
}

func CloseDatabase(conn *sqlite.Conn) {
	dbPool.Put(conn)
}

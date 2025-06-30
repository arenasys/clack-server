package storage

import (
	. "clack/common"
	"context"
	_ "embed"
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
var dbPoolWait sync.WaitGroup
var dbLog = NewLogger("DATABASE")

func StartDatabase(ctx *ClackContext) {
	ctx.Subsystems.Add(1)
	dbLog.Println("Starting")

	schema := sqlitemigration.Schema{
		Migrations: strings.Split(schema, "\n\n"),
	}

	dbPool = sqlitemigration.NewPool(dbFile, schema, sqlitemigration.Options{
		Flags:    sqlite.OpenReadWrite | sqlite.OpenCreate,
		PoolSize: 16,

		PrepareConn: func(conn *sqlite.Conn) error {
			sqlitex.ExecuteTransient(conn, "PRAGMA journal_mode = WAL;", nil)
			sqlitex.ExecuteTransient(conn, "PRAGMA synchronous = NORMAL;", nil)
			sqlitex.ExecuteTransient(conn, "PRAGMA foreign_keys = ON;", nil)
			sqlitex.ExecuteTransient(conn, "PRAGMA optimize;", nil)

			return nil
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

		CloseDatabase()

		dbLog.Println("Finished")
		ctx.Subsystems.Done()
	}()
}

func CheckpointDatabase() {
	pool, _ := sqlitex.NewPool(dbFile, sqlitex.PoolOptions{
		Flags:    sqlite.OpenReadWrite | sqlite.OpenCreate,
		PoolSize: 1})
	conn, _ := pool.Take(context.TODO())
	NewTransaction(conn).Checkpoint()
	pool.Put(conn)
	pool.Close()
}

func CloseDatabase() {
	dbLog.Println("Waiting for connections to close")
	dbPoolWait.Wait()
	dbLog.Println("Closing connection pool")
	dbPool.Close()
	//dbLog.Println("Running checkpoint")
	//CheckpointDatabase()
}

func OpenConnection(ctx context.Context) (*sqlite.Conn, error) {
	dbPoolWait.Add(1)
	return dbPool.Get(ctx)
}

func CloseConnection(conn *sqlite.Conn) {
	if conn == nil {
		return
	}
	dbPool.Put(conn)
	dbPoolWait.Done()
}

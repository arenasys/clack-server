package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	. "clack/common"

	"clack/network"
	"clack/storage"
	"clack/testing"
)

var MainCtx context.Context

var mainLog = NewLogger("MAIN")

func init() {
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT)

	var cancel context.CancelFunc
	MainCtx, cancel = context.WithCancel(context.Background())

	go func() {
		<-sigchan
		mainLog.Println("Shutting down...")
		signal.Stop(sigchan)
		cancel()
	}()
}

func main() {
	var dataExists bool = false
	if _, err := os.Stat(DataFolder); err == nil {
		dataExists = true
	}

	if !dataExists {
		os.Mkdir(DataFolder, 0755)
	}

	dbWait := storage.StartDatabase(MainCtx)
	srvWait := network.StartServer(MainCtx)

	if !dataExists {
		mainLog.Println("Populating database")
		testing.PopulateDatabase(MainCtx)
		mainLog.Println("Done")
	}

	<-MainCtx.Done()
	dbWait.Wait()
	srvWait.Wait()
	mainLog.Println("Exiting")
}

func PrintMemUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("Alloc = %v MiB", bToMb(m.Alloc))
	fmt.Printf("\tTotalAlloc = %v MiB", bToMb(m.TotalAlloc))
	fmt.Printf("\tSys = %v MiB", bToMb(m.Sys))
	fmt.Printf("\tNumGC = %v\n", m.NumGC)
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

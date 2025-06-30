package network

import (
	"context"
	_ "embed"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gorilla/mux"

	"clack/common"
)

var srvCtx context.Context
var srvLog = common.NewLogger("SERVER")

func buildRouter() *mux.Router {
	r := mux.NewRouter()

	buildAPIRouter(r)
	buildMediaRouter(r)

	r.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		proxy := httputil.NewSingleHostReverseProxy(&url.URL{
			Scheme: "http",
			Host:   "localhost:5173",
		})
		req.Host = "localhost:5173"
		proxy.ServeHTTP(w, req)
	}))

	return r
}

func StartServer(ctx *common.ClackContext) {
	srvCtx = ctx
	ctx.Subsystems.Add(1)
	port := ":8000"

	srvLog.Printf("Starting on %s\n", port)

	r := buildRouter()

	srv := &http.Server{
		Addr:    ":8000",
		Handler: r,
	}

	go func() {
		<-srvCtx.Done()
		srv.Shutdown(context.Background())
		srvLog.Println("Finished")
		ctx.Subsystems.Done()
	}()

	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			srvLog.Fatalf("ListenAndServe(): %v", err)
		}
	}()
}

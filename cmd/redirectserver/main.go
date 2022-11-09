/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/klog/v2"

	"sigs.k8s.io/porche/cmd/redirectserver/app"
	"sigs.k8s.io/porche/cmd/redirectserver/pkg/blobcache"
)

func main() {
	// klog setup
	klog.InitFlags(nil)
	flag.Parse()
	defer klog.Flush()

	// cloud run expects us to listen to HTTP on $PORT
	// https://cloud.google.com/run/docs/container-contract#port
	port := getEnv("PORT", "8080")

	cfg := app.MirrorConfig{
		CanonicalFallback: getEnv("FALLBACK_LOCATION", "https://artifacts.k8s.io/"),
		InfoURL:           "https://github.com/kubernetes/registry.k8s.io",
		PrivacyURL:        "https://www.linuxfoundation.org/privacy-policy/",
	}

	mirrorCache := blobcache.NewCachedBlobChecker(http.DefaultClient)
	redirectServer := app.NewServer(cfg, mirrorCache)

	// configure server with reasonable timeout
	// we only serve redirects and small hashes, 10s should be sufficient
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           redirectServer,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
	}

	// signal handler for graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// start serving
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.Fatal(err)
		}
	}()
	klog.InfoS("listening", "port", port)
	klog.InfoS("registry", "configuration", cfg)

	// Graceful shutdown
	<-done
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		klog.Fatalf("Server didn't exit gracefully %v", err)
	}
}

// getEnv returns defaultValue if key is not set, else the value of os.LookupEnv(key)
func getEnv(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}
